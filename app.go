package main

import (
	"context"
	"errors"
	"log"

	"github.com/StephenSHorton/mead/internal/bridge"
	"github.com/StephenSHorton/mead/internal/meadcore"
	"github.com/StephenSHorton/mead/internal/runner"
)

// errBottlesUnavailable is returned to the frontend when the bottle
// manager isn't wired (store failed to open at startup). The frontend
// maps this to a "Mead couldn't open its data directory" empty state.
var errBottlesUnavailable = errors.New("bottles unavailable (see app log)")

// errProcessNotFound is the frontend-facing analog of
// runner.ErrProcessNotFound — Wails errors surface to JS as the
// .message string, so we want something self-explanatory.
var errProcessNotFound = errors.New("process not found")

// runnerID is a typed alias so we don't sprinkle string casts through
// the bindings. Wails surfaces the underlying string in JS untouched.
func runnerID(s string) runner.RunID { return runner.RunID(s) }

// App is the Wails-bound surface. Methods on App that don't start with a
// lowercase letter become JS-callable from the frontend via
// `wailsjs/go/main/App.{Method}`. The MCP bridge has its own surface
// (see internal/meadcore/handlers.go) — the two share the same Core
// singleton so a JSON-RPC bottles.create and a UI click flow through
// the same mutator.
type App struct {
	ctx    context.Context
	core   *meadcore.Core
	bridge *bridge.Bridge
}

// NewApp constructs the app singleton and the bridge but does NOT start
// the bridge yet. Wails' OnStartup is where listeners get bound — see
// startup below — so that crashes during Wails init don't leak a
// listening port + lockfile.
func NewApp() *App {
	c, err := meadcore.New()
	if err != nil {
		// meadcore.New degrades gracefully on store failures — the
		// only path that returns an error here would be a future
		// fatal-on-startup condition, which we'd want to know about.
		log.Printf("meadcore.New: %v", err)
	}
	b := bridge.New(bridge.Config{AppName: "mead"})
	meadcore.RegisterAll(b, c)
	return &App{core: c, bridge: b}
}

// startup is invoked by Wails once the window is ready. We bring up the
// MCP bridge here so its port is reachable from the moment the GUI
// renders. If the bridge fails to bind, we log and continue — Mead as a
// GUI is still useful without agent access.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.bridge.Start(); err != nil {
		log.Printf("MCP bridge failed to start: %v (continuing without agent access)", err)
		return
	}
	log.Printf("MCP bridge listening on 127.0.0.1:%d (token: %s…)", a.bridge.Port(), a.bridge.TokenShort())
}

// shutdown is invoked by Wails on window close. The bridge cleans up its
// lockfile and stops accepting connections.
func (a *App) shutdown(_ context.Context) {
	if a.bridge != nil {
		a.bridge.Stop()
	}
}

// BridgePort exposes the bridge's listening port to the frontend so the
// in-app Agent Console can display it. The frontend never connects to
// the bridge itself — that's for external MCP clients — it just shows
// the user where to point a Claude Code mcp-config entry.
func (a *App) BridgePort() int { return a.bridge.Port() }

// BridgeTokenShort exposes the first 8 chars of the auth token for the
// Agent Console header. Mirrors the wc3-forge behavior so we don't
// accidentally surface the full secret in a screenshotted/streamed UI.
func (a *App) BridgeTokenShort() string { return a.bridge.TokenShort() }

// ---- Bottles bindings exposed to the frontend ---------------------------
//
// These thin wrappers exist so the Svelte side can drive the same code
// path the MCP bridge uses. Each delegates to the core's bottle manager,
// returning the same shape the bridge does so the JS side can share a
// single Bottle type.

// BottleSummary is the JSON-friendly shape the frontend sees. Mirrors
// bottleSummary in meadcore/handlers.go but lives here because the
// Wails binding generator scans this package.
type BottleSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at,omitempty"`
	WineVersion string `json:"wine_version,omitempty"`
}

// ListBottles returns every known bottle, oldest first. Errors come
// through to the frontend as Promise rejections.
func (a *App) ListBottles() ([]BottleSummary, error) {
	if a.core == nil || a.core.Bottles == nil {
		return []BottleSummary{}, nil
	}
	bs, err := a.core.Bottles.List()
	if err != nil {
		return nil, err
	}
	out := make([]BottleSummary, 0, len(bs))
	for _, b := range bs {
		out = append(out, BottleSummary{
			ID:          b.ID,
			Name:        b.Name,
			CreatedAt:   b.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			WineVersion: b.WineVersion,
		})
	}
	return out, nil
}

// CreateBottle materializes a new Wine prefix. Blocking (wineboot
// --init can take 5-15 seconds on a warm install); the frontend
// renders a spinner while awaiting the Promise. The context here is
// just context.Background — UI-initiated cancellation isn't wired in
// v0.2.
func (a *App) CreateBottle(name string) (*BottleSummary, error) {
	if a.core == nil || a.core.Bottles == nil {
		return nil, errBottlesUnavailable
	}
	b, err := a.core.Bottles.Create(a.ctx, name)
	if err != nil {
		return nil, err
	}
	return &BottleSummary{
		ID:          b.ID,
		Name:        b.Name,
		CreatedAt:   b.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		WineVersion: b.WineVersion,
	}, nil
}

