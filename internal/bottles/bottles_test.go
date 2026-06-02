package bottles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// fakeWineBin writes a shell script to a temp file that emulates a
// successful `wineboot --init`. It just creates the WINEPREFIX dir
// (so we can verify the runner composed the env correctly) and exits
// 0. Returns the absolute path, suitable for MEAD_WINE_PATH.
func fakeWineBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-wine")
	script := `#!/bin/sh
# Fake wine: mkdir -p $WINEPREFIX/drive_c to simulate wineboot --init
if [ -z "$WINEPREFIX" ]; then
  echo "WINEPREFIX not set" >&2
  exit 1
fi
mkdir -p "$WINEPREFIX/drive_c"
echo "fake-wine: created $WINEPREFIX/drive_c"
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	return bin
}

// fakeWineVersionBin produces a fake `wine --version`-able binary
// that prints a fixed version string but is also functional for the
// wineboot path. Lets one binary satisfy both Locator.Version() and
// the wineboot --init call.
func fakeWineVersionBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-wine")
	script := `#!/bin/sh
case "$1" in
  --version)
    echo "wine-fake-1.0"
    ;;
  wineboot)
    if [ -z "$WINEPREFIX" ]; then exit 1; fi
    mkdir -p "$WINEPREFIX/drive_c"
    ;;
  *)
    echo "fake-wine: unknown command $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	return bin
}

func newTestManager(t *testing.T, winePath string) (*Manager, *store.Store) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	// Force the wine locator to find our fake binary via the env
	// override. t.Setenv resets it at the end of the test.
	t.Setenv("MEAD_WINE_PATH", winePath)
	w := wine.New()
	r := runner.New()
	return New(s, w, r), s
}

func TestCreate_HappyPath(t *testing.T) {
	m, s := newTestManager(t, fakeWineVersionBin(t))

	b, err := m.Create(context.Background(), "Diablo II")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Name != "Diablo II" {
		t.Errorf("Name = %q", b.Name)
	}
	if !store.UUIDIsh(b.ID) {
		t.Errorf("ID = %q; expected UUID", b.ID)
	}
	if b.WineVersion != "wine-fake-1.0" {
		t.Errorf("WineVersion = %q; expected wine-fake-1.0", b.WineVersion)
	}

	// The fake wineboot creates drive_c/ — verify the prefix dir
	// exists with the expected child.
	prefix, err := s.PrefixDir(b.ID)
	if err != nil {
		t.Fatalf("PrefixDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prefix, "drive_c")); err != nil {
		t.Errorf("expected drive_c/ under prefix: %v", err)
	}

	// Persisted metadata should round-trip.
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != b.ID || got.Name != b.Name {
		t.Errorf("roundtrip mismatch")
	}
}

func TestCreate_EmptyNameRejected(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	for _, name := range []string{"", "   ", "\t\n"} {
		if _, err := m.Create(context.Background(), name); !errors.Is(err, ErrBottleNameRequired) {
			t.Errorf("Create(%q): expected ErrBottleNameRequired, got %v", name, err)
		}
	}
}

func TestCreate_NameConflict(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	if _, err := m.Create(context.Background(), "Foo"); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := m.Create(context.Background(), "foo"); !errors.Is(err, ErrBottleNameConflict) {
		t.Errorf("expected ErrBottleNameConflict for case-insensitive dupe, got %v", err)
	}
}

func TestCreate_RollsBackOnWinebootFailure(t *testing.T) {
	// A wine binary that exits non-zero on wineboot. Create should
	// remove the partial prefix so List() doesn't show a phantom.
	dir := t.TempDir()
	bin := filepath.Join(dir, "broken-wine")
	if err := os.WriteFile(bin, []byte(`#!/bin/sh
case "$1" in
  --version) echo "wine-broken" ;;
  wineboot)  echo "boot failed!" >&2; exit 42 ;;
esac
`), 0o755); err != nil {
		t.Fatalf("write broken-wine: %v", err)
	}
	m, s := newTestManager(t, bin)

	_, err := m.Create(context.Background(), "Cursed")
	if err == nil {
		t.Fatal("expected error from broken wineboot")
	}
	// Wineboot's stderr should be surfaced in the error.
	if !errStringContains(err, "boot failed") {
		t.Errorf("error didn't include wineboot output: %v", err)
	}
	// No bottles should be visible.
	bs, err := s.LoadBottles()
	if err != nil {
		t.Fatalf("LoadBottles: %v", err)
	}
	if len(bs) != 0 {
		t.Errorf("expected 0 bottles after rollback, got %d", len(bs))
	}
}

func TestCreate_FailsWhenWineNotFound(t *testing.T) {
	// Force locator into the no-wine state.
	t.Setenv("MEAD_WINE_PATH", "/definitely/does/not/exist")
	t.Setenv("PATH", "/tmp/empty-path-dir") // also strip PATH so wine64/wine misses
	// Point HOME at an empty temp dir so the home-relative candidates
	// resolve to nothing: the Mead-owned-Wine fallback (step 1b checks
	// ~/projects/mead/scripts/wine/bin/wine) and the ~/Applications cask
	// dir. Without this, a dev box that has built the Mead-owned Wine at
	// ~/projects/mead/scripts/wine defeats the neutralization and Create
	// unexpectedly succeeds.
	t.Setenv("HOME", t.TempDir())
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	// Neutralize every wine-resolution source so this stays deterministic
	// on a dev box that has GPTK (or the Mead-owned Wine) installed:
	// MEAD_WINE_PATH points at a missing path (step 1); HOME is an empty
	// temp dir so the Mead-owned-Wine + ~/Applications candidates miss
	// (step 1b); the running test binary isn't under a repo with
	// scripts/wine nor an .app bundle (steps 1b-walkup, 2); the empty PATH
	// means `brew` can't be found so the formula lookup is skipped (step 4)
	// and `wine64`/`wine` miss (step 5); and WithAppDirs() (step 3)
	// disables gcenx-cask detection in /Applications.
	m := New(s, wine.New(wine.WithAppDirs()), runner.New())

	_, err = m.Create(context.Background(), "Anything")
	if err == nil {
		t.Fatal("expected error when wine isn't located")
	}
	if !errors.Is(err, wine.ErrWineNotFound) {
		t.Errorf("expected wrapped ErrWineNotFound, got %v", err)
	}
}

func TestList_EmptyByDefault(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	bs, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(bs) != 0 {
		t.Errorf("expected empty list, got %d", len(bs))
	}
}

func TestDelete_Idempotent(t *testing.T) {
	m, _ := newTestManager(t, fakeWineVersionBin(t))
	b, err := m.Create(context.Background(), "to-delete")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Delete(b.ID); err != nil {
		t.Fatalf("Delete #1: %v", err)
	}
	if err := m.Delete(b.ID); err != nil {
		t.Errorf("Delete #2 should be idempotent, got %v", err)
	}
}

func errStringContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), substr)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
