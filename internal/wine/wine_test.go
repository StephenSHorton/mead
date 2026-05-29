package wine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newLocatorWithStubs builds a Locator whose hooks the test fully owns.
// Each candidate becomes "exists?" via the stat set; lookupEnv returns
// the supplied env map; lookPath returns supplied entries or "not found"
// for anything else; executable returns the supplied path or its
// configured error; brewPrefix returns supplied prefix or its error.
//
// Default ALL hooks to "miss" — tests that care about a layer override
// just that layer.
type stubs struct {
	env         map[string]string
	exists      map[string]bool // path → exists?
	pathLookup  map[string]string
	exe         string
	exeErr      error
	brewPath    string
	brewErr     error
	runOutput   []byte
	runErr      error
}

func newLocatorWithStubs(s stubs) *Locator {
	l := New()
	l.lookupEnv = func(k string) string {
		if s.env == nil {
			return ""
		}
		return s.env[k]
	}
	l.stat = func(p string) error {
		if s.exists != nil && s.exists[p] {
			return nil
		}
		return os.ErrNotExist
	}
	l.lookPath = func(name string) (string, error) {
		if p, ok := s.pathLookup[name]; ok {
			return p, nil
		}
		return "", errors.New("not found in PATH")
	}
	l.executable = func() (string, error) {
		return s.exe, s.exeErr
	}
	l.brewPrefix = func(formula string) (string, error) {
		return s.brewPath, s.brewErr
	}
	l.runCommand = func(name string, args ...string) ([]byte, error) {
		return s.runOutput, s.runErr
	}
	return l
}

func TestPath_EnvOverrideWins(t *testing.T) {
	// Even with a bundled GPTK present, MEAD_WINE_PATH should be
	// preferred — that's the developer's escape hatch.
	envPath := "/Users/me/custom/wine64"
	bundlePath := "/Users/me/Mead.app/Contents/Resources/wine/bin/wine64"
	l := newLocatorWithStubs(stubs{
		env:    map[string]string{"MEAD_WINE_PATH": envPath},
		exists: map[string]bool{envPath: true, bundlePath: true},
		exe:    "/Users/me/Mead.app/Contents/MacOS/Mead",
	})

	got, err := l.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != envPath {
		t.Errorf("Path = %q; want %q", got, envPath)
	}
}

func TestPath_BundledGPTKBeatsBrewAndPath(t *testing.T) {
	bundlePath := "/Users/me/Mead.app/Contents/Resources/wine/bin/wine64"
	brewPath := "/opt/homebrew/opt/game-porting-toolkit/bin/wine64"
	pathPath := "/usr/local/bin/wine64"
	l := newLocatorWithStubs(stubs{
		exists: map[string]bool{
			bundlePath: true, brewPath: true, pathPath: true,
		},
		exe:        "/Users/me/Mead.app/Contents/MacOS/Mead",
		brewPath:   "/opt/homebrew/opt/game-porting-toolkit",
		pathLookup: map[string]string{"wine64": pathPath},
	})

	got, err := l.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != bundlePath {
		t.Errorf("Path = %q; want bundled %q", got, bundlePath)
	}
}

func TestPath_BrewBeatsPath(t *testing.T) {
	brewBin := "/opt/homebrew/opt/game-porting-toolkit/bin/wine64"
	pathBin := "/usr/local/bin/wine64"
	l := newLocatorWithStubs(stubs{
		exists:     map[string]bool{brewBin: true, pathBin: true},
		exe:        "/tmp/Mead.app/Contents/MacOS/Mead", // bundle path won't exist via stat
		brewPath:   "/opt/homebrew/opt/game-porting-toolkit",
		pathLookup: map[string]string{"wine64": pathBin},
	})

	got, err := l.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != brewBin {
		t.Errorf("Path = %q; want brew %q", got, brewBin)
	}
}

func TestPath_PathWine64BeatsPlainWine(t *testing.T) {
	w64 := "/usr/local/bin/wine64"
	w := "/usr/local/bin/wine"
	l := newLocatorWithStubs(stubs{
		exists:     map[string]bool{w64: true, w: true},
		exe:        "/missing",
		brewErr:    errors.New("brew not installed"),
		pathLookup: map[string]string{"wine64": w64, "wine": w},
	})

	got, err := l.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != w64 {
		t.Errorf("Path = %q; want wine64 %q", got, w64)
	}
}

