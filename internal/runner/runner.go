// Package runner is Mead's process supervisor. It spawns Wine processes
// targeting a specific bottle, captures their stdout/stderr into the
// store's per-bottle log directory, tracks PIDs for kill/list, and
// emits events (process_started, process_exited, process_output) the
// app's event bus forwards to both the GUI and the MCP bridge.
//
// Why a separate supervisor: every Mead operation that mutates the user
// environment — installing an app, launching it, invoking winetricks,
// running a one-shot wine command for diagnostics — ultimately boils
// down to "spawn a Wine process against a prefix and watch what
// happens." Centralizing that into one place means logging, env
// composition, and the agent's process_logs/process_kill MCP surface
// all share the same code path.
//
// This package does NOT decide WHICH binary to spawn (caller passes the
// argv; wine.Locator + bottles.Manager compose what gets passed). It
// just runs things and remembers them.
package runner

import "context"

// Runner is the supervisor. One per app process.
type Runner struct{}

// New returns an empty Runner. v0.1 will wire it to a store for log
// persistence; for the stub no construction args are needed.
func New() *Runner { return &Runner{} }

// Spec is the description of a process to launch. The runner owns
// stdout/stderr capture and bottle-relative env composition.
type Spec struct {
	BottleID string            // selects the WINEPREFIX
	Argv     []string          // [wineBinary, ".../setup.exe", "/S", ...] — caller composes
	Env      map[string]string // adds to (overrides) the bottle's env_overrides
	Detach   bool              // true = fire-and-forget (apps_launch); false = wait + capture (apps_install)
}

// Run launches the process described by spec. For detached specs the
// returned RunID identifies the process for later process_logs /
// process_kill calls; the returned error is non-nil only if the spawn
// itself failed. For non-detached specs, Run blocks until the process
// exits and the error reflects the exit status.
//
// Pass ctx to support agent-initiated cancellation; closing ctx kills
// the process tree.
func (r *Runner) Run(ctx context.Context, spec Spec) (RunID, error) {
	return "", errNotImplemented
}

// RunID is the opaque identifier the supervisor hands back. The on-disk
// log file is named with this ID, so the agent's process_logs(id)
// resolves trivially.
type RunID string