// DeleteBottle removes a bottle by id. Irreversible. The frontend is
// expected to confirm with the user before calling.
func (a *App) DeleteBottle(id string) error {
	if a.core == nil || a.core.Bottles == nil {
		return errBottlesUnavailable
	}
	return a.core.Bottles.Delete(id)
}

// ---- Apps + processes bindings exposed to the frontend ------------------
//
// These mirror the MCP surface (apps.install, apps.launch, process.list,
// etc.) but as method calls on the Wails App struct so the GUI can drive
// them without speaking JSON-RPC. They return the same payload shapes the
// bridge does, formatted for the Svelte side.

// ProcessSummary is the JSON-friendly shape the frontend uses to render
// the process list and the live log viewer. Mirrors the bridge's
// processSummary; duplicated here because Wails' binding generator
// scans the main package, not internal/.
type ProcessSummary struct {
	RunID     string   `json:"run_id"`
	BottleID  string   `json:"bottle_id,omitempty"`
	Argv      []string `json:"argv"`
	StartedAt string   `json:"started_at"`
	LogPath   string   `json:"log_path,omitempty"`
	Exited    bool     `json:"exited"`
	ExitedAt  string   `json:"exited_at,omitempty"`
	ExitCode  int      `json:"exit_code,omitempty"`
	RunErr    string   `json:"run_err,omitempty"`
}

// LogsChunk is the response shape from ProcessLogs(). The frontend
// polls: pass offset=NextOffset back on the next call until Exited is
// true and Bytes is empty.
type LogsChunk struct {
	Bytes      string `json:"bytes"`
	NextOffset int64  `json:"next_offset"`
	Exited     bool   `json:"exited"`
}

// InstallApp spawns a Windows installer inside the named bottle and
// returns the RunID the frontend uses to poll logs.
func (a *App) InstallApp(bottleID, installerPath string) (string, error) {
	if a.core == nil || a.core.Apps == nil {
		return "", errBottlesUnavailable
	}
	proc, err := a.core.Apps.Install(a.ctx, bottleID, installerPath)
	if err != nil {
		return "", err
	}
	return string(proc.ID()), nil
}

// LaunchApp starts an already-installed app inside a bottle. exePath
// is either absolute or relative to <prefix>/drive_c — same contract
// as the apps.launch MCP method.
func (a *App) LaunchApp(bottleID, exePath string) (string, error) {
	if a.core == nil || a.core.Apps == nil {
		return "", errBottlesUnavailable
	}
	proc, err := a.core.Apps.Launch(a.ctx, bottleID, exePath)
	if err != nil {
		return "", err
	}
	return string(proc.ID()), nil
}

// ListProcesses returns every tracked process (alive + exited),
// optionally filtered by bottleID. Empty bottleID returns all.
func (a *App) ListProcesses(bottleID string) ([]ProcessSummary, error) {
	if a.core == nil || a.core.Runner == nil {
		return []ProcessSummary{}, nil
	}
	ps := a.core.Runner.List()
	out := make([]ProcessSummary, 0, len(ps))
	for _, p := range ps {
		if bottleID != "" && p.BottleID() != bottleID {
			continue
		}
		s := ProcessSummary{
			RunID:     string(p.ID()),
			BottleID:  p.BottleID(),
			Argv:      p.Argv(),
			StartedAt: p.StartedAt().UTC().Format("2006-01-02T15:04:05.000Z"),
			LogPath:   p.LogPath(),
			Exited:    p.Exited(),
			ExitCode:  p.ExitCode(),
			RunErr:    p.RunErr(),
		}
		if t := p.ExitedAt(); !t.IsZero() {
			s.ExitedAt = t.UTC().Format("2006-01-02T15:04:05.000Z")
		}
		out = append(out, s)
	}
	return out, nil
}

// ProcessLogs reads up to limit bytes from a process's log file
// starting at offset. The frontend's log viewer polls this in a loop
// while Exited is false (and once more after to drain the tail).
func (a *App) ProcessLogs(runID string, offset int64, limit int) (*LogsChunk, error) {
	if a.core == nil || a.core.Runner == nil {
		return nil, errBottlesUnavailable
	}
	proc, ok := a.core.Runner.Get(runnerID(runID))
	if !ok {
		return nil, errProcessNotFound
	}
	data, next, exited, err := proc.Logs(offset, limit)
	if err != nil {
		return nil, err
	}
	return &LogsChunk{Bytes: string(data), NextOffset: next, Exited: exited}, nil
}

// KillProcess sends SIGTERM (escalating to SIGKILL after 2s) to the
// running process. No-op if it's already exited.
func (a *App) KillProcess(runID string) error {
	if a.core == nil || a.core.Runner == nil {
		return errBottlesUnavailable
	}
	proc, ok := a.core.Runner.Get(runnerID(runID))
	if !ok {
		return errProcessNotFound
	}
	return proc.Kill()
}
