// Package history is Mead's undo/redo journal: a global, on-disk record
// of the reversible prefix mutations Mead has performed, so an agent (or
// the user) can walk a bad change back out.
//
// The journal is a classic two-stack model — an undo stack and a redo
// stack — persisted to a single history.json under the store root. It is
// GLOBAL (not per-bottle) on purpose: undoing a bottles.create deletes
// the bottle, and a per-bottle journal would delete its own file mid-undo.
//
// This package is deliberately "dumb": it stores Entry records and manages
// the two stacks + atomic persistence, but it does NOT know how to apply
// an inverse. The meadcore layer owns the apply logic (it has the bottle /
// registry managers) and calls the manager PRIMITIVES directly during
// undo/redo — which is also why no replay-suppression flag is needed: the
// recording lives in the MCP handlers, not the primitives, so replaying an
// inverse through a primitive never re-records.
//
// What's recorded (all Undoable): env.set, dll.override, registry.set,
// bottles.create, bottles.clone. The unbounded/destructive mutations
// (winetricks.run, apps.install/uninstall, bottles.delete) are NOT tracked
// — their only honest inverse is a full prefix snapshot, which is a
// separate feature. So "undo" walks the tracked operations in LIFO order
// and is silent about untracked side effects.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// schemaVersion tags the on-disk doc. Extend the shape at the end and
// ignore unknown fields so older Mead parses newer journals.
const schemaVersion = 1

// maxEntries caps undo+redo so history.json stays small enough to rewrite
// whole on every mutation. Oldest undo entries are dropped past the cap.
const maxEntries = 200

var (
	// ErrNothingToUndo / ErrNothingToRedo: the corresponding stack is empty.
	ErrNothingToUndo = errors.New("nothing to undo")
	ErrNothingToRedo = errors.New("nothing to redo")
	// ErrEntryNotUndoable: the top undo entry is flagged not-undoable.
	// Today nothing records such an entry — a mutation that can't be
	// cleanly reverted is simply NOT recorded (stays invisible to undo),
	// rather than recorded as a barrier. The flag + this guard are
	// defensive: if barrier entries are ever introduced, handleHistoryUndo
	// must SKIP past them (pop without applying), not error here — an
	// erroring barrier would wedge all older undo history beneath it.
	ErrEntryNotUndoable = errors.New("the most recent recorded operation is not undoable")
	// ErrStale: the stack changed between Peek and Commit (a concurrent
	// mutation). The caller should re-peek and retry.
	ErrStale = errors.New("history changed concurrently; retry")
)

// State is an op-specific before/after snapshot. Which fields are
// meaningful depends on the Entry.Op:
//   - env.set / dll.override: Value carries the whole env-var value (for
//     dll.override, the entire WINEDLLOVERRIDES string). The inverse is
//     purely Value-driven — value=="" means the key is deleted (absent) —
//     so Present is INFORMATIONAL only here, not load-bearing.
//   - registry.set: Present is LOAD-BEARING — it selects the inverse:
//     Present=false (value/key didn't exist) → inverse is Delete;
//     Present=true → inverse is Set(prior Type+Data).
type State struct {
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
	Type    string `json:"type,omitempty"`
	Data    string `json:"data,omitempty"`
}

// Entry is one reversible operation. Before/After carry the state needed
// to compute the inverse (undo, from Before) and re-apply (redo, from
// After). Key/Name identify the target (env var name / registry key path
// + value name); they're empty for bottle-lifecycle ops.
type Entry struct {
	ID        string `json:"id"`
	Op        string `json:"op"`
	BottleID  string `json:"bottle_id"`
	Timestamp string `json:"timestamp"`
	Undoable  bool   `json:"undoable"`
	Key       string `json:"key,omitempty"`
	Name      string `json:"name,omitempty"`
	Before    State  `json:"before"`
	After     State  `json:"after"`
	Summary   string `json:"summary,omitempty"`
}

type doc struct {
	SchemaVersion int     `json:"schema_version"`
	Undo          []Entry `json:"undo"`
	Redo          []Entry `json:"redo"`
}

// Journal is the persistent two-stack undo/redo log. Safe for concurrent
// use; one per process (the meadcore.Core singleton owns it). It keeps the
// doc in memory and rewrites the whole file atomically on every mutation —
// fine because the journal is bounded by maxEntries and a single Mead
// instance is enforced by the bridge lockfile.
type Journal struct {
	path string
	mu   sync.Mutex
	d    doc
}