func TestPath_AllMissReturnsErrWineNotFound(t *testing.T) {
	l := newLocatorWithStubs(stubs{
		exe:     "/missing",
		brewErr: errors.New("brew not installed"),
	})

	_, err := l.Path()
	if err == nil {
		t.Fatal("Path: expected error, got nil")
	}
	if !errors.Is(err, ErrWineNotFound) {
		t.Errorf("expected ErrWineNotFound, got %v", err)
	}
	// The error message should enumerate what was tried so the UI can
	// render an actionable hint.
	msg := err.Error()
	// The error message should mention wine64 + wine being checked on
	// PATH and report both as "not on PATH" when lookPath fails.
	for _, want := range []string{"wine64 (not on PATH)", "wine (not on PATH)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
}

func TestPath_Cached(t *testing.T) {
	// Resolve once; subsequent calls should NOT re-stat or re-look-up.
	calls := 0
	l := New()
	l.lookupEnv = func(string) string { return "/tmp/fake-wine" }
	l.stat = func(p string) error {
		calls++
		return nil
	}
	if _, err := l.Path(); err != nil {
		t.Fatalf("Path #1: %v", err)
	}
	first := calls
	for i := 0; i < 5; i++ {
		if _, err := l.Path(); err != nil {
			t.Fatalf("Path #%d: %v", i+2, err)
		}
	}
	if calls != first {
		t.Errorf("expected resolution to be cached; stat called %d times after first call", calls-first)
	}
}

func TestVersion_ReadsAndTrims(t *testing.T) {
	l := newLocatorWithStubs(stubs{
		env:       map[string]string{"MEAD_WINE_PATH": "/tmp/fake-wine"},
		exists:    map[string]bool{"/tmp/fake-wine": true},
		runOutput: []byte("wine-9.0\n"),
	})

	ver, err := l.Version()
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if ver != "wine-9.0" {
		t.Errorf("Version = %q; want %q", ver, "wine-9.0")
	}
}

func TestVersion_PropagatesPathError(t *testing.T) {
	l := newLocatorWithStubs(stubs{
		exe:     "/missing",
		brewErr: errors.New("brew not installed"),
	})

	_, err := l.Version()
	if !errors.Is(err, ErrWineNotFound) {
		t.Errorf("Version: expected ErrWineNotFound, got %v", err)
	}
}

func TestVersion_Cached(t *testing.T) {
	calls := 0
	l := newLocatorWithStubs(stubs{
		env:    map[string]string{"MEAD_WINE_PATH": "/tmp/fake-wine"},
		exists: map[string]bool{"/tmp/fake-wine": true},
	})
	l.runCommand = func(string, ...string) ([]byte, error) {
		calls++
		return []byte("wine-9.0"), nil
	}
	for i := 0; i < 3; i++ {
		if _, err := l.Version(); err != nil {
			t.Fatalf("Version #%d: %v", i+1, err)
		}
	}
	if calls != 1 {
		t.Errorf("expected runCommand called once; got %d", calls)
	}
}

func TestPreamble_D3DMetalWineSetsDyldAndOverrides(t *testing.T) {
	winePath := "/Applications/D4Mac.app/Contents/SharedSupport/Wine/bin/wine"
	framework := "/Applications/D4Mac.app/Contents/SharedSupport/Wine/lib/external/D3DMetal.framework"
	l := newLocatorWithStubs(stubs{
		env:    map[string]string{"MEAD_WINE_PATH": winePath},
		exists: map[string]bool{winePath: true, framework: true},
	})

	env, err := l.Preamble()
	if err != nil {
		t.Fatalf("Preamble: %v", err)
	}
	wantDyld := "/Applications/D4Mac.app/Contents/SharedSupport/Wine/lib/external:/usr/local/lib:/usr/lib"
	if env["DYLD_FALLBACK_LIBRARY_PATH"] != wantDyld {
		t.Errorf("DYLD_FALLBACK_LIBRARY_PATH = %q; want %q", env["DYLD_FALLBACK_LIBRARY_PATH"], wantDyld)
	}
	if env["WINEDLLOVERRIDES"] != "winemenubuilder.exe=d;mscoree=d;mshtml=d" {
		t.Errorf("WINEDLLOVERRIDES = %q", env["WINEDLLOVERRIDES"])
	}
	if env["ROSETTA_ADVERTISE_AVX"] != "1" {
		t.Errorf("ROSETTA_ADVERTISE_AVX = %q; want 1", env["ROSETTA_ADVERTISE_AVX"])
	}
}

func TestPreamble_PlainWineReturnsEmpty(t *testing.T) {
	// A wine with no D3DMetal.framework alongside it needs no preamble.
	winePath := "/usr/local/bin/wine64"
	l := newLocatorWithStubs(stubs{
		env:    map[string]string{"MEAD_WINE_PATH": winePath},
		exists: map[string]bool{winePath: true}, // framework absent
	})

	env, err := l.Preamble()
	if err != nil {
		t.Fatalf("Preamble: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("expected empty preamble for plain wine, got %v", env)
	}
}

func TestPreamble_PropagatesPathError(t *testing.T) {
	l := newLocatorWithStubs(stubs{
		exe:     "/missing",
		brewErr: errors.New("brew not installed"),
	})
	if _, err := l.Preamble(); !errors.Is(err, ErrWineNotFound) {
		t.Errorf("Preamble: expected ErrWineNotFound, got %v", err)
	}
}

func TestPath_BundleLayoutResolution(t *testing.T) {
	// Verify we look exactly at <bundle>/Contents/Resources/wine/bin/wine64
	// when the running binary is at <bundle>/Contents/MacOS/<binary>.
	exe := "/Users/me/Mead.app/Contents/MacOS/Mead"
	want := filepath.Join("/Users/me/Mead.app/Contents/Resources/wine/bin/wine64")
	l := newLocatorWithStubs(stubs{
		exists: map[string]bool{want: true},
		exe:    exe,
	})
	got, err := l.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != want {
		t.Errorf("Path = %q; want %q", got, want)
	}
}
