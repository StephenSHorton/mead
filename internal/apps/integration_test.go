package apps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// TestIntegration_WC3PrefixPath_MatchesWC3ForgeExpectation is the
// load-bearing dogfood test for the Mead↔wc3-forge integration.
//
// wc3-forge resolves its WC3 install via the WC3FORGE_WC3_PATH env
// var (see asset_handler.go::wc3InstallPath in the wc3-forge repo).
// When a user installs Warcraft III into a Mead bottle via Mead's
// apps.install, the install lands somewhere under the bottle's Wine
// prefix. This test asserts that the canonical install path Mead
// produces is exactly the directory shape wc3-forge expects to
// point WC3FORGE_WC3_PATH at — guaranteeing the integration works
// without any cross-repo code.
//
// Scenario the test simulates:
//
//  1. Mead creates a bottle.
//  2. Apps.Install runs an installer in that bottle. (We use a
//     fake "Battle.net installer" shell script that materializes
//     the canonical WC3 install structure under
//     drive_c/Program Files (x86)/Warcraft III/.)
//  3. After install, the path
//     <prefix>/drive_c/Program Files (x86)/Warcraft III/
//     exists and contains a .build.info file (the marker CascLib
//     uses to identify a CASC root).
//  4. That path is what the user would set WC3FORGE_WC3_PATH to.
func TestIntegration_WC3PrefixPath_MatchesWC3ForgeExpectation(t *testing.T) {
	// fake-wine that mimics what Battle.net would do for a WC3 install:
	// mkdir the canonical path and drop a fake .build.info marker so
	// CascLib (and our path-existence check) is satisfied.
	wineBin := filepath.Join(t.TempDir(), "fake-wine")
	script := `#!/bin/sh
case "$1" in
  --version) echo "wine-mead-integration-1.0" ;;
  wineboot)  [ -n "$WINEPREFIX" ] && mkdir -p "$WINEPREFIX/drive_c" ;;
  *)
    # "$1" is the installer path. We don't actually run it — we just
    # simulate what a successful WC3 installer would produce inside
    # the Wine prefix.
    INSTALL_DIR="$WINEPREFIX/drive_c/Program Files (x86)/Warcraft III"
    mkdir -p "$INSTALL_DIR/_retail_/x86_64"
    # CascLib looks for .build.info at the install root to identify a
    # CASC storage. Drop a minimal one so future code that stats or
    # partially parses it doesn't immediately error.
    cat > "$INSTALL_DIR/.build.info" <<EOF
Branch!STRING:0|Active!DEC:1|Build Key!HEX:16|Tags!STRING:0
us|1|deadbeefcafef00d|Mead-integration-test
EOF
    echo "installed WC3 to $INSTALL_DIR"
    ;;
esac
`
	if err := os.WriteFile(wineBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	t.Setenv("MEAD_WINE_PATH", wineBin)

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	w := wine.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	m := New(s, bm, w, r)
	b, err := bm.Create(context.Background(), "wc3-bottle")
	if err != nil {
		t.Fatalf("bottles.Create: %v", err)
	}

	// Stand-in for a downloaded "Warcraft III installer.exe" — fake-wine
	// ignores the contents.
	installer := filepath.Join(t.TempDir(), "Warcraft-III-Setup.exe")
	if err := os.WriteFile(installer, []byte{}, 0o644); err != nil {
		t.Fatalf("touch installer: %v", err)
	}

	proc, err := m.Install(context.Background(), b.ID, installer)
	if err != nil {
		t.Fatalf("apps.Install: %v", err)
	}
	<-proc.Done()
	if proc.ExitCode() != 0 {
		logs, _, _, _ := proc.Logs(0, 0)
		t.Fatalf("install exited %d. logs:\n%s", proc.ExitCode(), logs)
	}

	// The exact path wc3-forge's WC3FORGE_WC3_PATH should be set to.
	// This is the integration contract: Mead places WC3 here, wc3-forge
	// looks for it here. If either side moves this path the test fails
	// and someone has to think about how to reconcile.
	wc3ForgeExpectedPath := filepath.Join(
		s.Root(), "bottles", b.ID,
		"prefix", "drive_c", "Program Files (x86)", "Warcraft III",
	)

	info, err := os.Stat(wc3ForgeExpectedPath)
	if err != nil {
		t.Fatalf("expected install path missing: %v\npath: %s", err, wc3ForgeExpectedPath)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s, got file", wc3ForgeExpectedPath)
	}

	// .build.info — CascLib's CASC-root marker. wc3-forge's
	// asset_handler.go opens CASC against this directory; the marker
	// being absent is the difference between "valid install" and
	// "wc3-forge thinks the path is broken."
	buildInfo, err := os.ReadFile(filepath.Join(wc3ForgeExpectedPath, ".build.info"))
	if err != nil {
		t.Fatalf("read .build.info: %v", err)
	}
	if !strings.Contains(string(buildInfo), "Mead-integration-test") {
		t.Errorf(".build.info content unexpected: %q", buildInfo)
	}

	t.Logf("WC3FORGE_WC3_PATH=%q", wc3ForgeExpectedPath)
}
