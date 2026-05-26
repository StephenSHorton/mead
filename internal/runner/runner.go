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

	// CommandContext so an external cancel kills the spawned process.
	// We also pre-build a derived context whose cancel we keep so
	// Process.Kill() can fire it.
	derived, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(derived, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = composeEnv(os.Environ(), spec.Env)
	cmd.Stdout = f
	cmd.Stderr = f
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
		cmd:       cmd,
		cancel:    cancel,
		done:      make(chan struct{}),
		logFile:   f,
	}

	r.mu.Lock()
	r.processes[p.id] = p
	r.mu.Unlock()

	// Reaper goroutine: wait for the process to exit, record state,
	// close the log file, signal done.
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
		close(p.done)
	}()

	return p, nil
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
