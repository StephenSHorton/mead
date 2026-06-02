package meadcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/StephenSHorton/mead/internal/history"
	"github.com/StephenSHorton/mead/internal/registry"

	"github.com/google/uuid"
)

// The History surface lets the agent walk back a reversible prefix change.
// Recording happens inside the MUTATION handlers (handleEnvSet,
// handleDLLOverride, handleRegistrySet, handleBottlesCreate/Clone) AFTER
// the change succeeds; undo/redo here REPLAY by calling the manager
// PRIMITIVES directly (Bottles.SetEnv, Registry.Set/Delete, Bottles.Delete)
// — never the handlers — so a replayed inverse never re-records.
//
// Tracked + undoable: env.set, dll.override, registry.set, bottles.create,
// bottles.clone. Undoing a create/clone deletes the bottle (terminal: it
// can't be redone onto the same id, so it clears the redo stack). Other
// prefix mutations (winetricks.run, apps.install/uninstall, bottles.delete)
// are NOT tracked.

// nowStamp formats a timestamp the way the sidecar/diagnostics layer does.
func nowStamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// --- recording (called by the mutation handlers) ----------------------

// record pushes an entry, best-effort: a journal-write failure is logged
// by Record's caller chain but never fails the (already-applied) mutation.
func (c *Core) record(e history.Entry) {
	if c.History == nil {
		return
	}
	e.ID = uuid.NewString()
	e.Timestamp = nowStamp()
	_ = c.History.Record(e)
}

func (c *Core) recordEnvSet(bottleID, key string, before, after history.State) {
	c.record(history.Entry{
		Op: "env.set", BottleID: bottleID, Undoable: true,
		Key: key, Before: before, After: after,
		Summary: "env.set " + key,
	})
}

func (c *Core) recordDLLOverride(bottleID, dll string, before, after history.State) {
	c.record(history.Entry{
		Op: "dll.override", BottleID: bottleID, Undoable: true,
		Key: "WINEDLLOVERRIDES", Before: before, After: after,
		Summary: "dll.override " + dll,
	})
}

// recordRegistrySet records a registry.set whose prior state was cleanly
// captured (so it can be reverted). Sets that can't be cleanly reverted
// are NOT recorded at all (see handleRegistrySet) — never recorded as a
// non-undoable entry, which would wedge the undo stack.
func (c *Core) recordRegistrySet(bottleID, key, valueName string, before, after history.State) {
	name := valueName
	if name == "" {
		name = "(default key)"
	}
	c.record(history.Entry{
		Op: "registry.set", BottleID: bottleID, Undoable: true,
		Key: key, Name: valueName, Before: before, After: after,
		Summary: "registry.set " + key + " " + name,
	})
}

func (c *Core) recordBottleLifecycle(op, bottleID, name string) {
	c.record(history.Entry{
		Op: op, BottleID: bottleID, Undoable: true,
		Summary: strings.TrimPrefix(op, "bottles.") + " bottle " + name,
	})
}

// --- apply (undo inverse / redo forward) -------------------------------

// applyInverse reverts a recorded op using its Before state, by calling
// the manager primitive directly. terminal=true means the inverse deleted
// a bottle (create/clone undo) and so must clear the redo stack. A missing
// bottle surfaces as an error from the primitive → the caller does not
// commit, leaving the stack intact.
func (c *Core) applyInverse(e history.Entry) (terminal bool, err error) {
	switch e.Op {
	case "env.set":
		return false, c.Bottles.SetEnv(e.BottleID, e.Key, e.Before.Value)
	case "dll.override":
		return false, c.Bottles.SetEnv(e.BottleID, "WINEDLLOVERRIDES", e.Before.Value)
	case "registry.set":
		if e.Before.Present {
			return false, c.Registry.Set(context.Background(), e.BottleID, e.Key, e.Name, e.Before.Type, e.Before.Data)
		}
		// Value (or key) didn't exist before → delete it. Note: if the
		// original set also auto-created the parent key, this removes only
		// the value and leaves an empty key behind — an inert cosmetic
		// orphan, not data loss.
		return false, c.Registry.Delete(context.Background(), e.BottleID, e.Key, e.Name)
	case "bottles.create", "bottles.clone":
		return true, c.Bottles.Delete(e.BottleID)
	default:
		return false, fmt.Errorf("cannot undo unrecognized op %q", e.Op)
	}
}

// applyForward re-applies a recorded op using its After state. Only the
// env/dll/registry ops ever reach the redo stack (create/clone undo is
// terminal and never pushed), so those are the only cases handled.
func (c *Core) applyForward(e history.Entry) error {
	switch e.Op {
	case "env.set":
		return c.Bottles.SetEnv(e.BottleID, e.Key, e.After.Value)
	case "dll.override":
		return c.Bottles.SetEnv(e.BottleID, "WINEDLLOVERRIDES", e.After.Value)
	case "registry.set":
		return c.Registry.Set(context.Background(), e.BottleID, e.Key, e.Name, e.After.Type, e.After.Data)
	default:
		return fmt.Errorf("cannot redo op %q", e.Op)
	}
}

