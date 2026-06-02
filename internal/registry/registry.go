// Package registry reads and writes Windows-registry entries inside a
// Mead bottle's Wine prefix.
//
// It works by shelling Wine's bundled `reg` tool — `wine reg query` to
// read, `wine reg add … /f` to write — synchronously through the runner
// and parsing the (byte-exact, version-stable) output. Registry access
// is the last gap in the agent's prefix-repair surface: env.set and
// dll.override cover environment + DLL behavior, but a lot of Windows
// compat knobs (CSMT, GL renderer selection, app-specific settings) live
// in the registry, and the agent needs to both inspect and set them.
//
// Composition mirrors apps.Manager (minus winetricks): store for prefix
// paths, bottles for the existence check + persisted env overrides, wine
// for the binary + GPTK preamble, runner for the spawn. It does NOT own
// wire transport — meadcore wraps it as registry.get / registry.set.
package registry

import (
	"context"
	"fmt"
	"strings"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

// regNotFoundMarker is the literal line `reg query` / `reg delete` print
// to STDOUT (alongside exit code 1) when the key or value is absent.
// Empirically verified against wine-11.0 (GPTK 3.0).
const regNotFoundMarker = "reg: Unable to find the specified registry key"

// validRegTypes is the set of registry value-type tokens Set accepts and
// forwards to `reg add /t`. Matches Windows reg.exe's writable types.
var validRegTypes = map[string]bool{
	"REG_SZ":        true,
	"REG_MULTI_SZ":  true,
	"REG_EXPAND_SZ": true,
	"REG_DWORD":     true,
	"REG_QWORD":     true,
	"REG_BINARY":    true,
	"REG_NONE":      true,
}

// Manager runs registry reads/writes against a bottle's prefix. One per
// process; meadcore.Core owns it.
type Manager struct {
	store   *store.Store
	bottles *bottles.Manager
	wine    *wine.Locator
	runner  *runner.Runner
}

// New returns a Manager wired to the given dependencies. All four are
// expected non-nil; meadcore.Core's constructor fills them.
func New(s *store.Store, b *bottles.Manager, w *wine.Locator, r *runner.Runner) *Manager {
	return &Manager{store: s, bottles: b, wine: w, runner: r}
}

// Value is a single registry value as rendered by `reg query`: its name,
// Wine type token (REG_SZ, REG_DWORD, …) and data. For REG_DWORD the
// data is normalized from Wine's "0x<hex>" rendering to a decimal
// string; all other types pass their rendered data through verbatim.
type Value struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
}

// QueryResult is what Query returns: the echoed full key path, the key's
// values (always non-nil — empty for a value-less key or a single-value
// miss), and the immediate subkey full paths (omitted for a /v query).
type QueryResult struct {
	Key     string   `json:"key"`
	Values  []Value  `json:"values"`
	Subkeys []string `json:"subkeys,omitempty"`
}

// Query reads a registry key — or one named value within it — from the
// bottle's prefix via `wine reg query`. key is a full Windows path such
// as `HKEY_CURRENT_USER\Software\Wine\Direct3D`. A non-empty valueName
// narrows the read to that single value (`/v <name>`); otherwise the
// whole key's values + immediate subkeys are returned.
//
// Synchronous (runner.Run) — reg query is fast and headless. A missing
// key/value maps to ErrRegistryKeyNotFound (exit 1 + the "Unable to
// find" marker); any other non-zero exit is wrapped with the captured
// output for the agent to read.
func (m *Manager) Query(ctx context.Context, bottleID, key, valueName string) (*QueryResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrRegistryKeyRequired
	}
	args := []string{"reg", "query", key}
	if v := strings.TrimSpace(valueName); v != "" {
		args = append(args, "/v", v)
	}
	res, err := m.runWine(ctx, bottleID, args)
	if err != nil {
		if res == nil {
			// Pre-run failure (unknown bottle / no wine binary).
			return nil, err
		}
		if res.ExitCode == 1 && strings.Contains(string(res.Output), regNotFoundMarker) {
			return nil, ErrRegistryKeyNotFound
		}
		return nil, fmt.Errorf("reg query exited %d: %w\noutput:\n%s", res.ExitCode, err, truncate(res.Output, 4096))
	}
	qr := parseRegQuery(res.Output, key)
	return &qr, nil
}

