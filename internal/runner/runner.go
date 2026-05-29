// Package runner is Mead's process supervisor.
//
// Two operating modes:
//
//   - Run(ctx, spec): synchronous. Spawns a process, waits for it to
//     exit, returns combined stdout+stderr captured into a bounded
//     in-memory buffer. Used for short, blocking operations like
//     wineboot --init.
//
//   - Spawn(ctx, spec): detached. Spawns a process, returns immediately
//     with a *Process. stdout+stderr stream into a per-process log file
//     at Spec.LogPath. The supervisor retains the Process in an
//     in-memory registry keyed by RunID so an MCP client can later
//     poll Logs(), check ExitCode, or Kill it.
//
// The Spawn surface is what makes Mead's agent-driven debug story
// work: Claude can call apps.install → poll process.logs → read the
// error → call winetricks.run → retry. The log file outlives the
// process so logs are still readable after it exits.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// RunID is the opaque identifier the supervisor hands back. Used both
// as a key into the process registry and as the per-run log filename
// stem.
type RunID string

// Runner is the supervisor. One per app process; safe for concurrent
// use across goroutines.
type Runner struct {
	mu        sync.RWMutex
	processes map[RunID]*Process
}

// New returns an empty Runner.
func New() *Runner {
	return &Runner{processes: make(map[RunID]*Process)}
}

// Spec is the description of a process to launch.
type Spec struct {
	// Argv is the command line to execute. argv[0] is the binary path
	// (caller has already resolved it via wine.Locator). Subsequent
	// entries are arguments. Never invoked through a shell — no
	// quoting/escaping needed.
	Argv []string

	// Env is added to (and overrides) the inherited os.Environ. Use
	// this for WINEPREFIX, DXVK_HUD, WINEDEBUG, etc.
	Env map[string]string

	// BottleID, if set, tags the process for later filtering via
	// process.list. Bottle-less processes (e.g. `wine --version` for
	// the locator) leave this empty.
	BottleID string

	// LogPath is the on-disk file detached Spawn() writes combined
	// stdout+stderr into. The directory must exist (caller pre-creates
	// it via store.Store). Ignored by Run().
	//
	// If empty, Spawn() returns an error — a log file is mandatory so
	// the agent can always retrieve output later.
	LogPath string
}

// Result is what a synchronous Run returns. ExitCode is -1 if the
// process was killed by a signal; combined output is captured to a
// bounded buffer so a runaway log doesn't OOM the editor.
type Result struct {
	ExitCode int
	Output   []byte
}

// Run launches the process described by spec and blocks until it
// exits. Returns the captured combined output (stdout+stderr) plus an
// error if the process exited non-zero or the spawn itself failed.
//
// ctx cancellation kills the process group; the captured output up to
// that point is still returned via Result.Output.
//
// Use Spawn() instead when the caller can't (or shouldn't) block.
func (r *Runner) Run(ctx context.Context, spec Spec) (*Result, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("runner.Run: empty argv")
	}

	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = composeEnv(os.Environ(), spec.Env)

	var buf cappedBuffer
	buf.Cap = 1 << 20
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	// If a log path is supplied, tee output to disk too — useful for
	// debugging failed wineboot --init invocations after the fact.
	if spec.LogPath != "" {
		f, err := os.Create(spec.LogPath)
		if err != nil {
			return nil, fmt.Errorf("create log file: %w", err)
		}
		defer f.Close()
		cmd.Stdout = io.MultiWriter(&buf, f)
		cmd.Stderr = cmd.Stdout
	}

	if err := cmd.Run(); err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return &Result{ExitCode: exitCode, Output: buf.Bytes()}, fmt.Errorf("run %s: %w", spec.Argv[0], err)
	}
	return &Result{ExitCode: cmd.ProcessState.ExitCode(), Output: buf.Bytes()}, nil
}

