// Package wine locates the Wine binary Mead spawns to run Windows code,
// reports its version, and exposes the environment variables Wine expects
// for prefix selection and feature flags.
//
// Resolution order for the Wine binary (first hit wins):
//
//  1. MEAD_WINE_PATH env var (full path to a `wine` / `wine64` binary).
//  2. The bundled GPTK Wine at <app>/Contents/Resources/wine/bin/wine64.
//  3. The gcenx "game-porting-toolkit" Homebrew *cask*, which installs a
//     ready-to-run GPTK Wine into "Game Porting Toolkit.app" under
//     /Applications (or ~/Applications).
//  4. $(brew --prefix game-porting-toolkit)/bin/wine64 — the Apple
//     *formula*, if Homebrew is installed and it's present.
//  5. PATH lookup of `wine64`, then `wine`.
//
// v0.1 ships bundled GPTK; the cask + Homebrew + PATH fallbacks exist so
// developers without the bundle in their build tree can still iterate.
// The cask (step 3) is the GPTK Mead is designed to run against, so a
// user who `brew install --cask gcenx/wine/game-porting-toolkit` is
// auto-detected without ever setting MEAD_WINE_PATH.
// This package does NOT spawn Wine for actual workloads — see runner.
// It DOES shell `wine --version` to populate Locator.Version(), because
// the version string is metadata-of-the-locator, not a unit of work.
package wine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Locator finds the Wine binary on the current machine. Construct once
// and reuse; resolution is performed lazily on the first Path() call
// and cached for the life of the Locator.
type Locator struct {
	mu          sync.Mutex
	resolved    bool
	path        string
	pathErr     error
	version     string
	versionErr  error
	versionDone bool

	// Hooks. Tests override these; production paths use the defaults
	// initialized in New(). Keeping them as fields rather than package
	// vars means parallel tests don't trample each other.
	lookupEnv   func(string) string
	stat        func(string) error
	lookPath    func(string) (string, error)
	executable  func() (string, error)
	userHomeDir func() (string, error)
	brewPrefix  func(formula string) (string, error)
	runCommand  func(name string, args ...string) ([]byte, error)

	// appDirs, when set (appDirsSet == true), overrides the directories
	// searched for the gcenx GPTK cask's "Game Porting Toolkit.app".
	// Default (unset) is /Applications + ~/Applications. See WithAppDirs.
	appDirs    []string
	appDirsSet bool
}

// Option configures a Locator at construction time.
type Option func(*Locator)

// WithAppDirs overrides the application directories searched for the
// gcenx "game-porting-toolkit" cask (default: /Applications and
// ~/Applications). Pass it to point Mead at a non-standard cask install
// location; pass it with no dirs to disable cask detection entirely
// (used by tests that need a deterministic "no wine on this host" state
// regardless of what's installed in /Applications).
func WithAppDirs(dirs ...string) Option {
	return func(l *Locator) {
		l.appDirs = dirs
		l.appDirsSet = true
	}
}

