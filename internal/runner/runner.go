// Package runner is Mead's process supervisor. v0.1 implements only the
// synchronous case — Run(ctx, spec) launches a process, waits for it to
// exit, captures combined stdout+stderr, and returns the captured output
// plus the exit error. That's enough to invoke wineboot --init from
// bottles.Create.
//
// The detached / streaming case (apps.launch, process.logs) is v0.2;
// the doc comment on Spec captures the planned shape so we don't lose
// it.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Runner is the supervisor. One per app process. The struct has no
// fields in v0.1 — when log persistence and the process-tracking map
// land they live here.
type Runner struct{}

// New returns an empty Runner.
func New() *Runner { return &Runner{} }

// Spec is the description of a process to launch. The runner composes
// the bottle-relative env (WINEPREFIX et al.) on top of os.Environ so
// the spawned process inherits the user's PATH but gets Mead's
// overrides on top.
type Spec struct {
	// Argv is the command line to execute. argv[0] is the binary path
	// (caller has already resolved it via wine.Locator). Subsequent
	// entries are arguments. Never invoked through a shell — no
	// quoting/escaping needed.
	Argv []string

	// Env is added (and overrides) the inherited os.Environ. Use this
	// for WINEPREFIX, DXVK_HUD, WINEDEBUG, etc.
	Env map[string]string

	// Detach controls whether Run waits for the process. v0.1 only
	// supports Detach=false; passing true returns ErrDetachUnsupported.
	Detach bool
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
func (r *Runner) Run(ctx context.Context, spec Spec) (*Result, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("runner.Run: empty argv")
	}
	if spec.Detach {
		return nil, ErrDetachUnsupported
	}

	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = composeEnv(os.Environ(), spec.Env)

	// Cap captured output so a chatty Wine subprocess can't blow up
	// memory. 1 MiB is plenty for a wineboot --init or a winetricks
	// step; longer-running spawns will switch to streaming in v0.2.
	var buf cappedBuffer
	buf.Cap = 1 << 20
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return &Result{ExitCode: exitCode, Output: buf.Bytes()}, fmt.Errorf("run %s: %w", spec.Argv[0], err)
	}
	return &Result{ExitCode: cmd.ProcessState.ExitCode(), Output: buf.Bytes()}, nil
}

// composeEnv returns base with overrides applied. Overrides win on
// key collision. Filter-then-append rather than O(n*m) replace so a
// large os.Environ stays cheap.
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
		// Split on first '=' to find the key.
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