// Spawn launches the process described by spec in detached mode and
// returns immediately. The returned *Process tracks the live process;
// it is also registered in the Runner so Get/List/Kill can find it
// by RunID later.
//
// stdout+stderr are streamed to spec.LogPath. The caller is responsible
// for creating the parent directory.
func (r *Runner) Spawn(ctx context.Context, spec Spec) (*Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("runner.Spawn: empty argv")
	}
	if spec.LogPath == "" {
		return nil, fmt.Errorf("runner.Spawn: LogPath required")
	}

	f, err := os.Create(spec.LogPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}

	// Bound the on-disk log. A chatty process under a verbose WINEDEBUG
	// can emit gigabytes (a single Battle.net launch was observed
	// writing a 3.5 GB log of fixme spam, enough to threaten the disk
	// and stall the very GPU init it was logging). The head of the log
	// holds the actionable output; past the cap we drop and leave a
	// one-time marker. Stdout==Stderr is the SAME writer, so exec
	// funnels both streams through a single output copier — the capped
	// writer never sees concurrent writes.
	logW := &cappedWriter{w: f, cap: maxSpawnLogBytes}

	// CommandContext so an external cancel kills the spawned process.
	// We also pre-build a derived context whose cancel we keep so
	// Process.Kill() can fire it.
	derived, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(derived, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = composeEnv(os.Environ(), spec.Env)
	cmd.Stdout = logW
	cmd.Stderr = logW
	// SysProcAttr.Setpgid puts the child in its own process group so
	// Kill() can target the whole group with a single signal. Useful
	// when a Wine launch spawns helper processes (wineserver et al).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		f.Close()
		cancel()
		return nil, fmt.Errorf("start %s: %w", spec.Argv[0], err)
	}

	p := &Process{
		id:        RunID(uuid.NewString()),
		bottleID:  spec.BottleID,
		argv:      append([]string(nil), spec.Argv...),
		startedAt: time.Now().UTC(),
		logPath:   spec.LogPath,
		metaPath:  sidecarPathFor(spec.LogPath),
		cmd:       cmd,
		cancel:    cancel,
		done:      make(chan struct{}),
		logFile:   f,
		pid:       cmd.Process.Pid,
	}

	// Write the initial sidecar immediately so a discovery scan that
	// runs the moment after Spawn picks up this process.
	if err := p.writeSidecar(); err != nil {
		// Sidecar write failures are non-fatal — the in-memory
		// registry still tracks the process, just won't survive a
		// Mead restart. Log loudly so this gets noticed.
		fmt.Fprintf(os.Stderr, "runner.Spawn: write sidecar failed: %v\n", err)
	}

	r.mu.Lock()
	r.processes[p.id] = p
	r.mu.Unlock()

	// Reaper goroutine: wait for the process to exit, record state,
	// update the sidecar, close the log file, signal done.
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exited = true
		p.exitedAt = time.Now().UTC()
		p.exitCode = -1
		if cmd.ProcessState != nil {
			p.exitCode = cmd.ProcessState.ExitCode()
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			p.runErr = err
		}
		p.mu.Unlock()
		_ = f.Close()
		_ = p.writeSidecar() // final state — best effort.
		close(p.done)
	}()

	return p, nil
}

// Adopt re-registers a process from its on-disk sidecar JSON. Called
// during discovery at Mead startup (see meadcore.New). The sidecar
// at metaPath is loaded; the process is registered in the runner's
// in-memory map. If the sidecar reports exited=false and the pid is
// still alive in the OS, a watcher goroutine polls until the pid
// dies and then updates the sidecar with synthesized exit info
// (exit_code=-1, run_err describing the orphan recovery). If the
// pid is already dead at adoption time, the sidecar is updated
// immediately.
//
// Returns the adopted Process so the caller can introspect or chain.
// Idempotent on the runID: re-adopting the same sidecar replaces the
// in-memory entry but does NOT touch the disk file.
func (r *Runner) Adopt(metaPath string) (*Process, error) {
	s, err := readSidecar(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read sidecar: %w", err)
	}
	p := &Process{
		id:        s.ID,
		bottleID:  s.BottleID,
		argv:      append([]string(nil), s.Argv...),
		startedAt: parseTime(s.StartedAt),
		logPath:   s.LogPath,
		metaPath:  metaPath,
		pid:       s.PID,
		done:      make(chan struct{}),
		exited:    s.Exited,
		exitedAt:  parseTime(s.ExitedAt),
		exitCode:  s.ExitCode,
	}
	if s.RunErr != "" {
		p.runErr = errors.New(s.RunErr)
	}

	r.mu.Lock()
	r.processes[p.id] = p
	r.mu.Unlock()

	if p.exited {
		close(p.done) // already exited — done channel ready immediately
		return p, nil
	}

	// Sidecar says not exited. Check if the pid is actually alive.
	if !pidAlive(p.pid) {
		// Process died while Mead was offline. Synthesize the exit.
		p.mu.Lock()
		p.exited = true
		p.exitedAt = time.Now().UTC()
		p.exitCode = -1
		p.runErr = errors.New("process exited while Mead was not running (synthesized)")
		p.mu.Unlock()
		_ = p.writeSidecar()
		close(p.done)
		return p, nil
	}

	// Alive — spawn a watcher goroutine. We can't cmd.Wait() on a
	// process we didn't spawn, so we poll. 1s cadence is plenty
	// granular — these are wineboot/winetricks/install-style
	// long-runners, sub-second exit precision doesn't matter.
	go r.watchOrphan(p)
	return p, nil
}

