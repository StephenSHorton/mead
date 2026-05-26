// Package bottles owns the lifecycle of a Wine prefix: create, list,
// get, delete. A "bottle" in Mead's language is a self-contained Wine
// prefix plus the metadata Mead tracks about it (display name, creation
// time, originating Wine version, env overrides).
//
// First-class bottle UX is a deliberate v0.1 decision (see CLAUDE.md
// "scope"): users explicitly create a bottle, then install apps into
// it. The agent reasons about bottles as containers — this is what
// makes the MCP surface clean ("install Foo.exe INTO bottle-X" rather
// than Whisky's "install Foo.exe and we'll pick a bottle for you").
//
// This package composes store (metadata persistence), wine (binary
// location), and runner (process spawning). It does NOT spawn Wine
// directly or persist JSON itself.
package bottles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"

	"github.com/google/uuid"
)

// Bottle is a re-export of store.Bottle so callers outside this package
// can refer to bottles.Bottle without importing the store package.
type Bottle = store.Bottle

// Manager is the entry point for bottle operations. Construct one per
// process (the meadcore.Core singleton owns it).
type Manager struct {
	store   *store.Store
	wine    *wine.Locator
	runner  *runner.Runner
	mu      sync.Mutex // serializes Create + Delete against each other
}

// New returns a Manager wired to the given dependencies. All three
// must be non-nil; the constructor doesn't validate (the meadcore.Core
// constructor is responsible for filling them).
func New(s *store.Store, w *wine.Locator, r *runner.Runner) *Manager {
	return &Manager{store: s, wine: w, runner: r}
}

// Create makes a new prefix and registers a Bottle for it. Steps:
//
//  1. Validate name (non-empty, unique).
//  2. Locate the Wine binary; capture its version for the metadata.
//  3. Generate a UUID id.
//  4. Run `wineboot --init` with WINEPREFIX=<bottleDir>/prefix to
//     materialize a fresh prefix.
//  5. Persist metadata.json.
//
// On wineboot failure, the partially-created bottle directory is
// removed before returning so the user doesn't see a phantom entry
// in the list. The error includes wineboot's captured output to make
// failures actionable.
//
// Holds m.mu for the duration to keep concurrent name-uniqueness
// checks honest — two simultaneous "create Diablo II" requests
// shouldn't both succeed.
func (m *Manager) Create(ctx context.Context, name string) (*Bottle, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBottleNameRequired
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	taken, err := m.store.NameTaken(name)
	if err != nil {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if taken {
		return nil, ErrBottleNameConflict
	}

	winePath, err := m.wine.Path()
	if err != nil {
		return nil, fmt.Errorf("locate wine: %w", err)
	}
	wineVersion, _ := m.wine.Version() // Best-effort; we don't fail Create just because --version stuttered.

	id := uuid.NewString()
	prefixDir, err := m.store.PrefixDir(id)
	if err != nil {
		// Shouldn't happen — uuid.NewString returns a valid UUID — but
		// defending against future changes to the id source.
		return nil, fmt.Errorf("resolve prefix dir: %w", err)
	}

	// wineboot --init creates the prefix directory tree (system.reg,
	// drive_c/, dosdevices/, …). WINEPREFIX must be absolute; the
	// store layer guarantees that.
	res, err := m.runner.Run(ctx, runner.Spec{
		Argv: []string{winePath, "wineboot", "--init"},
		Env:  map[string]string{"WINEPREFIX": prefixDir},
	})
	if err != nil {
		// Roll back the partial prefix so the user doesn't see a
		// half-baked bottle in the list.
		_ = m.store.DeleteBottle(id)
		return nil, fmt.Errorf("wineboot --init: %w\noutput:\n%s", err, truncate(res.Output, 4096))
	}

	b := &Bottle{
		ID:          id,
		Name:        name,
		CreatedAt:   time.Now().UTC(),
		WineVersion: wineVersion,
	}
	if err := m.store.SaveBottle(b); err != nil {
		_ = m.store.DeleteBottle(id)
		return nil, fmt.Errorf("save bottle: %w", err)
	}
	return b, nil
}

// List returns every known bottle, oldest first. Safe to call from any
// goroutine.
func (m *Manager) List() ([]*Bottle, error) {
	return m.store.LoadBottles()
}

// Get returns the bottle with the given id. Returns ErrBottleNotFound
// (wrapping the store error) for an unknown id; an io error otherwise.
func (m *Manager) Get(id string) (*Bottle, error) {
	b, err := m.store.LoadBottle(id)
	if err != nil {
		if errors.Is(err, store.ErrBottleNotFound) {
			return nil, ErrBottleNotFound
		}
		return nil, err
	}
	return b, nil
}

// Delete removes the bottle's prefix directory and metadata.
// Irreversible; the caller is expected to confirm with the user before
// invoking. Idempotent — deleting an unknown id returns nil.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.DeleteBottle(id)
}

// SetEnv sets (or, when value is empty, unsets) one entry in the
// bottle's env_overrides map. Persisted to metadata.json so the env
// survives Mead restarts. apps.Install / apps.Launch fold these into
// the spawned wine process's environment alongside WINEPREFIX.
//
// Mutex held for the read-modify-write so concurrent SetEnv calls
// don't race on the metadata.
func (m *Manager) SetEnv(id, key, value string) error {
	if key == "" {
		return ErrEnvKeyRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := m.store.LoadBottle(id)
	if err != nil {
		if errors.Is(err, store.ErrBottleNotFound) {
			return ErrBottleNotFound
		}
		return err
	}
	if b.EnvOverrides == nil && value != "" {
		b.EnvOverrides = make(map[string]string)
	}
	if value == "" {
		delete(b.EnvOverrides, key)
	} else {
		b.EnvOverrides[key] = value
	}
	return m.store.SaveBottle(b)
}

// EnvOverrides returns a copy of the bottle's persisted env-overrides
// map. Returns nil when none are set. Used by callers that need to
// compose env (apps.Install/Launch) and by the MCP env.get handler.
func (m *Manager) EnvOverrides(id string) (map[string]string, error) {
	b, err := m.store.LoadBottle(id)
	if err != nil {
		if errors.Is(err, store.ErrBottleNotFound) {
			return nil, ErrBottleNotFound
		}
		return nil, err
	}
	if len(b.EnvOverrides) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(b.EnvOverrides))
	for k, v := range b.EnvOverrides {
		out[k] = v
	}
	return out, nil
}

// truncate clips b to at most n bytes for inclusion in user-facing
// error messages. Long wineboot output is rarely actionable past the
// first few KiB.
func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return append(b[:n], []byte("\n[truncated]")...)
}
