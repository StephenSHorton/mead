// Package store persists Mead's user-visible state: bottle metadata, app
// shortcuts per bottle, install history, environment overrides.
//
// Storage layout (under $HOME/Library/Application Support/Mead/):
//
//	bottles/
//	  <bottle-id>/
//	    prefix/             # the Wine prefix (drive_c, dosdevices, system.reg, ...)
//	    metadata.json       # {id, name, created_at, wine_version, env_overrides, ...}
//	    apps.json           # [{path, name, icon, last_launched_at, ...}]
//	    logs/
//	      <timestamp>-<run-id>.log
//	config.json             # top-level: default-wine path, last-active bottle, etc.
//
// This package owns reads + writes against that tree. It does NOT spawn
// Wine processes (that's runner), create prefixes (that's bottles), or
// locate the Wine binary (that's wine).
package store

// Store is the file-backed persistence layer. Construct one per process;
// it's safe to share across goroutines (operations serialize on a
// per-bottle mutex once we add concurrency, which v0.1 doesn't need yet).
type Store struct {
	root string // $HOME/Library/Application Support/Mead by default
}

// Open returns a Store backed by the given root directory. The directory
// is created on first use. Pass an empty string for the default.
func Open(root string) (*Store, error) {
	return nil, errNotImplemented
}

// Root returns the resolved storage root path. Useful for the UI when
// surfacing "where did Mead put my bottles?" to the user.
func (s *Store) Root() string { return s.root }
