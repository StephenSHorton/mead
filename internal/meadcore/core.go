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

// New constructs a fully-wired core. Call RegisterAll(b, c) afterward to
// expose its operations on the MCP bridge.
//
// v0.1 returns a Core whose dependencies are present but mostly stub-
// returning. That's deliberate: we want the wiring shape right before
// filling in behavior, so the agent surface stays stable as bodies land.
func New() *Core {
	// store.Open is allowed to fail (no perms on ~/Library, etc.) but
	// the stub currently always fails — accept that for now and let
	// downstream handlers report ErrNotImplemented. When store.Open
	// gets a real body, propagate its error properly.
	s, _ := store.Open("")
	return &Core{
		Store:   s,
		Wine:    wine.New(),
		Bottles: bottles.New(),
		Runner:  runner.New(),
	}
}
