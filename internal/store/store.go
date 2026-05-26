// Package store persists Mead's user-visible state: bottle metadata, app
// shortcuts per bottle, install history, environment overrides.
//
// Storage layout (under $HOME/Library/Application Support/Mead/ by default):
//
//	bottles/
//	  <bottle-id>/
//	    metadata.json   # {id, name, created_at, wine_version, env_overrides, ...}
//	    prefix/         # the Wine prefix (drive_c, dosdevices, system.reg, ...)
//
// Bottle IDs are UUIDs (server-generated, never user-supplied). All
// methods that take an id validate it against UUIDIsh before touching
// the filesystem so a malicious or buggy caller can't path-traverse out
// of the bottles/ root.
//
// This package owns paths + IO + serialization. It does NOT spawn Wine
// processes (runner), locate the Wine binary (wine), or validate domain
// rules like name-uniqueness (bottles).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Bottle is the persistent representation of a Wine prefix Mead owns.
// The JSON shape is the wire contract for v0.1 — extending it means
// adding fields (and bumping a schema-version field, when we add one).
// Mutating an existing field's meaning is a migration.
type Bottle struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	CreatedAt    time.Time         `json:"created_at"`
	WineVersion  string            `json:"wine_version,omitempty"`
	EnvOverrides map[string]string `json:"env_overrides,omitempty"`
}

// Store is the file-backed persistence layer. Safe for concurrent use —
// individual file operations are atomic on the host filesystem (we
// write to a tempfile + rename) and we don't hold any in-memory caches
// that could go stale.
type Store struct {
	root string
}

// Open returns a Store rooted at the given directory. Pass an empty
// string for the default ($HOME/Library/Application Support/Mead). The
// root + bottles/ subdir are created if they don't exist; passing a
// path that exists but isn't a directory is an error.
func Open(root string) (*Store, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("user home: %w", err)
		}
		root = filepath.Join(home, "Library", "Application Support", "Mead")
	}
	if err := os.MkdirAll(filepath.Join(root, "bottles"), 0o755); err != nil {
		return nil, fmt.Errorf("create store root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("store root %q is not a directory", root)
	}
	return &Store{root: root}, nil
}

// Root returns the resolved storage root path. Useful for the UI when
// surfacing "where did Mead put my bottles?" to the user.
func (s *Store) Root() string { return s.root }

// BottleDir returns the on-disk directory for a bottle. Validates the
// id; returns "" + an error for invalid ids so callers can't accidentally
// pass an attacker-supplied id straight into filesystem operations.
func (s *Store) BottleDir(id string) (string, error) {
	if !UUIDIsh(id) {
		return "", fmt.Errorf("invalid bottle id %q", id)
	}
	return filepath.Join(s.root, "bottles", id), nil
}

// PrefixDir returns the Wine prefix directory for a bottle (the path
// callers set WINEPREFIX to when spawning wine). Validates the id.
func (s *Store) PrefixDir(id string) (string, error) {
	dir, err := s.BottleDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prefix"), nil
}

// LogsDir returns (and mkdir-ps) the per-bottle log directory. Spawn
// callers compose a filename of their choosing (typically <runID>.log)
// under this path. Validates the id.
func (s *Store) LogsDir(id string) (string, error) {
	dir, err := s.BottleDir(id)
	if err != nil {
		return "", err
	}
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return "", fmt.Errorf("create logs dir: %w", err)
	}
	return logs, nil
}

// SaveBottle atomically writes a bottle's metadata. Creates the
// bottle's directory if it doesn't exist. Validates b.ID before
// touching disk.
//
// Atomicity: writes to <dir>/.metadata.json.tmp then renames into
// place. Concurrent writers to the SAME bottle race on the rename
// (last-writer-wins); that's acceptable because bottles.Manager
// serializes its own mutations.
func (s *Store) SaveBottle(b *Bottle) error {
	dir, err := s.BottleDir(b.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bottle dir: %w", err)
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bottle: %w", err)
	}
	tmp := filepath.Join(dir, ".metadata.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp metadata: %w", err)
	}
	final := filepath.Join(dir, "metadata.json")
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename metadata into place: %w", err)
	}
	return nil
}

// LoadBottle returns the bottle for the given id. ErrBottleNotFound if
// the metadata file is missing; a wrapped IO/JSON error otherwise.
func (s *Store) LoadBottle(id string) (*Bottle, error) {
	dir, err := s.BottleDir(id)
	if err != nil {
		return nil, err
	}
	return readBottleFile(filepath.Join(dir, "metadata.json"))
}

// LoadBottles returns every bottle whose metadata.json exists under
// bottles/, sorted by CreatedAt ascending (oldest first). Directories
// without a valid metadata.json are skipped silently — they represent
// in-progress creates that crashed.
func (s *Store) LoadBottles() ([]*Bottle, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "bottles"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bottles dir: %w", err)
	}
	var out []*Bottle
	for _, e := range entries {
		if !e.IsDir() || !UUIDIsh(e.Name()) {
			continue
		}
		b, err := readBottleFile(filepath.Join(s.root, "bottles", e.Name(), "metadata.json"))
		if err != nil {
			// Silent skip on missing/invalid — a half-written bottle
			// shouldn't break list rendering for healthy ones.
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// DeleteBottle removes the bottle's directory tree. Idempotent —
// deleting an unknown id returns nil so callers can use it as a "make
// sure this is gone" primitive.
func (s *Store) DeleteBottle(id string) error {
	dir, err := s.BottleDir(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove bottle dir: %w", err)
	}
	return nil
}

// NameTaken reports whether any bottle currently has the given display
// name. Used by bottles.Manager.Create to enforce uniqueness before
// generating an ID — the comparison is case-insensitive ASCII because
// macOS filesystems default to case-insensitive and we don't want
// "Diablo II" and "diablo ii" to coexist confusingly.
func (s *Store) NameTaken(name string) (bool, error) {
	bs, err := s.LoadBottles()
	if err != nil {
		return false, err
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for _, b := range bs {
		if strings.ToLower(strings.TrimSpace(b.Name)) == target {
			return true, nil
		}
	}
	return false, nil
}

func readBottleFile(path string) (*Bottle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBottleNotFound
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var b Bottle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &b, nil
}

// uuidPattern matches RFC 4122 UUIDs in the canonical hyphenated form.
// We use it both to validate ids before they touch the filesystem
// (path-traversal guard) and to skip non-bottle entries inside the
// bottles/ directory during listing.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// UUIDIsh reports whether s looks like an RFC 4122 UUID. Exported so
// the bottles package can reject malformed input at its boundary too.
func UUIDIsh(s string) bool {
	return uuidPattern.MatchString(s)
}
