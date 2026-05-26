package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process is the supervisor-side handle for a single spawned program.
// All getters are safe to call concurrently.
//
// A Process may be either *live* (we spawned it; cmd is set) or
// *adopted* (re-registered after a Mead restart from a sidecar file;
// cmd is nil and we know it only by pid). Adopted processes still
// support Kill (via direct syscall) and Logs (the file is on disk).
type Process struct {
	// Immutable after Spawn returns (or after Adopt completes).
	id        RunID
	bottleID  string
	argv      []string
	startedAt time.Time
	logPath   string
	metaPath  string // sidecar JSON: written on Spawn, updated on exit, read on Adopt.

	// Live process plumbing. nil for adopted processes.
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{} // closed when the reaper has recorded exit state.
	logFile *os.File      // closed by the reaper; do not write to from outside.

	// pid is set at Spawn time AND when adopting an alive process.
	// Used by Kill for adopted processes (which have no cmd handle).
	pid int

	// Mutable; guarded by mu.
	mu       sync.RWMutex
	exited   bool
	exitedAt time.Time
	exitCode int
	runErr   error
}

// ID returns the opaque RunID assigned at Spawn time.
func (p *Process) ID() RunID { return p.id }

// BottleID returns the bottle this process is associated with, or "".
func (p *Process) BottleID() string { return p.bottleID }

// Argv returns a copy of the argv this process was started with.
func (p *Process) Argv() []string {
	out := make([]string, len(p.argv))
	copy(out, p.argv)
	return out
}

// StartedAt returns the wall-clock time the process was spawned.
func (p *Process) StartedAt() time.Time { return p.startedAt }

// LogPath returns the on-disk path the process's combined output is
// streamed to. The file is opened for write at Spawn time and closed
// by the reaper when the process exits; readers can open it for read
// at any time.
func (p *Process) LogPath() string { return p.logPath }

// Exited reports whether the process has finished. Use the channel
// from Done() to block on it.
func (p *Process) Exited() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exited
}

// ExitCode returns the OS exit code (0 success, non-zero failure, -1
// if the process was killed by a signal). Meaningful only after
// Exited() returns true.
func (p *Process) ExitCode() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitCode
}

// ExitedAt returns the wall-clock time at which the reaper recorded
// the process exit. Zero value if still running.
func (p *Process) ExitedAt() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exitedAt
}

// RunErr returns the error captured by exec.Cmd.Wait, if any. Useful
// for distinguishing "process ran and exited non-zero" (RunErr == nil,
// ExitCode != 0) from "kernel signalled the process" or "spawn
// failed". Returns "" if no error was recorded.
func (p *Process) RunErr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.runErr == nil {
		return ""
	}
	return p.runErr.Error()
}

// Done returns a channel closed once the reaper has fully recorded
// the process's exit state. Blocking on this is the canonical "wait
// for completion" idiom.
func (p *Process) Done() <-chan struct{} { return p.done }

// PID returns the OS process id, or 0 if the process never started.
// Stable across the process's lifetime. For adopted processes the
// pid was read from the sidecar; we don't re-validate liveness here.
func (p *Process) PID() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return p.pid
}

// adopted reports whether this Process was reattached from a sidecar
// rather than spawned in the current Mead process. Adopted processes
// have no cmd handle so Kill must use the raw PID and Wait isn't
// available — we poll for exit instead.
func (p *Process) adopted() bool { return p.cmd == nil }

// writeSidecar serializes the Process's current state to its sidecar
// file. Called by the reaper on exit, by Spawn for the initial write,
// and by the orphan watcher when synthesizing exit info. Safe to call
// concurrently — writeSidecar (the package func) does an atomic
// rename.
func (p *Process) writeSidecar() error {
	if p.metaPath == "" {
		return nil
	}
	p.mu.RLock()
	s := &sidecar{
		ID:        p.id,
		BottleID:  p.bottleID,
		Argv:      append([]string(nil), p.argv...),
		StartedAt: fmtTime(p.startedAt),
		LogPath:   p.logPath,
		PID:       p.pid,
		Exited:    p.exited,
		ExitedAt:  fmtTime(p.exitedAt),
		ExitCode:  p.exitCode,
	}
	if p.runErr != nil {
		s.RunErr = p.runErr.Error()
	}
	p.mu.RUnlock()
	return writeSidecar(p.metaPath, s)
}

// pidAlive reports whether the given OS pid still refers to a live
// process. Uses signal 0 (no-op signal) which only checks for
// existence. Returns false for invalid pids.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// kill(pid, 0) returns nil if the process exists and we have
	// permission to signal it. ESRCH means it's gone. EPERM means
	// the process exists but we can't signal it (treat as alive —
	// it's still there).
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

// Kill sends SIGTERM to the process (or its group for live processes
// we spawned), then escalates to SIGKILL after grace if it hasn't
// exited. Returns nil if the process is already gone.
//
// For LIVE processes (we spawned them with Setpgid): targets the
// whole process group via Kill(-pgid, ...). Wine's auxiliary
// processes (wineserver et al) live in the same group, so killing
// the group cleans up the whole tree.
//
// For ADOPTED processes (reattached from sidecar; cmd is nil): we
// did NOT set up the process group, so Getpgid would return some
// group structure we don't own — possibly including OUR OWN PID,
// which would terminate Mead itself. Targets the PID only.
func (p *Process) Kill() error {
	if p.Exited() {
		return nil
	}
	pid := p.PID()
	if pid <= 0 {
		return fmt.Errorf("no pid to kill")
	}
	// For live processes, cancelling the exec.CommandContext signals
	// Go's own watchdog in parallel — belt + suspenders.
	if p.cancel != nil {
		p.cancel()
	}

	useGroup := !p.adopted()
	sendSignal := func(sig syscall.Signal) {
		if useGroup {
			if pgid, err := syscall.Getpgid(pid); err == nil {
				_ = syscall.Kill(-pgid, sig)
				return
			}
		}
		_ = syscall.Kill(pid, sig)
	}

	sendSignal(syscall.SIGTERM)
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		sendSignal(syscall.SIGKILL)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("process %d did not exit after kill", pid)
	}
}

// Logs reads up to limit bytes from the process's log file starting
// at offset. Returns the bytes read, the next offset to pass on the
// subsequent call (offset + n), and whether the process has exited
// (so a polling caller knows when to stop).
//
// limit=0 means "all available". A reasonable cap from a UI/MCP
// perspective is 64 KiB per call.
//
// A reader can poll Logs(offset) until exited==true && len(bytes)==0,
// at which point the log is fully drained.
func (p *Process) Logs(offset int64, limit int) ([]byte, int64, bool, error) {
	f, err := os.Open(p.logPath)
	if err != nil {
		return nil, offset, p.Exited(), fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, offset, p.Exited(), fmt.Errorf("seek log: %w", err)
		}
	}

	// Determine how much to read. If limit > 0, cap at limit. Otherwise
	// read the whole file from offset.
	var buf []byte
	if limit > 0 {
		buf = make([]byte, limit)
		n, err := io.ReadFull(f, buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, offset, p.Exited(), fmt.Errorf("read log: %w", err)
		}
		return buf[:n], offset + int64(n), p.Exited(), nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, p.Exited(), fmt.Errorf("read log: %w", err)
	}
	return data, offset + int64(len(data)), p.Exited(), nil
}
