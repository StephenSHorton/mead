package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
	"github.com/StephenSHorton/mead/internal/winetricks"
)

// newTestStack builds a Mead stack pointed at a temp directory with a
// fake wine binary that logs its argv + env so tests can assert what
// was launched. Returns the manager, store, and bottleID of a fresh
// bottle created up front (the common test setup).
func newTestStack(t *testing.T) (*Manager, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()

	// fake-wine logs its invocation to argv-trace.log so we can verify
	// what apps.Install/Launch composed.
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "fake-wine")
	traceFile := filepath.Join(binDir, "argv-trace.log")
	script := `#!/bin/sh
echo "argv0=$0" >> ` + traceFile + `
i=1
for arg in "$@"; do
  echo "arg${i}=$arg" >> ` + traceFile + `
  i=$((i+1))
done
case "$1" in
  --version) echo "wine-test-1.0" ;;
  wineboot)  [ -n "$WINEPREFIX" ] && mkdir -p "$WINEPREFIX/drive_c" ;;
  *)         echo "ran $@"; echo "WINEPREFIX=$WINEPREFIX" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	t.Setenv("MEAD_WINE_PATH", bin)

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	w := wine.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	m := New(s, bm, w, nil, r)

	// Create a bottle for tests to operate on.
	b, err := bm.Create(context.Background(), "test-bottle")
	if err != nil {
		t.Fatalf("bottles.Create: %v", err)
	}
	return m, s, b.ID
}

func TestInstall_SpawnsWineWithInstaller(t *testing.T) {
	m, s, bottleID := newTestStack(t)

	// A fake installer — apps.Install doesn't actually run it
	// (fake-wine intercepts), but the path needs to exist for
	// absoluteizing.
	installer := filepath.Join(t.TempDir(), "Setup.exe")
	if err := os.WriteFile(installer, []byte{}, 0o644); err != nil {
		t.Fatalf("touch installer: %v", err)
	}

	proc, err := m.Install(context.Background(), bottleID, installer)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	<-proc.Done()

	if proc.ExitCode() != 0 {
		t.Errorf("ExitCode = %d; want 0", proc.ExitCode())
	}

	// Log file should be under <store>/bottles/<id>/logs/ alongside
	// its sidecar JSON.
	logsDir := filepath.Join(s.Root(), "bottles", bottleID, "logs")
	files, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("read logs dir: %v", err)
	}
	var logs, sidecars int
	for _, f := range files {
		switch {
		case strings.HasSuffix(f.Name(), ".log"):
			logs++
		case strings.HasSuffix(f.Name(), ".json"):
			sidecars++
		}
	}
	if logs != 1 || sidecars != 1 {
		t.Errorf("expected 1 .log + 1 .json under %s, got %d logs + %d sidecars", logsDir, logs, sidecars)
	}

	// Argv should have been wine <abs installer>, and WINEPREFIX
	// should point at the bottle's prefix.
	data, err := os.ReadFile(proc.LogPath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "ran "+installer) {
		t.Errorf("fake-wine didn't see the installer path: %q", out)
	}
	wantPrefix := filepath.Join(s.Root(), "bottles", bottleID, "prefix")
	if !strings.Contains(out, "WINEPREFIX="+wantPrefix) {
		t.Errorf("WINEPREFIX env not set correctly: %q", out)
	}
}

func TestInstall_EmptyPathRejected(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	if _, err := m.Install(context.Background(), bottleID, ""); !errors.Is(err, ErrInstallerPathRequired) {
		t.Errorf("expected ErrInstallerPathRequired, got %v", err)
	}
	if _, err := m.Install(context.Background(), bottleID, "   "); !errors.Is(err, ErrInstallerPathRequired) {
		t.Errorf("expected ErrInstallerPathRequired for whitespace, got %v", err)
	}
}

func TestInstall_UnknownBottle(t *testing.T) {
	m, _, _ := newTestStack(t)
	_, err := m.Install(context.Background(), "00000000-0000-0000-0000-000000000000", "/some/path.exe")
	if !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Errorf("expected ErrBottleNotFound, got %v", err)
	}
}

func TestLaunch_RelativePathResolvedToDriveC(t *testing.T) {
	m, s, bottleID := newTestStack(t)

	proc, err := m.Launch(context.Background(), bottleID, "windows/notepad.exe")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	<-proc.Done()

	wantPath := filepath.Join(s.Root(), "bottles", bottleID, "prefix", "drive_c", "windows", "notepad.exe")
	data, err := os.ReadFile(proc.LogPath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "ran "+wantPath) {
		t.Errorf("Launch didn't resolve relative path: %q (expected %q in argv)", data, wantPath)
	}
}

func TestLaunch_AbsolutePathPassedThrough(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	abs := "/Applications/Some.app/Contents/MacOS/Some"
	proc, err := m.Launch(context.Background(), bottleID, abs)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	<-proc.Done()
	data, _ := os.ReadFile(proc.LogPath())
	if !strings.Contains(string(data), "ran "+abs) {
		t.Errorf("Absolute path mangled: %q", data)
	}
}

func TestLaunch_EmptyPathRejected(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	if _, err := m.Launch(context.Background(), bottleID, ""); !errors.Is(err, ErrExePathRequired) {
		t.Errorf("expected ErrExePathRequired, got %v", err)
	}
}

func TestList_EmptyForNow(t *testing.T) {
	m, _, bottleID := newTestStack(t)
	apps, err := m.List(bottleID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("v0.2 List should return empty, got %d apps", len(apps))
	}
}

func TestList_UnknownBottle(t *testing.T) {
	m, _, _ := newTestStack(t)
	_, err := m.List("00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Errorf("expected ErrBottleNotFound, got %v", err)
	}
}

// TestRunWinetricks_SetsWineAndWineserverEnv guards the silent-fail bug
// where winetricks shelled `wineserver` via PATH and on a Mac with
// Homebrew wine installed picked a different wineserver than WINE.
// Both env vars must be set and point at binaries in the same dir.
func TestRunWinetricks_SetsWineAndWineserverEnv(t *testing.T) {
	m, _, bottleID := newTestStack(t)

	// Re-wire the manager with a winetricks locator pointing at a fake
	// script that echoes the env vars we care about into a known file.
	binDir := t.TempDir()
	wtBin := filepath.Join(binDir, "fake-winetricks")
	traceFile := filepath.Join(binDir, "winetricks-trace.log")
	script := `#!/bin/sh
