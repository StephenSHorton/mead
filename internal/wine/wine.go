// Package wine locates the Wine binary Mead spawns to run Windows code,
// reports its version, and exposes the environment variables Wine expects
// for prefix selection and feature flags.
//
// Resolution order for the Wine binary (first hit wins):
//
//  1. MEAD_WINE_PATH env var (full path to a `wine` / `wine64` binary).
//  2. The bundled GPTK Wine at <app>/Contents/Resources/wine/bin/wine64.
//  3. $(brew --prefix game-porting-toolkit)/bin/wine64 if Homebrew is
//     installed and the formula is present.
//  4. PATH lookup of `wine64`, then `wine`.
//
// v0.1 ships bundled GPTK; the Homebrew + PATH fallbacks exist so
// developers without the bundle in their build tree can still iterate.
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
	lookupEnv  func(string) string
	stat       func(string) error
	lookPath   func(string) (string, error)
	executable func() (string, error)
	brewPrefix func(formula string) (string, error)
	runCommand func(name string, args ...string) ([]byte, error)
}

// New returns an unconfigured Locator whose hooks use the real OS.
// The first Path() call performs resolution and caches the result.
func New() *Locator {
	return &Locator{
		lookupEnv:  os.Getenv,
		stat:       statErr,
		lookPath:   exec.LookPath,
		executable: os.Executable,
		brewPrefix: brewPrefix,
		runCommand: runCommand,
	}
}

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

	// 3. Homebrew GPTK. Shell out only when reachable — `brew` may not
	// be installed.
	if prefix, err := l.brewPrefix("game-porting-toolkit"); err == nil && prefix != "" {
		if p, ok := check(filepath.Join(prefix, "bin", "wine64")); ok {
			return p, nil
		}
	}

	// 4. PATH lookup. wine64 first (matches the 64-bit-only Apple
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