// --- MCP handlers ------------------------------------------------------

// entryView is the compact, agent-facing projection of a journal entry
// (no before/after blobs). Used by history.list and the undo/redo results.
type entryView struct {
	ID        string `json:"id"`
	Op        string `json:"op"`
	BottleID  string `json:"bottle_id"`
	Timestamp string `json:"timestamp"`
	Undoable  bool   `json:"undoable"`
	Summary   string `json:"summary,omitempty"`
}

func toEntryView(e history.Entry) entryView {
	return entryView{
		ID: e.ID, Op: e.Op, BottleID: e.BottleID,
		Timestamp: e.Timestamp, Undoable: e.Undoable, Summary: e.Summary,
	}
}

type historyActionResult struct {
	// Entry is the operation that was undone/redone.
	Entry entryView `json:"entry"`
}

func (c *Core) handleHistoryUndo(_ json.RawMessage) (any, error) {
	if err := c.requireHistory(); err != nil {
		return nil, err
	}
	e, ok := c.History.PeekUndo()
	if !ok {
		return nil, history.ErrNothingToUndo
	}
	if !e.Undoable {
		return nil, fmt.Errorf("%w (op: %s)", history.ErrEntryNotUndoable, e.Op)
	}
	terminal, err := c.applyInverse(e)
	if err != nil {
		return nil, fmt.Errorf("undo %s: %w", e.Op, err)
	}
	if err := c.History.CommitUndo(e.ID, terminal); err != nil {
		return nil, err
	}
	return historyActionResult{Entry: toEntryView(e)}, nil
}

func (c *Core) handleHistoryRedo(_ json.RawMessage) (any, error) {
	if err := c.requireHistory(); err != nil {
		return nil, err
	}
	e, ok := c.History.PeekRedo()
	if !ok {
		return nil, history.ErrNothingToRedo
	}
	if err := c.applyForward(e); err != nil {
		return nil, fmt.Errorf("redo %s: %w", e.Op, err)
	}
	if err := c.History.CommitRedo(e.ID); err != nil {
		return nil, err
	}
	return historyActionResult{Entry: toEntryView(e)}, nil
}

type historyListParams struct {
	// BottleID optionally filters the listing to one bottle (display only;
	// undo/redo always operate on the global stack).
	BottleID string `json:"bottle_id"`
}

type historyListResult struct {
	// Undo/Redo are oldest-first; the NEXT to be undone/redone is the LAST
	// element of Undo / Redo respectively. Never null.
	Undo []entryView `json:"undo"`
	Redo []entryView `json:"redo"`
}

func (c *Core) handleHistoryList(raw json.RawMessage) (any, error) {
	if err := c.requireHistory(); err != nil {
		return nil, err
	}
	var p historyListParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
	}
	undo, redo := c.History.List()
	out := historyListResult{Undo: []entryView{}, Redo: []entryView{}}
	for _, e := range undo {
		if p.BottleID != "" && e.BottleID != p.BottleID {
			continue
		}
		out.Undo = append(out.Undo, toEntryView(e))
	}
	for _, e := range redo {
		if p.BottleID != "" && e.BottleID != p.BottleID {
			continue
		}
		out.Redo = append(out.Redo, toEntryView(e))
	}
	return out, nil
}

// registrySetBefore captures the prior state of a registry target so the
// inverse can restore (or delete) it. Returns undoable=false when the set
// is a no-op key-create over an existing key, or when the prior state
// couldn't be read (so we won't claim a wrong inverse).
func (c *Core) registrySetBefore(bottleID, key, valueName string) (before history.State, undoable bool) {
	ctx := context.Background()
	if valueName != "" {
		qr, err := c.Registry.Query(ctx, bottleID, key, valueName)
		if err == nil {
			if len(qr.Values) > 0 {
				return history.State{Present: true, Type: qr.Values[0].Type, Data: qr.Values[0].Data}, true
			}
			return history.State{}, true // key exists, value absent → inverse deletes the value
		}
		if errors.Is(err, registry.ErrRegistryKeyNotFound) {
			return history.State{}, true // value absent → inverse deletes it
		}
		return history.State{}, false // couldn't read prior state
	}
	// Key-level create: only cleanly undoable if the key was absent (then
	// inverse = delete the key). If it already exists, the create is a
	// no-op with no safe inverse (deleting would clobber existing subkeys).
	if _, err := c.Registry.Query(ctx, bottleID, key, ""); err != nil {
		if errors.Is(err, registry.ErrRegistryKeyNotFound) {
			return history.State{}, true
		}
	}
	return history.State{}, false
}
