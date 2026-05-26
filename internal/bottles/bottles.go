// Package bottles owns the lifecycle of a Wine prefix: create, clone,
// rename, delete. A "bottle" in Mead's language is a self-contained Wine
// prefix plus the metadata Mead tracks about it (display name, creation
// time, originating Wine version, env overrides, dll overrides).
//
// First-class bottle UX is a deliberate v0.1 decision (see CLAUDE.md
// "scope"): users explicitly create a bottle, then install apps into
// it. The agent reasons about bottles as containers — this is what makes
// the MCP surface clean ("install Foo.exe INTO bottle-X" rather than
// Whisky's "install Foo.exe and we'll pick a bottle for you").
//
// This package does NOT spawn Wine (runner does that), persist metadata
// (store does that), or locate the Wine binary (wine does that). It
// composes those three to implement the bottle operations.
package bottles

import "time"

// Bottle is the in-memory representation of a single Wine prefix. Mutate
// only via Manager methods; direct field writes won't persist.
type Bottle struct {
	ID          string            // stable opaque id (uuid v7 in production); the prefix dir name
	Name        string            // user-visible label
	CreatedAt   time.Time         // wall clock; tagged at creation, never updated
	WineVersion string            // version string captured from `wine --version` at creation
	EnvOverrides map[string]string // per-bottle env vars (DXVK_HUD, WINEDEBUG, …)
}

// Manager is the entry point for bottle operations. Construct one per
// process (typically owned by the app singleton, mirroring wc3-forge's
// forge.Session).
type Manager struct {
	// fields wired up in v0.1 implementation: store.Store, wine.Locator,
	// and a sync.RWMutex for concurrent access.
}

// New returns a Manager with no bottles loaded. Call Load() to hydrate
// from the on-disk store.
func New() *Manager { return &Manager{} }

// Create makes a new prefix and registers a Bottle for it. Name is the
// user-visible label; the on-disk ID is generated. Returns the freshly
// created Bottle so the caller (typically the MCP handler) can echo it
// back to the agent.
func (m *Manager) Create(name string) (*Bottle, error) {
	return nil, errNotImplemented
}

// List returns every known bottle, ordered by creation time (oldest
// first). Safe to call from any goroutine.
func (m *Manager) List() ([]*Bottle, error) {
	return nil, errNotImplemented
}

// Get returns the Bottle with the given ID, or ErrBottleNotFound.
func (m *Manager) Get(id string) (*Bottle, error) {
	return nil, errNotImplemented
}

// Delete removes the bottle's prefix directory and metadata. Irreversible;
// the caller is expected to confirm with the user before invoking.
func (m *Manager) Delete(id string) error {
	return errNotImplemented
}
