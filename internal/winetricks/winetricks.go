// Package winetricks locates the winetricks binary on the host.
//
// Winetricks is a separate bash script from wine itself, distributed
// independently of GPTK. Most macOS users install it via
// `brew install winetricks`. Mead doesn't bundle it for v0.2 — the
// binary is small (~500K bash script) and depends on system tools
// (curl, unzip, cabextract) that brew also brings in, so bundling
// it without those dependencies is fragile. Better UX: detect it,
// surface a clear "brew install winetricks" hint if missing.
//
// Locator mirrors the structure of wine.Locator: lazy resolution,
// cached, injectable hooks for testing.
package winetricks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Locator finds the winetricks binary on the current machine.
type Locator struct {
	mu       sync.Mutex
	resolved bool
	path     string
	pathErr  error

	lookupEnv  func(string) string
	stat       func(string) error
	lookPath   func(string) (string, error)
	brewPrefix func(formula string) (string, error)
}

// New returns a Locator wired to the real OS.
func New() *Locator {
	return &Locator{
		lookupEnv:  os.Getenv,
		stat:       statErr,
		lookPath:   exec.LookPath,
		brewPrefix: brewPrefix,
	}
}

// Path returns the absolute path to a winetricks binary, or an error
// listing the candidates that were checked. Resolution order:
//
//  1. MEAD_WINETRICKS_PATH env var.
//  2. $(brew --prefix winetricks)/bin/winetricks.
//  3. PATH lookup.
func (l *Locator) Path() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.resolved {
		l.path, l.pathErr = l.resolve()
		l.resolved = true
	}
	return l.path, l.pathErr
}

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

	if p, ok := check(l.lookupEnv("MEAD_WINETRICKS_PATH")); ok {
		return p, nil
	}
	if prefix, err := l.brewPrefix("winetricks"); err == nil && prefix != "" {
		if p, ok := check(filepath.Join(prefix, "bin", "winetricks")); ok {
			return p, nil
		}
	}
	if p, err := l.lookPath("winetricks"); err == nil {
		if p, ok := check(p); ok {
			return p, nil
		}
	} else {
		tried = append(tried, "winetricks (not on PATH)")
	}

	return "", fmt.Errorf("%w (tried: %s)", ErrNotFound, strings.Join(tried, "; "))
}

func statErr(p string) error {
	_, err := os.Stat(p)
	return err
}

func brewPrefix(formula string) (string, error) {
	out, err := exec.Command("brew", "--prefix", formula).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
