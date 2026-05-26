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

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// Core is the singleton wired up at app startup. Hand it to the bridge
// (via RegisterAll) and to the Wails App struct so both surfaces share it.
type Core struct {
	Store   *store.Store
	Wine    *wine.Locator
	Bottles *bottles.Manager
	Runner  *runner.Runner
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
	r := runner.New()
	return &Core{
		Store:   s,
		Wine:    w,
		Runner:  r,
		Bottles: bottles.New(s, w, r),
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