echo "WINE=$WINE" >> ` + traceFile + `
echo "WINESERVER=$WINESERVER" >> ` + traceFile + `
echo "WINEPREFIX=$WINEPREFIX" >> ` + traceFile + `
`
	if err := os.WriteFile(wtBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-winetricks: %v", err)
	}
	t.Setenv("MEAD_WINETRICKS_PATH", wtBin)

	m.winetricks = winetricks.New()

	proc, err := m.RunWinetricks(context.Background(), bottleID, "d3dx9")
	if err != nil {
		t.Fatalf("RunWinetricks: %v", err)
	}
	<-proc.Done()

	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("read winetricks trace: %v", err)
	}
	out := string(data)

	wineEnv := os.Getenv("MEAD_WINE_PATH")
	if wineEnv == "" {
		t.Fatalf("MEAD_WINE_PATH not set by newTestStack")
	}
	wantWineLine := "WINE=" + wineEnv
	if !strings.Contains(out, wantWineLine) {
		t.Errorf("trace missing %q\ngot:\n%s", wantWineLine, out)
	}
	wantWineserverLine := "WINESERVER=" + filepath.Join(filepath.Dir(wineEnv), "wineserver")
	if !strings.Contains(out, wantWineserverLine) {
		t.Errorf("trace missing %q\ngot:\n%s", wantWineserverLine, out)
	}
}
