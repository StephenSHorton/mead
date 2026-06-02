package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Journal {
	t.Helper()
	j, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return j
}

func entry(id, op, bottle string) Entry {
	return Entry{ID: id, Op: op, BottleID: bottle, Undoable: true}
}

func TestOpen_MissingFileIsEmpty(t *testing.T) {
	j := openTemp(t)
	if _, ok := j.PeekUndo(); ok {
		t.Error("fresh journal should have no undo entries")
	}
	undo, redo := j.List()
	if len(undo) != 0 || len(redo) != 0 {
		t.Errorf("fresh journal not empty: undo=%d redo=%d", len(undo), len(redo))
	}
}

func TestRecord_PushesAndClearsRedo(t *testing.T) {
	j := openTemp(t)
	if err := j.Record(entry("1", "env.set", "b")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// Undo it so there's something on the redo stack.
	top, _ := j.PeekUndo()
	if err := j.CommitUndo(top.ID, false); err != nil {
		t.Fatalf("CommitUndo: %v", err)
	}
	if _, ok := j.PeekRedo(); !ok {
		t.Fatal("expected a redo entry after undo")
	}
	// A new Record must clear the redo stack.
	if err := j.Record(entry("2", "env.set", "b")); err != nil {
		t.Fatalf("Record 2: %v", err)
	}
	if _, ok := j.PeekRedo(); ok {
		t.Error("Record should have cleared the redo stack")
	}
}

func TestUndoRedo_RoundTrip(t *testing.T) {
	j := openTemp(t)
	for _, id := range []string{"1", "2", "3"} {
		if err := j.Record(entry(id, "env.set", "b")); err != nil {
			t.Fatalf("Record %s: %v", id, err)
		}
	}
	// Undo top (id 3) → redo.
	top, _ := j.PeekUndo()
	if top.ID != "3" {
		t.Fatalf("undo top = %s, want 3", top.ID)
	}
	if err := j.CommitUndo("3", false); err != nil {
		t.Fatalf("CommitUndo: %v", err)
	}
	// Redo top should be 3 now.
	rtop, ok := j.PeekRedo()
	if !ok || rtop.ID != "3" {
		t.Fatalf("redo top = %v/%s, want 3", ok, rtop.ID)
	}
	if err := j.CommitRedo("3"); err != nil {
		t.Fatalf("CommitRedo: %v", err)
	}
	// Back to 3 entries on undo, none on redo.
	undo, redo := j.List()
	if len(undo) != 3 || len(redo) != 0 {
		t.Errorf("after round-trip: undo=%d redo=%d, want 3/0", len(undo), len(redo))
	}
}

func TestCommitUndo_Terminal_ClearsRedo(t *testing.T) {
	j := openTemp(t)
	_ = j.Record(entry("create", "bottles.create", "b"))
	_ = j.Record(entry("env", "env.set", "b"))
	// Undo env (→redo), then undo create as terminal.
	_ = j.CommitUndo("env", false)
	if _, ok := j.PeekRedo(); !ok {
		t.Fatal("env should be on redo")
	}
	if err := j.CommitUndo("create", true); err != nil {
		t.Fatalf("terminal CommitUndo: %v", err)
	}
	undo, redo := j.List()
	if len(undo) != 0 {
		t.Errorf("undo should be empty, got %d", len(undo))
	}
	if len(redo) != 0 {
		t.Errorf("terminal undo must clear redo, got %d entries", len(redo))
	}
}

func TestCommit_StaleIDRejected(t *testing.T) {
	j := openTemp(t)
	_ = j.Record(entry("1", "env.set", "b"))
	if err := j.CommitUndo("not-the-top", false); !errors.Is(err, ErrStale) {
		t.Errorf("CommitUndo with wrong id = %v, want ErrStale", err)
	}
}

func TestPeek_EmptyStacks(t *testing.T) {
	j := openTemp(t)
	if _, ok := j.PeekUndo(); ok {
		t.Error("PeekUndo on empty should be false")
	}
	if _, ok := j.PeekRedo(); ok {
		t.Error("PeekRedo on empty should be false")
	}
}

func TestCap_DropsOldest(t *testing.T) {
	j := openTemp(t)
	total := maxEntries + 50
	for i := 0; i < total; i++ {
		if err := j.Record(Entry{ID: string(rune('a')) + itoa(i), Op: "env.set", BottleID: "b", Undoable: true}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	undo, _ := j.List()
	if len(undo) != maxEntries {
		t.Fatalf("undo len = %d, want capped at %d", len(undo), maxEntries)
	}
	// The newest entry must survive; the oldest must have been dropped.
	if undo[len(undo)-1].ID != "a"+itoa(total-1) {
		t.Errorf("newest entry missing: top = %s", undo[len(undo)-1].ID)
	}
	if undo[0].ID == "a0" {
		t.Errorf("oldest entry should have been dropped, but undo[0] = %s", undo[0].ID)
	}
}

func TestBottleSummary(t *testing.T) {
	j := openTemp(t)
	_ = j.Record(entry("1", "env.set", "A"))
	_ = j.Record(entry("2", "dll.override", "A"))
	_ = j.Record(entry("3", "env.set", "B"))
	// Undo the B entry so it lands on redo.
	_ = j.CommitUndo("3", false)

	ud, rd, last := j.BottleSummary("A")
	if ud != 2 || rd != 0 {
		t.Errorf("bottle A: undo=%d redo=%d, want 2/0", ud, rd)
	}
	if last != "dll.override" {
		t.Errorf("bottle A last op = %q, want dll.override (newest)", last)
	}
	ud, rd, _ = j.BottleSummary("B")
	if ud != 0 || rd != 1 {
		t.Errorf("bottle B: undo=%d redo=%d, want 0/1", ud, rd)
	}
}

func TestPersistence_ReloadsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	j, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = j.Record(entry("1", "env.set", "b"))
	_ = j.Record(entry("2", "registry.set", "b"))
	_ = j.CommitUndo("2", false) // 1 on undo, 2 on redo

	// Reopen from disk — stacks must survive.
	j2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	undo, redo := j2.List()
	if len(undo) != 1 || undo[0].ID != "1" {
		t.Errorf("reloaded undo = %+v, want [1]", undo)
	}
	if len(redo) != 1 || redo[0].ID != "2" {
		t.Errorf("reloaded redo = %+v, want [2]", redo)
	}
}

func TestOpen_CorruptIsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "history.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if _, err := Open(dir); err == nil {
		t.Error("Open should error on a corrupt journal so the caller can degrade")
	}
}

// itoa avoids importing strconv just for the cap test's id suffixes.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
