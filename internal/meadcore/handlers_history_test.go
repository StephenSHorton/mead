package meadcore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/history"
	"github.com/StephenSHorton/mead/internal/registry"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// historyFakeWine stands in for the wine binary across the whole history
// flow: --version + wineboot (for bottles.create) and the reg query/add/
// delete surface (for registry.set + its undo). Argv is appended to
// $MEAD_ARGLOG. A key path containing "Missing" reports not-found from
// `reg query` so a test can drive the "value was absent → undo deletes"
// branch.
func historyFakeWine(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-wine")
	script := `#!/bin/sh
[ -n "$MEAD_ARGLOG" ] && printf '%s\n' "$*" >> "$MEAD_ARGLOG"
case "$1" in
  --version) echo "wine-fake-1.0"; exit 0 ;;
  wineboot) [ -z "$WINEPREFIX" ] && exit 1; mkdir -p "$WINEPREFIX/drive_c"; exit 0 ;;
  reg)
    case "$2" in
      query)
        case "$3" in
          *Missing*) printf 'reg: Unable to find the specified registry key\r\n'; exit 1 ;;
          *) printf '\r\n%s\r\n    Greeting    REG_SZ    hello\r\n\r\n' "$3"; exit 0 ;;
        esac ;;
      add) printf 'ok\r\n'; exit 0 ;;
      delete) printf 'ok\r\n'; exit 0 ;;
      *) exit 2 ;;
    esac ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	return bin
}

func newHistoryCore(t *testing.T) (*Core, string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	argLog := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("MEAD_ARGLOG", argLog)
	t.Setenv("MEAD_WINE_PATH", historyFakeWine(t))
	w := wine.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	hj, err := history.Open(s.Root())
	if err != nil {
		t.Fatalf("history.Open: %v", err)
	}
	c := &Core{
		Store:    s,
		Wine:     w,
		Runner:   r,
		Bottles:  bm,
		Registry: registry.New(s, bm, w, r),
		History:  hj,
	}
	return c, argLog
}

// makeBottle creates a bottle through the handler (so the create is
// recorded) and returns its id.
func makeBottle(t *testing.T, c *Core, name string) string {
	t.Helper()
	out := callJSON(t, c.handleBottlesCreate, bottlesCreateParams{Name: name})
	return out.(bottleSummary).ID
}

func env(t *testing.T, c *Core, id string) map[string]string {
	t.Helper()
	m, err := c.Bottles.EnvOverrides(id)
	if err != nil {
		t.Fatalf("EnvOverrides: %v", err)
	}
	if m == nil {
		return map[string]string{}
	}
	return m
}

func TestEnvSet_RecordUndoRedo(t *testing.T) {
	c, _ := newHistoryCore(t)
	id := makeBottle(t, c, "b")

	// Set a brand-new key.
	callJSON(t, c.handleEnvSet, envSetParams{BottleID: id, Key: "DXVK_HUD", Value: "fps"})
	if env(t, c, id)["DXVK_HUD"] != "fps" {
		t.Fatal("env not set")
	}

	// Undo → key should be gone (it was absent before).
	undone := callJSON(t, c.handleHistoryUndo, struct{}{}).(historyActionResult)
	if undone.Entry.Op != "env.set" {
		t.Errorf("undone op = %q", undone.Entry.Op)
	}
	if _, ok := env(t, c, id)["DXVK_HUD"]; ok {
		t.Error("undo should have removed the key that was previously absent")
	}

	// Redo → key restored.
	callJSON(t, c.handleHistoryRedo, struct{}{})
	if env(t, c, id)["DXVK_HUD"] != "fps" {
		t.Error("redo should have restored the key")
	}
}

func TestEnvSet_UndoRestoresPriorValue(t *testing.T) {
	c, _ := newHistoryCore(t)
	id := makeBottle(t, c, "b")
	callJSON(t, c.handleEnvSet, envSetParams{BottleID: id, Key: "WINEDEBUG", Value: "warn"})
	callJSON(t, c.handleEnvSet, envSetParams{BottleID: id, Key: "WINEDEBUG", Value: "fixme-all"})
	if env(t, c, id)["WINEDEBUG"] != "fixme-all" {
		t.Fatal("second set didn't take")
	}
	// Undo the second set → should restore "warn", not delete.
	callJSON(t, c.handleHistoryUndo, struct{}{})
	if got := env(t, c, id)["WINEDEBUG"]; got != "warn" {
		t.Errorf("undo restored %q, want warn", got)
	}
}

func TestDLLOverride_UndoRestoresWholeString(t *testing.T) {
	c, _ := newHistoryCore(t)
	id := makeBottle(t, c, "b")
	callJSON(t, c.handleDLLOverride, dllOverrideParams{BottleID: id, DLL: "d3d11", Mode: "native,builtin"})
	if !strings.Contains(env(t, c, id)["WINEDLLOVERRIDES"], "d3d11=native,builtin") {
		t.Fatal("dll override not applied")
	}
	callJSON(t, c.handleHistoryUndo, struct{}{})
	if _, ok := env(t, c, id)["WINEDLLOVERRIDES"]; ok {
		t.Error("undo should have removed WINEDLLOVERRIDES (was absent before)")
	}
}

func TestBottlesCreate_UndoDeletesBottle(t *testing.T) {
	c, _ := newHistoryCore(t)
	id := makeBottle(t, c, "doomed")
	if _, err := c.Bottles.Get(id); err != nil {
		t.Fatalf("bottle should exist post-create: %v", err)
	}
	// Undo the create → bottle gone, redo stack cleared (terminal).
	undone := callJSON(t, c.handleHistoryUndo, struct{}{}).(historyActionResult)
	if undone.Entry.Op != "bottles.create" {
		t.Errorf("undone op = %q", undone.Entry.Op)
	}
	if _, err := c.Bottles.Get(id); !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Errorf("bottle should be gone after undo, got %v", err)
	}
	undo, redo := c.History.List()
	if len(undo) != 0 || len(redo) != 0 {
		t.Errorf("terminal undo should leave both stacks empty: undo=%d redo=%d", len(undo), len(redo))
	}
}

func TestRegistrySet_UndoDeletesNewValue(t *testing.T) {
	c, argLog := newHistoryCore(t)
	id := makeBottle(t, c, "b")
	_ = os.WriteFile(argLog, nil, 0o644) // drop create-time argv

	// Set a value under a key the fake reports as Missing → before absent.
	callJSON(t, c.handleRegistrySet, registrySetParams{
		BottleID: id, Key: `HKCU\Software\Missing`, Value: "Foo", Type: "REG_SZ", Data: "bar",
	})
	// Undo → since the value was absent before, the inverse is reg delete.
	callJSON(t, c.handleHistoryUndo, struct{}{})
	got, _ := os.ReadFile(argLog)
	if !strings.Contains(string(got), "reg delete") {
		t.Errorf("undo of a value-create should issue reg delete; argv:\n%s", got)
	}
}

func TestHistoryListParams_Decode(t *testing.T) {
	var p historyListParams
	if err := json.Unmarshal([]byte(`{"bottle_id":"b1"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestRegistrySet_NonUndoableDoesNotWedgeUndo(t *testing.T) {
	// A registry.set that can't be cleanly captured (here: a key-level
	// create over a key the fake reports as already-existing) must NOT be
	// recorded as a barrier — undo of the earlier bottles.create must still
	// work. (Regression guard: a non-undoable entry on top used to wedge
	// the whole undo stack.)
	c, _ := newHistoryCore(t)
	id := makeBottle(t, c, "b") // recorded, undoable

	// Key-level set (empty Value) over an existing key → registrySetBefore
	// returns recordable=false → not recorded.
	callJSON(t, c.handleRegistrySet, registrySetParams{
		BottleID: id, Key: `HKCU\Software\Existing`, Value: "",
	})
	undo, _ := c.History.List()
	if len(undo) != 1 || undo[0].Op != "bottles.create" {
		t.Fatalf("non-undoable set should not be recorded; undo stack = %+v", undo)
	}
	// Undo must reach the create, not error on a barrier.
	if _, err := c.handleHistoryUndo(nil); err != nil {
		t.Fatalf("undo should reach the create, got %v", err)
	}
	if _, err := c.Bottles.Get(id); !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Errorf("create should have been undone, got %v", err)
	}
}

func TestHistory_EmptyStackErrors(t *testing.T) {
	c, _ := newHistoryCore(t)
	if _, err := c.handleHistoryUndo(nil); !errors.Is(err, history.ErrNothingToUndo) {
		t.Errorf("undo on empty = %v, want ErrNothingToUndo", err)
	}
	if _, err := c.handleHistoryRedo(nil); !errors.Is(err, history.ErrNothingToRedo) {
		t.Errorf("redo on empty = %v, want ErrNothingToRedo", err)
	}
}

func TestHistoryList_FiltersByBottle(t *testing.T) {
	c, _ := newHistoryCore(t)
	a := makeBottle(t, c, "A")
	b := makeBottle(t, c, "B")
	callJSON(t, c.handleEnvSet, envSetParams{BottleID: a, Key: "X", Value: "1"})

	out := callJSON(t, c.handleHistoryList, historyListParams{BottleID: a}).(historyListResult)
	// Bottle A: its create + the env.set = 2 undo entries.
	if len(out.Undo) != 2 {
		t.Errorf("bottle A undo entries = %d, want 2", len(out.Undo))
	}
	for _, e := range out.Undo {
		if e.BottleID != a {
			t.Errorf("filter leaked an entry for %s", e.BottleID)
		}
	}
	// Unfiltered: A.create + B.create + env.set = 3.
	all := callJSON(t, c.handleHistoryList, historyListParams{}).(historyListResult)
	if len(all.Undo) != 3 {
		t.Errorf("unfiltered undo entries = %d, want 3", len(all.Undo))
	}
	_ = b
}

func TestHistoryList_NeverNull(t *testing.T) {
	c, _ := newHistoryCore(t)
	out := callJSON(t, c.handleHistoryList, historyListParams{}).(historyListResult)
	if out.Undo == nil || out.Redo == nil {
		t.Error("history.list stacks must never be null")
	}
}