// Set writes a registry value into the bottle's prefix via `wine reg add
// … /f`. key is the full Windows path. When valueName is empty the key
// itself is created and no value is written (data/regType ignored).
// Otherwise regType (one of validRegTypes; empty defaults to REG_SZ)
// and data define the value.
//
// Synchronous (runner.Run). /f is ALWAYS passed so reg never blocks on
// an overwrite-confirmation prompt — a hang would be fatal to a headless
// MCP call. Returns nil on success; a wrapped ErrRegistryWriteFailed
// (with captured output) on a non-zero exit.
func (m *Manager) Set(ctx context.Context, bottleID, key, valueName, regType, data string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrRegistryKeyRequired
	}
	args := []string{"reg", "add", key}
	if valueName = strings.TrimSpace(valueName); valueName != "" {
		t := strings.ToUpper(strings.TrimSpace(regType))
		if t == "" {
			t = "REG_SZ"
		}
		if !validRegTypes[t] {
			return fmt.Errorf("%w: %q", ErrRegistryTypeInvalid, regType)
		}
		args = append(args, "/v", valueName, "/t", t, "/d", data)
	}
	args = append(args, "/f")

	res, err := m.runWine(ctx, bottleID, args)
	if err != nil {
		if res == nil {
			return err
		}
		return fmt.Errorf("%w: reg add exited %d\noutput:\n%s", ErrRegistryWriteFailed, res.ExitCode, truncate(res.Output, 4096))
	}
	return nil
}

// Delete removes a registry value (when valueName is non-empty) or the
// whole key (when valueName is empty) from the bottle's prefix via
// `wine reg delete … /f`. /f forces no-prompt, matching Set. It is the
// inverse history.undo applies for a registry.set that CREATED a value or
// key, so it is idempotent: deleting something already gone (exit 1 + the
// not-found marker `reg` prints) returns nil, mirroring store.DeleteBottle's
// "make sure this is gone" semantics.
//
// Synchronous (runner.Run). Deleting a key removes its whole subtree —
// history undo only does this for a key it confirmed it created, so
// there's nothing of the user's underneath to lose.
func (m *Manager) Delete(ctx context.Context, bottleID, key, valueName string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrRegistryKeyRequired
	}
	args := []string{"reg", "delete", key}
	if v := strings.TrimSpace(valueName); v != "" {
		args = append(args, "/v", v)
	}
	args = append(args, "/f")

	res, err := m.runWine(ctx, bottleID, args)
	if err != nil {
		if res == nil {
			return err
		}
		if res.ExitCode == 1 && strings.Contains(string(res.Output), regNotFoundMarker) {
			return nil // already absent — idempotent
		}
		return fmt.Errorf("%w: reg delete exited %d\noutput:\n%s", ErrRegistryWriteFailed, res.ExitCode, truncate(res.Output, 4096))
	}
	return nil
}

// runWine composes the bottle's Wine environment — the GPTK preamble,
// then the bottle's persisted env overrides, then WINEPREFIX last, the
// identical layering apps.specForBottle uses — and runs `wine <args…>`
// synchronously. Pre-run failures (unknown bottle, no wine binary)
// return (nil, err); a process that actually ran returns its non-nil
// Result alongside any non-zero-exit error from runner.Run, so callers
// can inspect ExitCode + Output to classify the failure.
func (m *Manager) runWine(ctx context.Context, bottleID string, wineArgs []string) (*runner.Result, error) {
	// bottles.Get enforces the UUIDIsh path-traversal guard (via
	// store.BottleDir) and maps a missing bottle to
	// bottles.ErrBottleNotFound for the handler.
	if _, err := m.bottles.Get(bottleID); err != nil {
		return nil, err
	}
	winePath, err := m.wine.Path()
	if err != nil {
		return nil, fmt.Errorf("locate wine: %w", err)
	}
	prefix, err := m.store.PrefixDir(bottleID)
	if err != nil {
		return nil, err
	}

	env := map[string]string{}
	if preamble, err := m.wine.Preamble(); err == nil {
		for k, v := range preamble {
			env[k] = v
		}
	}
	if overrides, err := m.bottles.EnvOverrides(bottleID); err == nil {
		for k, v := range overrides {
			env[k] = v
		}
	}
	env["WINEPREFIX"] = prefix

	return m.runner.Run(ctx, runner.Spec{
		Argv: append([]string{winePath}, wineArgs...),
		Env:  env,
	})
}

// truncate clips b to at most n bytes for inclusion in user-facing error
// messages. (Local copy — bottles has the same unexported helper; not
// worth exporting across packages for one use.)
func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return append(b[:n], []byte("\n[truncated]")...)
}