// New returns a Locator whose hooks use the real OS, configured by any
// supplied options. The first Path() call performs resolution and
// caches the result.
func New(opts ...Option) *Locator {
	l := &Locator{
		lookupEnv:   os.Getenv,
		stat:        statErr,
		lookPath:    exec.LookPath,
		executable:  os.Executable,
		userHomeDir: os.UserHomeDir,
		brewPrefix:  brewPrefix,
		runCommand:  runCommand,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// gptkCaskAppRelPath is where the gcenx "game-porting-toolkit" Homebrew
// cask drops the wine64 binary inside the installed .app bundle. Joined
// onto an application directory (/Applications or ~/Applications) to
// form a full candidate path.
const gptkCaskAppRelPath = "Game Porting Toolkit.app/Contents/Resources/wine/bin/wine64"

// Path returns the absolute path to the Wine binary Mead will spawn, or
// ErrWineNotFound (wrapped with the candidates that were tried) if no
// usable Wine could be located. The error explains which candidates
// were tried so the UI can render an actionable message.
func (l *Locator) Path() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.resolved {
		l.path, l.pathErr = l.resolve()
		l.resolved = true
	}
	return l.path, l.pathErr
}

// Version returns the version string reported by `<wine> --version`
// (typically "wine-9.0" or "wine-9.0 (Staging)"). Used for UI display
// and for tagging bottle metadata so we know which Wine created which
// prefix — relevant when the bundled binary updates and we need to
// decide whether existing bottles need migration.
//
// Implicitly resolves Path() first; if that fails the same error is
// surfaced here.
func (l *Locator) Version() (string, error) {
	path, err := l.Path()
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.versionDone {
		return l.version, l.versionErr
	}
	l.versionDone = true
	out, err := l.runCommand(path, "--version")
	if err != nil {
		l.versionErr = fmt.Errorf("run %q --version: %w", path, err)
		return "", l.versionErr
	}
	l.version = strings.TrimSpace(string(out))
	if l.version == "" {
		l.versionErr = fmt.Errorf("%q --version returned empty output", path)
		return "", l.versionErr
	}
	return l.version, nil
}

// Preamble returns the extra environment variables this particular Wine
// needs to run correctly — beyond WINEPREFIX, which the caller always
// sets. Callers (bottles.Create's wineboot, apps.Install/Launch) merge
// this UNDERNEATH the bottle's own env overrides so a user's env.set
// always wins.
//
// For an Apple GPTK / D3DMetal Wine — detected by a D3DMetal.framework
// sitting in <wineRoot>/lib/external — this returns:
//
//   - DYLD_FALLBACK_LIBRARY_PATH: prepends lib/external so the Mach-O
//     loader finds D3DMetal.framework, libd3dshared.dylib, DXMT, and
//     libMoltenVK.dylib (the macOS graphics-bridge libs the translated
//     d3d DLLs dlopen). Without it the d3d11/d3d12/dxgi builtins fail to
//     bind their backend and the app falls back to a dead GL path (or
//     renders nothing). /usr/local/lib:/usr/lib keeps the system default
//     fallbacks reachable.
//   - WINEDLLOVERRIDES: disables winemenubuilder/mscoree/mshtml so a
//     fresh prefix doesn't stall on a Mono/Gecko install prompt.
//   - ROSETTA_ADVERTISE_AVX: makes Rosetta advertise AVX so titles that
//     probe for it (Battle.net, many D3D games) take their fast path.
//   - WINEDEBUG=fixme-all: silences the noisy fixme channel. A CEF app
//     (Battle.net) under the default WINEDEBUG floods the log with
//     gigabytes of fixme:msvcp / fixme:d3dkmt — enough to threaten the
//     disk and stall GPU init. fixme-all keeps err+warn, which is what
//     the agent debug loop actually reads. Overridable per bottle.
//
// Returns an empty map (never nil — callers can range freely) for a
// plain Wine that needs no preamble, and propagates a Path() error.
//
// The D3DMetal Wine's own loader binary carries the
// allow-dyld-environment-variables + disable-library-validation
// entitlements, so the DYLD_ override survives exec into the hardened
// Wine process.
func (l *Locator) Preamble() (map[string]string, error) {
	path, err := l.Path()
	if err != nil {
		return nil, err
	}
	return preambleFor(path, l.stat), nil
}

// preambleFor is the pure core of Preamble: given a resolved Wine binary
// path and a stat hook, decide whether it's a D3DMetal Wine and return
// the matching env. Split out so tests can drive it without a real FS.
func preambleFor(winePath string, stat func(string) error) map[string]string {
	env := map[string]string{}
	// <wineRoot>/bin/<wine|wine64> → <wineRoot>.
	wineRoot := filepath.Dir(filepath.Dir(winePath))
	external := filepath.Join(wineRoot, "lib", "external")
	if stat(filepath.Join(external, "D3DMetal.framework")) != nil {
		return env // not a D3DMetal Wine — no preamble needed.
	}
	env["DYLD_FALLBACK_LIBRARY_PATH"] = external + ":/usr/local/lib:/usr/lib"
	env["WINEDLLOVERRIDES"] = "winemenubuilder.exe=d;mscoree=d;mshtml=d"
	env["ROSETTA_ADVERTISE_AVX"] = "1"
	env["WINEDEBUG"] = "fixme-all"
	return env
}

// resolve walks the candidate list in priority order, returning the
// first path that exists. The pathErr it returns on miss includes every
// candidate so the UI can render an actionable message.
func (l *Locator) resolve() (string, error) {
	var tried []string

	check := func(p string) (string, bool) {
		if p == "" {
			return "", false
		}
		tried = append(tried, p)
		if l.stat(p) == nil {
			return p, true
		}
		return "", false
	}

	// 1. Env var override.
	if p, ok := check(l.lookupEnv("MEAD_WINE_PATH")); ok {
		return p, nil
	}

	// 2. Bundled GPTK relative to the running binary. The Wails-built
	// bundle layout is <app>.app/Contents/MacOS/<binary>, so the
	// bundled Wine sits at ../Resources/wine/bin/wine64.
	if exe, err := l.executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources", "wine", "bin", "wine64")
		if p, ok := check(bundled); ok {
			return p, nil
		}
	}

	// 3. gcenx "game-porting-toolkit" Homebrew cask. Unlike the Apple
	// formula (step 4), the cask installs a ready-to-run GPTK Wine into
	// an app bundle under /Applications (the Homebrew default) or
	// ~/Applications. This is the GPTK Mead is designed to run against,
	// so prefer it over the formula — a plain stat, no shell-out.
	caskDirs := l.appDirs
	if !l.appDirsSet {
		caskDirs = []string{"/Applications"}
		if home, err := l.userHomeDir(); err == nil && home != "" {
			caskDirs = append(caskDirs, filepath.Join(home, "Applications"))
		}
	}
	for _, dir := range caskDirs {
		if p, ok := check(filepath.Join(dir, gptkCaskAppRelPath)); ok {
			return p, nil
		}
	}

	// 4. Homebrew GPTK formula. Shell out only when reachable — `brew`
	// may not be installed.
	if prefix, err := l.brewPrefix("game-porting-toolkit"); err == nil && prefix != "" {
		if p, ok := check(filepath.Join(prefix, "bin", "wine64")); ok {
			return p, nil
		}
	}

	// 5. PATH lookup. wine64 first (matches the 64-bit-only Apple
	// Silicon reality), then wine as fallback for older installs.
	for _, name := range []string{"wine64", "wine"} {
		p, err := l.lookPath(name)
		if err != nil {
			tried = append(tried, name+" (not on PATH)")
			continue
		}
		if p, ok := check(p); ok {
			return p, nil
		}
	}

	return "", fmt.Errorf("%w (tried: %s)", ErrWineNotFound, strings.Join(tried, "; "))
}

// statErr collapses os.Stat to "returns nil iff the path exists and is
// statable" — the existence-check the resolver needs.
func statErr(path string) error {
	_, err := os.Stat(path)
	return err
}

// brewPrefix runs `brew --prefix <formula>` with a short timeout and
// returns the trimmed stdout, or an error if brew isn't installed or
// the formula isn't present. Used as the third-priority Wine source.
func brewPrefix(formula string) (string, error) {
	cmd := exec.Command("brew", "--prefix", formula)
	// brew --prefix is fast (<100ms) when it works; cap to 2s to keep
	// startup snappy on machines without brew where the PATH lookup
	// itself dominates.
	done := make(chan struct{})
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return "", fmt.Errorf("brew --prefix %s: timed out", formula)
	}
	if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		return "", fmt.Errorf("brew --prefix %s: exited %d", formula, cmd.ProcessState.ExitCode())
	}
	return strings.TrimSpace(out.String()), nil
}

// runCommand runs a command and returns its combined stdout. Wine's
// --version prints a single line to stdout; CombinedOutput would also
// capture stderr which on macOS Wine prints unrelated dyld warnings —
// stick to stdout.
func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
