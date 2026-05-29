// Package meadcore is the editor-core analog for Mead: the application
// singleton that owns the bottle manager, wine locator, runner, and the
// store, plus the single registration point for every MCP method.
//
// Two control surfaces (the GUI and external MCP clients) converge here
// so every operation flows through the same mutator and emits the same
// events. Adding a new MCP tool means: write `handle<Foo>`, add
// `reg("foo.bar", handleFoo)` to RegisterAll, and call into the
// underlying manager from inside the handler.
package meadcore

import (
	"fmt"
	"log"

	"github.com/StephenSHorton/mead/internal/apps"
	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/registry"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
	"github.com/StephenSHorton/mead/internal/winetricks"
)

// Core is the singleton wired up at app startup. Hand it to the bridge
// (via RegisterAll) and to the Wails App struct so both surfaces share it.
type Core struct {
	Store      *store.Store
	Wine       *wine.Locator
	Winetricks *winetricks.Locator
	Bottles    *bottles.Manager
	Runner     *runner.Runner
	Apps       *apps.Manager
	Registry   *registry.Manager
}

// New constructs a fully-wired core. If the store can't be opened
// (e.g. ~/Library is read-only), Mead degrades to a bridge-only mode
// where bottles operations fail but the bridge still answers liveness
// pings — better than refusing to start.
//
// Call RegisterAll(b, c) afterward to expose the operations on the
// MCP bridge.
func New() (*Core, error) {
	s, err := store.Open("")
	if err != nil {
		// Log and continue with a nil store; downstream handlers report
		// the error per-call so the agent can debug.
		log.Printf("meadcore: store.Open failed: %v (bottles operations will fail; bridge stays up)", err)
		return &Core{
			Wine:   wine.New(),
			Runner: runner.New(),
		}, nil
	}
	w := wine.New()
	wt := winetricks.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	// Discover and re-register processes from previous Mead sessions.
	// Each spawned process writes a <logPath>.json sidecar; here we
	// scan every bottle's logs/ dir and Adopt them so:
	//  - past-session log files are accessible via process.logs
	//  - still-running orphans (e.g. a long-running winetricks
	//    install that outlived a Mead restart) get a watcher
	//    re-attached and remain killable via process.kill
	// Discovery failures are logged but never fatal; the rest of
	// Mead works regardless.
	if err := adoptExistingProcesses(s, r); err != nil {
		log.Printf("meadcore: process discovery: %v (continuing)", err)
	}
	return &Core{
		Store:      s,
		Wine:       w,
		Winetricks: wt,
		Runner:     r,
		Bottles:    bm,
		Apps:       apps.New(s, bm, w, wt, r),
		Registry:   registry.New(s, bm, w, r),
	}, nil
}

// requireBottles returns an error suitable for the MCP layer when the
// bottle manager isn't available (store didn't open). Handlers call
// this before touching c.Bottles.
func (c *Core) requireBottles() error {
	if c.Bottles == nil {
		return fmt.Errorf("bottles unavailable: store failed to open at startup (see app log)")
	}
	return nil
}

// requireApps mirrors requireBottles for the apps subsystem.
func (c *Core) requireApps() error {
	if c.Apps == nil {
		return fmt.Errorf("apps unavailable: store failed to open at startup (see app log)")
	}
	return nil
}

// requireRegistry mirrors requireBottles for the registry subsystem.
func (c *Core) requireRegistry() error {
	if c.Registry == nil {
		return fmt.Errorf("registry unavailable: store failed to open at startup (see app log)")
	}
	return nil
}
