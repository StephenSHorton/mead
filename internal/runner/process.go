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
type Process struct {
	// Immutable after Spawn returns.
	id        RunID
	bottleID  string
	argv      []string
	startedAt time.Time
	logPath   string

	// Live process plumbing.
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{} // closed when the reaper has recorded exit state.
	logFile *os.File      // closed by the reaper; do not write to from outside.

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
// Stable across the process's lifetime.
func (p *Process) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Kill sends SIGTERM to the process group, then escalates to SIGKILL
// after grace if the process hasn't exited. Returns nil if the
// process is already gone.
//
// The exec.CommandContext cancel is fired in parallel, which signals
// the Go stdlib's own watchdog as well — belt + suspenders.
func (p *Process) Kill() error {
	if p.Exited() {
		return nil
	}
	p.cancel()
	pgid, err := syscall.Getpgid(p.PID())
	if err == nil {
		// Negative pid = process group target.
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		// Grace period before SIGKILL escalation.
		select {
		case <-p.done:
			return nil
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	} else if p.cmd.Process != nil {
		// Best-effort fallback if the process group lookup failed.
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
			return nil
		case <-time.After(2 * time.Second):
			_ = p.cmd.Process.Kill()
		}
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("process %d did not exit after kill", p.PID())
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