// watchOrphan polls an adopted process's pid until it exits, then
// records synthesized exit info and signals done.
func (r *Runner) watchOrphan(p *Process) {
	const interval = time.Second
	for {
		time.Sleep(interval)
		if pidAlive(p.pid) {
			continue
		}
		p.mu.Lock()
		if p.exited {
			p.mu.Unlock()
			return // Kill() may have already set this — race-safe
		}
		p.exited = true
		p.exitedAt = time.Now().UTC()
		p.exitCode = -1
		p.runErr = errors.New("process exited (adopted; exit code unavailable)")
		p.mu.Unlock()
		_ = p.writeSidecar()
		close(p.done)
		return
	}
}

// Get returns the process for a given RunID, or nil + false if it
// isn't tracked. After Spawn() returns the process is immediately
// queryable; processes remain in the registry after they exit (so
// their logs stay accessible) until Forget() is called.
func (r *Runner) Get(id RunID) (*Process, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.processes[id]
	return p, ok
}

// List returns all tracked processes (alive + exited), ordered by
// StartedAt ascending.
func (r *Runner) List() []*Process {
	r.mu.RLock()
	out := make([]*Process, 0, len(r.processes))
	for _, p := range r.processes {
		out = append(out, p)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].startedAt.Before(out[j].startedAt) })
	return out
}

// Forget removes a process from the registry. The on-disk log file
// stays put. Idempotent — unknown ids are a no-op.
func (r *Runner) Forget(id RunID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.processes, id)
}

// composeEnv returns base with overrides applied. Overrides win on
// key collision.
func composeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(overrides))
	skip := make(map[string]bool, len(overrides))
	for k := range overrides {
		skip[k] = true
	}
	for _, kv := range base {
		eq := -1
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				eq = i
				break
			}
		}
		if eq == -1 || skip[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// cappedBuffer is a bytes.Buffer with an upper bound. Writes past the
// cap are silently dropped — the caller is interested in the head of
// the output (where the actionable error message lives) far more than
// the tail, and dropping is preferable to an unbounded allocation.
type cappedBuffer struct {
	b   bytes.Buffer
	Cap int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.Cap - c.b.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return c.b.Write(p)
}

func (c *cappedBuffer) Bytes() []byte { return c.b.Bytes() }

// maxSpawnLogBytes bounds a detached process's on-disk log. 64 MiB is
// far more than any real diagnostic needs but stops a runaway log from
// filling the disk.
const maxSpawnLogBytes = 64 << 20

// cappedWriter forwards to w until cap bytes have been written, then
// drops further data after emitting a one-time truncation marker. Used
// to bound the detached Spawn log file. NOT safe for concurrent use —
// the single exec output copier (Stdout==Stderr) is the only writer.
type cappedWriter struct {
	w         io.Writer
	cap       int
	written   int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.written >= c.cap {
		return len(p), nil // already full — drop, report consumed
	}
	if c.written+len(p) > c.cap {
		n, err := c.w.Write(p[:c.cap-c.written])
		c.written += n
		if !c.truncated {
			c.truncated = true
			_, _ = io.WriteString(c.w, "\n[mead: log truncated at 64 MiB cap; further output dropped]\n")
		}
		if err != nil {
			return n, err
		}
		return len(p), nil // dropped the tail, but the write "succeeded"
	}
	n, err := c.w.Write(p)
	c.written += n
	return n, err
}
