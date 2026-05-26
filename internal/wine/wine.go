// Package wine locates the Wine binary Mead spawns to run Windows code,
// reports its version, and exposes the environment variables Wine expects
// for prefix selection and feature flags.
//
// Resolution order for the Wine binary (first hit wins):
//
//  1. MEAD_WINE_PATH env var (full path to a `wine` or `wine64` binary).
//  2. The bundled GPTK Wine at <app>/Contents/Resources/wine/bin/wine64.
//  3. $(brew --prefix game-porting-toolkit)/bin/wine64 if Homebrew is
//     installed and the formula is present.
//  4. PATH lookup of `wine64`, then `wine`.
//
// v0.1 ships bundled GPTK; the Homebrew + PATH fallbacks exist so
// developers without the bundle in their build tree can still iterate.
// This package does NOT spawn Wine — see runner.
package wine

// Locator finds the Wine binary on the current machine. Construct once
// and reuse; resolution is cached so we don't restat on every call.
type Locator struct {
	cached string
}

// New returns an unconfigured Locator. The first Path() call performs
// resolution and caches the result.
func New() *Locator { return &Locator{} }

// Path returns the absolute path to the Wine binary Mead will spawn, or
// an error if no Wine could be located. The error explains which
// candidates were tried, so the UI can render an actionable message
// (typically "open Settings → re-bundle Wine" or "brew install
// game-porting-toolkit").
func (l *Locator) Path() (string, error) {
	return "", errNotImplemented
}

// Version returns the version string reported by `wine --version`
// (typically something like "wine-9.0-staging"). Used for UI display
// and for tagging bottle metadata so we know which Wine created which
// prefix (relevant for migration when we update the bundled binary).
func (l *Locator) Version() (string, error) {
	return "", errNotImplemented
}