// Open loads (or initializes) the journal at <root>/history.json. A
// missing file starts an empty journal. A corrupt/unparseable file is an
// error — the caller (meadcore) logs it and degrades to no-history rather
// than discarding the user's undo record silently or crashing.
func Open(root string) (*Journal, error) {
	j := &Journal{path: filepath.Join(root, "history.json"), d: doc{SchemaVersion: schemaVersion}}
	data, err := os.ReadFile(j.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return j, nil
		}
		return nil, fmt.Errorf("read history: %w", err)
	}
	if err := json.Unmarshal(data, &j.d); err != nil {
		return nil, fmt.Errorf("decode history %s: %w", j.path, err)
	}
	if j.d.SchemaVersion == 0 {
		j.d.SchemaVersion = schemaVersion
	}
	return j, nil
}

// Record pushes a completed mutation onto the undo stack and clears the
// redo stack (a new action invalidates the redo future). Best-effort from
// the caller's view: a returned error means the journal write failed, but
// the underlying mutation already succeeded — callers log and move on
// rather than rolling back a real change.
func (j *Journal) Record(e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.d.Undo = append(j.d.Undo, e)
	j.d.Redo = nil
	if len(j.d.Undo) > maxEntries {
		j.d.Undo = append([]Entry(nil), j.d.Undo[len(j.d.Undo)-maxEntries:]...)
	}
	return j.saveLocked()
}

// PeekUndo returns the top of the undo stack without mutating it.
func (j *Journal) PeekUndo() (Entry, bool) { return j.peek(false) }

// PeekRedo returns the top of the redo stack without mutating it.
func (j *Journal) PeekRedo() (Entry, bool) { return j.peek(true) }

func (j *Journal) peek(redo bool) (Entry, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.d.Undo
	if redo {
		s = j.d.Redo
	}
	if len(s) == 0 {
		return Entry{}, false
	}
	return s[len(s)-1], true
}

// CommitUndo finalizes an undo whose inverse the caller already applied
// successfully. It verifies the top of the undo stack is still the entry
// the caller acted on (by id) — guarding against a concurrent Record
// between Peek and Commit — then pops it. terminal=true (a structural
// undo that deleted a bottle) clears the redo stack and does NOT push the
// entry, since a deleted bottle can't be redone onto the same id; the
// usual case pushes the entry onto redo.
//
// Persistence is best-effort, like Record: the caller applies the real
// inverse and THEN calls CommitUndo, so a returned save error means the
// pop already happened in memory and on disk-failure the entry may reload
// as still-undoable on a restart (a second undo would re-apply the inverse,
// which the env/registry/delete inverses tolerate idempotently). Treat a
// Commit error as "applied; not durably recorded," not "undo failed."
func (j *Journal) CommitUndo(id string, terminal bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := len(j.d.Undo)
	if n == 0 || j.d.Undo[n-1].ID != id {
		return ErrStale
	}
	e := j.d.Undo[n-1]
	j.d.Undo = j.d.Undo[:n-1]
	if terminal {
		j.d.Redo = nil
	} else {
		j.d.Redo = append(j.d.Redo, e)
	}
	return j.saveLocked()
}

// CommitRedo finalizes a redo whose forward op the caller already
// re-applied. Verifies the redo top by id, pops it, pushes onto undo.
func (j *Journal) CommitRedo(id string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := len(j.d.Redo)
	if n == 0 || j.d.Redo[n-1].ID != id {
		return ErrStale
	}
	e := j.d.Redo[n-1]
	j.d.Redo = j.d.Redo[:n-1]
	j.d.Undo = append(j.d.Undo, e)
	return j.saveLocked()
}

// List returns copies of the undo and redo stacks (oldest-first). The top
// of each — the next to be undone / redone — is the LAST element.
func (j *Journal) List() (undo, redo []Entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]Entry(nil), j.d.Undo...), append([]Entry(nil), j.d.Redo...)
}

// BottleSummary returns the undo/redo depth attributable to one bottle and
// the most recent recorded op for it (newest undo entry). Used by
// bottles.inspect's history block.
func (j *Journal) BottleSummary(bottleID string) (undoDepth, redoDepth int, lastOp string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, e := range j.d.Undo {
		if e.BottleID == bottleID {
			undoDepth++
			lastOp = e.Op // undo is append-order, so the last match is newest
		}
	}
	for _, e := range j.d.Redo {
		if e.BottleID == bottleID {
			redoDepth++
		}
	}
	return undoDepth, redoDepth, lastOp
}

// saveLocked rewrites the whole journal atomically (temp + rename), the
// same durability pattern store.SaveBottle / runner.writeSidecar use. mu
// must be held.
func (j *Journal) saveLocked() error {
	data, err := json.MarshalIndent(j.d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp history: %w", err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("rename history into place: %w", err)
	}
	return nil
}
