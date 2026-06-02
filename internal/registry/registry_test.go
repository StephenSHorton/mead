package registry

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
)

// fakeWine writes a shell script that stands in for the wine binary: it
// satisfies bottles.Create's `--version` + `wineboot`, and emulates the
// `reg query` / `reg add` surface registry.Manager drives. Every
// invocation appends its argv to $MEAD_ARGLOG and its WINEPREFIX to
// $MEAD_PREFIXLOG so tests can assert what was actually run.
func fakeWine(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-wine")
	script := `#!/bin/sh
[ -n "$MEAD_ARGLOG" ] && printf '%s\n' "$*" >> "$MEAD_ARGLOG"
case "$1" in
  --version) echo "wine-fake-1.0"; exit 0 ;;
  wineboot)
    [ -z "$WINEPREFIX" ] && { echo "no prefix" >&2; exit 1; }
    mkdir -p "$WINEPREFIX/drive_c"; exit 0 ;;
  reg)
    [ -n "$MEAD_PREFIXLOG" ] && printf '%s\n' "$WINEPREFIX" >> "$MEAD_PREFIXLOG"
    sub="$2"; key="$3"
    case "$sub" in
      query)
        case "$key" in
          *Missing*) printf 'reg: Unable to find the specified registry key\r\n'; exit 1 ;;
          *) printf '\r\n%s\r\n    Greeting    REG_SZ    hello\r\n\r\n' "$key"; exit 0 ;;
        esac ;;
      add) printf 'reg: The operation completed successfully\r\n'; exit 0 ;;
      delete)
        case "$key" in
          *Missing*) printf 'reg: Unable to find the specified registry key\r\n'; exit 1 ;;
          *) printf 'The operation completed successfully.\r\n'; exit 0 ;;
        esac ;;
      *) echo "unknown reg sub: $sub" >&2; exit 2 ;;
    esac ;;
  *) echo "unknown cmd: $1" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake-wine: %v", err)
	}
	return bin
}

// newTestManager wires a registry.Manager (and the bottles.Manager it
// composes) against the fake wine, and returns a freshly-created bottle
// id plus the arg-log path.
func newTestManager(t *testing.T) (mgr *Manager, bottleID, argLog string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	argLog = filepath.Join(t.TempDir(), "args.log")
	t.Setenv("MEAD_ARGLOG", argLog)
	t.Setenv("MEAD_WINE_PATH", fakeWine(t))

	w := wine.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	b, err := bm.Create(context.Background(), "reg-test")
	if err != nil {
		t.Fatalf("create bottle: %v", err)
	}
	// Drop the create-time argv (version/wineboot) so per-test assertions
	// see only the reg call under test.
	_ = os.WriteFile(argLog, nil, 0o644)
	return New(s, bm, w, r), b.ID, argLog
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, _ := os.ReadFile(path)
	return string(data)
}

func TestQuery_HappyPath(t *testing.T) {
	m, id, _ := newTestManager(t)
	res, err := m.Query(context.Background(), id, `HKEY_CURRENT_USER\Software\MeadTest`, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Key != `HKEY_CURRENT_USER\Software\MeadTest` {
		t.Errorf("Key = %q", res.Key)
	}
	if len(res.Values) != 1 || res.Values[0].Name != "Greeting" || res.Values[0].Data != "hello" {
		t.Errorf("Values = %#v", res.Values)
	}
}

func TestQuery_NotFound(t *testing.T) {
	m, id, _ := newTestManager(t)
	_, err := m.Query(context.Background(), id, `HKEY_CURRENT_USER\Software\Missing`, "")
	if !errors.Is(err, ErrRegistryKeyNotFound) {
		t.Fatalf("err = %v, want ErrRegistryKeyNotFound", err)
	}
}

func TestQuery_EmptyKey(t *testing.T) {
	m, id, _ := newTestManager(t)
	if _, err := m.Query(context.Background(), id, "  ", ""); !errors.Is(err, ErrRegistryKeyRequired) {
		t.Fatalf("err = %v, want ErrRegistryKeyRequired", err)
	}
}

func TestQuery_UnknownBottle(t *testing.T) {
	m, _, _ := newTestManager(t)
	// A well-formed but nonexistent UUID — passes the UUIDIsh guard,
	// fails the load.
	_, err := m.Query(context.Background(), "00000000-0000-0000-0000-000000000000", `HKCU\Software\X`, "")
	if !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Fatalf("err = %v, want bottles.ErrBottleNotFound", err)
	}
}

func TestQuery_PassesValueFlag(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if _, err := m.Query(context.Background(), id, `HKCU\Software\MeadTest`, "Greeting"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := readLog(t, argLog); !strings.Contains(got, "/v Greeting") {
		t.Errorf("argv = %q, want it to contain /v Greeting", got)
	}
}

func TestQuery_SetsWinePrefix(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	argLog := filepath.Join(t.TempDir(), "args.log")
	prefixLog := filepath.Join(t.TempDir(), "prefix.log")
	t.Setenv("MEAD_ARGLOG", argLog)
	t.Setenv("MEAD_PREFIXLOG", prefixLog)
	t.Setenv("MEAD_WINE_PATH", fakeWine(t))
	w := wine.New()
	r := runner.New()
	bm := bottles.New(s, w, r)
	b, err := bm.Create(context.Background(), "px")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	m := New(s, bm, w, r)
	if _, err := m.Query(context.Background(), b.ID, `HKCU\Software\X`, ""); err != nil {
		t.Fatalf("Query: %v", err)
	}
	want, _ := s.PrefixDir(b.ID)
	if got := strings.TrimSpace(readLog(t, prefixLog)); got != want {
		t.Errorf("WINEPREFIX = %q, want %q", got, want)
	}
}

func TestSet_BuildsArgv(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if err := m.Set(context.Background(), id, `HKCU\Software\MeadTest`, "Greeting", "REG_SZ", "hello"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := readLog(t, argLog)
	for _, want := range []string{"reg add", "/v Greeting", "/t REG_SZ", "/d hello", "/f"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q missing %q", got, want)
		}
	}
}

func TestSet_DefaultsToSZ(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if err := m.Set(context.Background(), id, `HKCU\Software\MeadTest`, "Foo", "", "bar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := readLog(t, argLog); !strings.Contains(got, "/t REG_SZ") {
		t.Errorf("argv = %q, want default /t REG_SZ", got)
	}
}

func TestSet_KeyOnlyOmitsValueFlags(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if err := m.Set(context.Background(), id, `HKCU\Software\MeadTest`, "", "", ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := readLog(t, argLog)
	if !strings.Contains(got, "/f") {
		t.Errorf("argv = %q, want /f", got)
	}
	if strings.Contains(got, "/v") || strings.Contains(got, "/t") {
		t.Errorf("argv = %q, key-only Set must not pass /v or /t", got)
	}
}

func TestSet_InvalidType(t *testing.T) {
	m, id, _ := newTestManager(t)
	err := m.Set(context.Background(), id, `HKCU\Software\MeadTest`, "Foo", "REG_BOGUS", "x")
	if !errors.Is(err, ErrRegistryTypeInvalid) {
		t.Fatalf("err = %v, want ErrRegistryTypeInvalid", err)
	}
}

func TestSet_EmptyKey(t *testing.T) {
	m, id, _ := newTestManager(t)
	if err := m.Set(context.Background(), id, "", "Foo", "REG_SZ", "x"); !errors.Is(err, ErrRegistryKeyRequired) {
		t.Fatalf("err = %v, want ErrRegistryKeyRequired", err)
	}
}

func TestDelete_BuildsArgv(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if err := m.Delete(context.Background(), id, `HKCU\Software\MeadTest`, "Greeting"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := readLog(t, argLog)
	for _, want := range []string{"reg delete", "/v Greeting", "/f"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q missing %q", got, want)
		}
	}
}

func TestDelete_KeyOnlyOmitsValueFlag(t *testing.T) {
	m, id, argLog := newTestManager(t)
	if err := m.Delete(context.Background(), id, `HKCU\Software\MeadTest`, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := readLog(t, argLog)
	if !strings.Contains(got, "/f") {
		t.Errorf("argv = %q, want /f", got)
	}
	if strings.Contains(got, "/v") {
		t.Errorf("argv = %q, key-only Delete must not pass /v", got)
	}
}

func TestDelete_NotFoundIsIdempotent(t *testing.T) {
	m, id, _ := newTestManager(t)
	// Deleting something already gone must succeed (nil) — undo relies on
	// this to be a safe "make sure it's gone" inverse.
	if err := m.Delete(context.Background(), id, `HKCU\Software\Missing`, "x"); err != nil {
		t.Errorf("Delete of missing key should be nil, got %v", err)
	}
}

func TestDelete_EmptyKey(t *testing.T) {
	m, id, _ := newTestManager(t)
	if err := m.Delete(context.Background(), id, "  ", "x"); !errors.Is(err, ErrRegistryKeyRequired) {
		t.Fatalf("err = %v, want ErrRegistryKeyRequired", err)
	}
}
