// Package apps orchestrates installing and launching Windows
// executables inside a Mead bottle.
//
// Both operations boil down to "spawn wine with the right argv +
// WINEPREFIX environment + a place to write logs" — this package
// composes wine.Locator, bottles.Manager, store.Store, and the
// detached runner.Spawn() to do that.
//
// v0.2 keeps no per-app metadata of its own — apps.list returns
// nothing meaningful yet, because the question of "what apps are in
// a bottle?" is genuinely hard (Wine doesn't track this; we'd need
// to scan Program Files, parse the Windows registry, or watch
// installs in real time). The MCP surface is wired so future
// discovery work slots in without breaking the contract.
//
// What we DO track: every install or launch produces a runner.Process
// the agent can poll via process.logs. That's the diagnostic surface
// the whole product narrative rests on.
package apps

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// Manager is the entry point for app-level operations. Construct one
// per process; meadcore.Core owns it.
type Manager struct {
	store   *store.Store
	bottles *bottles.Manager
	wine    *wine.Locator
	runner  *runner.Runner
}

// New returns a Manager wired to the given dependencies. All four
// must be non-nil.
func New(s *store.Store, b *bottles.Manager, w *wine.Locator, r *runner.Runner) *Manager {
	return &Manager{store: s, bottles: b, wine: w, runner: r}
}

// Install kicks off a Windows installer inside the named bottle and
// returns the *runner.Process tracking it. The caller (typically the
// MCP handler) returns the process's RunID to the agent so the agent
// can poll process.logs to watch the install.
//
// installerPath is interpreted on the HOST: it's a path the user
// passed pointing at a downloaded .exe on the Mac filesystem. Wine
// then runs that executable in the bottle's prefix context. The path
// is converted to absolute so wine sees a stable reference even after
// cwd changes.
func (m *Manager) Install(ctx context.Context, bottleID, installerPath string) (*runner.Process, error) {
	if strings.TrimSpace(installerPath) == "" {
		return nil, ErrInstallerPathRequired
	}
	abs, err := filepath.Abs(installerPath)
	if err != nil {
		return nil, fmt.Errorf("resolve installer path: %w", err)
	}
	spec, err := m.specForBottle(bottleID, []string{abs})
	if err != nil {
		return nil, err
	}
	return m.runner.Spawn(ctx, spec)
}

// Launch starts an already-installed app inside a bottle. exePath is
// interpreted RELATIVE to the bottle's prefix drive_c — so launching
// Notepad in a bottle looks like:
//
//	Launch(ctx, bottleID, "windows/notepad.exe")
//
// Internally we translate that to <prefix>/drive_c/windows/notepad.exe
// and pass the resolved absolute path to wine. This sidesteps Wine's
// own path-translation rules (Z: drives, dosdevices/), which differ
// across wine versions and confuse the agent.
//
// Absolute exePaths are also accepted — they're passed through as-is
// (the agent might want to launch a host binary from inside the
// bottle, e.g. for diagnostics).
func (m *Manager) Launch(ctx context.Context, bottleID, exePath string) (*runner.Process, error) {
	if strings.TrimSpace(exePath) == "" {
		return nil, ErrExePathRequired
	}
	resolved := exePath
	if !filepath.IsAbs(exePath) {
		prefix, err := m.store.PrefixDir(bottleID)
		if err != nil {
			return nil, err
		}
		resolved = filepath.Join(prefix, "drive_c", exePath)
	}
	spec, err := m.specForBottle(bottleID, []string{resolved})
	if err != nil {
		return nil, err
	}
	return m.runner.Spawn(ctx, spec)
}

// List returns the apps known to live in the given bottle. v0.2
// returns an empty slice — auto-discovery is a v0.3 concern (parse
// the registry, scan Program Files, watch installs). The MCP layer
// surfaces this as `apps.list` returning {apps: []} which is a
// well-formed response, not an error, so agent code can branch on
// length without special-casing "not implemented."
func (m *Manager) List(bottleID string) ([]App, error) {
	if _, err := m.bottles.Get(bottleID); err != nil {
		return nil, err
	}
	return []App{}, nil
}

// App is the placeholder shape for an installed Windows program.
// Fields are aspirational — none populated until List() does real
// discovery. The shape is committed so future implementations can
// extend it without breaking the MCP contract.
type App struct {
	// Name is the user-visible label (e.g. "Notepad", "Warcraft III").
	Name string `json:"name"`
	// ExePath is relative to <prefix>/drive_c — the same shape Launch
	// accepts.
	ExePath string `json:"exe_path"`
}

// specForBottle composes the runner.Spec for invoking wine against a
// bottle: argv prefixed with the wine binary, WINEPREFIX env, log
// path under the bottle's logs/ dir. Shared between Install and
// Launch.
func (m *Manager) specForBottle(bottleID string, wineArgs []string) (runner.Spec, error) {
	b, err := m.bottles.Get(bottleID)
	if err != nil {
		return runner.Spec{}, err
	}
	winePath, err := m.wine.Path()
	if err != nil {
		return runner.Spec{}, fmt.Errorf("locate wine: %w", err)
	}
	prefix, err := m.store.PrefixDir(b.ID)
	if err != nil {
		return runner.Spec{}, err
	}
	logsDir, err := m.store.LogsDir(b.ID)
	if err != nil {
		return runner.Spec{}, err
	}
	// The RunID we'd use for the log filename isn't known until
	// Runner.Spawn returns; pre-generate the path with a placeholder
	// that the runner will treat as the actual log target. Since
	// Runner doesn't know the RunID before assigning one either, we
	// let it generate the ID and use a deterministic timestamp-based
	// filename. Simpler: use a temp filename inside logsDir.
	//
	// Implementation: pass the runID-less path; the runner overwrites
	// the placeholder once the RunID is known. (See runner_spawn for
	// the convention.) For v0.2 we sidestep this by using a
	// timestamp-based name guaranteed unique within ~ns resolution
	// per process.
	logPath := filepath.Join(logsDir, uniqueLogName())

	return runner.Spec{
		Argv:     append([]string{winePath}, wineArgs...),
		Env:      map[string]string{"WINEPREFIX": prefix},
		BottleID: b.ID,
		LogPath:  logPath,
	}, nil
}
