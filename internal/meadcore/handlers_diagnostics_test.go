package meadcore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/runner"
	"github.com/StephenSHorton/mead/internal/store"
	"github.com/StephenSHorton/mead/internal/wine"
)

const diagBottleID = "77777777-7777-7777-7777-777777777777"

// newDiagCore builds a Core backed by a temp store, sufficient for the
// read-only Diagnostics handlers. No wine binary is involved — bottles
// are seeded by writing metadata directly.
func newDiagCore(t *testing.T) (*Core, *store.Store) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	r := runner.New()
	return &Core{Store: s, Runner: r, Bottles: bottles.New(s, wine.New(), r)}, s
}

func seedBottle(t *testing.T, s *store.Store, b *store.Bottle) {
	t.Helper()
	if err := s.SaveBottle(b); err != nil {
		t.Fatalf("SaveBottle: %v", err)
	}
}

// writeLog writes a log file under the bottle's logs/ dir and returns
// its full path.
func writeLog(t *testing.T, s *store.Store, id, name, content string) string {
	t.Helper()
	dir, err := s.LogsDir(id)
	if err != nil {
		t.Fatalf("LogsDir: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write log %s: %v", name, err)
	}
	return p
}

// writeSidecar drops a minimal sidecar next to a log so ReadLogInfo can
// recover the run-id / exit metadata.
func writeSidecar(t *testing.T, logPath, runID string, exitCode int) {
	t.Helper()
	body := fmt.Sprintf(`{"id":%q,"bottle_id":%q,"started_at":"2026-06-01T10:00:00.000Z","exited":true,"exit_code":%d}`,
		runID, diagBottleID, exitCode)
	if err := os.WriteFile(logPath+".json", []byte(body), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
}

func callJSON(t *testing.T, h func(json.RawMessage) (any, error), params any) any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	out, err := h(raw)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return out
}

// --- wire-contract decode tests ---------------------------------------

func TestLogsTailParams_Decode(t *testing.T) {
	var p logsTailParams
	if err := json.Unmarshal([]byte(`{"bottle_id":"b1","lines":50,"run_id":"r1"}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" || p.Lines != 50 || p.RunID != "r1" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestLogsSearchParams_Decode(t *testing.T) {
	var p logsSearchParams
	const raw = `{"bottle_id":"b1","pattern":"err","regex":true,"ignore_case":true,"max_results":10,"run_id":"r1"}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.BottleID != "b1" || p.Pattern != "err" || !p.Regex || !p.IgnoreCase || p.MaxResults != 10 || p.RunID != "r1" {
		t.Errorf("decoded wrong: %+v", p)
	}
}

func TestBottlesInspectParams_Decode(t *testing.T) {
	var p bottlesInspectParams
	if err := json.Unmarshal([]byte(`{"id":"b1","include_disk":true}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.ID != "b1" || !p.IncludeDisk {
		t.Errorf("decoded wrong: %+v", p)
	}
}

// --- logs.tail ---------------------------------------------------------

func TestLogsTail_NewestFileLastLines(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "100-1.log", "old-a\nold-b\n")
	writeLog(t, s, diagBottleID, "200-2.log", "line1\nline2\nline3\nline4\n")

	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID, Lines: 2}).(logsTailResult)
	if out.File != "200-2.log" {
		t.Errorf("File = %q, want newest 200-2.log", out.File)
	}
	if out.Missing || out.Truncated {
		t.Errorf("unexpected flags: %+v", out)
	}
	want := []string{"line3", "line4"}
	if len(out.Lines) != 2 || out.Lines[0] != want[0] || out.Lines[1] != want[1] {
		t.Errorf("Lines = %v, want %v", out.Lines, want)
	}
}

func TestLogsTail_Missing(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID}).(logsTailResult)
	if !out.Missing {
		t.Errorf("expected Missing=true, got %+v", out)
	}
	if out.Lines == nil {
		t.Error("Lines must never be null")
	}
}

func TestLogsTail_DetectsTruncation(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "1-1.log", "head\n"+runner.TruncationMarker+"\n")
	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID}).(logsTailResult)
	if !out.Truncated {
		t.Errorf("expected Truncated=true, got %+v", out)
	}
}

func TestLogsTail_FewerLinesThanRequested(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	// No trailing newline on the last line — must still be returned.
	writeLog(t, s, diagBottleID, "1-1.log", "only1\nonly2")
	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID, Lines: 100}).(logsTailResult)
	if len(out.Lines) != 2 || out.Lines[1] != "only2" {
		t.Errorf("Lines = %v, want [only1 only2]", out.Lines)
	}
}

func TestLogsTail_ExactlyNLines(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "1-1.log", "a\nb\nc\n")
	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID, Lines: 3}).(logsTailResult)
	if len(out.Lines) != 3 || out.Lines[0] != "a" || out.Lines[2] != "c" {
		t.Errorf("Lines = %v, want [a b c]", out.Lines)
	}
}

func TestLogsTail_LongLineSpansChunks(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	// A single line longer than the 64 KiB backward-read chunk must come
	// back intact — the reader is byte-based, not a token-capped Scanner.
	long := strings.Repeat("x", 70000)
	writeLog(t, s, diagBottleID, "1-1.log", long+"\ntail\n")
	out := callJSON(t, c.handleLogsTail, logsTailParams{BottleID: diagBottleID, Lines: 2}).(logsTailResult)
	if len(out.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(out.Lines))
	}
	if len(out.Lines[0]) != 70000 {
		t.Errorf("long line truncated: got %d bytes, want 70000", len(out.Lines[0]))
	}
	if out.Lines[1] != "tail" {
		t.Errorf("Lines[1] = %q, want tail", out.Lines[1])
	}
}

func TestLogsTail_RunIDWrongBottle(t *testing.T) {
	c, _ := newDiagCore(t)
	// An unknown run id should surface ErrProcessNotFound.
	raw, _ := json.Marshal(logsTailParams{BottleID: diagBottleID, RunID: "nope"})
	if _, err := c.handleLogsTail(raw); !errors.Is(err, runner.ErrProcessNotFound) {
		t.Errorf("expected ErrProcessNotFound, got %v", err)
	}
}

// --- logs.search -------------------------------------------------------

func TestLogsSearch_Substring(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	lp := writeLog(t, s, diagBottleID, "1-1.log", "ok line\nerr: boom\nok again\nerr: again\n")
	writeSidecar(t, lp, "run-123", 0)

	out := callJSON(t, c.handleLogsSearch, logsSearchParams{BottleID: diagBottleID, Pattern: "err:"}).(logsSearchResult)
	if len(out.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d (%+v)", len(out.Matches), out.Matches)
	}
	if out.Matches[0].LineNumber != 2 || out.Matches[0].Line != "err: boom" {
		t.Errorf("match[0] = %+v", out.Matches[0])
	}
	if out.Matches[0].RunID != "run-123" {
		t.Errorf("run_id not mapped from sidecar: %q", out.Matches[0].RunID)
	}
	if out.FilesSearched != 1 {
		t.Errorf("FilesSearched = %d, want 1", out.FilesSearched)
	}
}

func TestLogsSearch_RegexIgnoreCase(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "1-1.log", "WARNING here\nnothing\nWarn too\n")
	out := callJSON(t, c.handleLogsSearch, logsSearchParams{
		BottleID: diagBottleID, Pattern: "^warn", Regex: true, IgnoreCase: true,
	}).(logsSearchResult)
	if len(out.Matches) != 2 {
		t.Errorf("expected 2 case-insensitive regex matches, got %d", len(out.Matches))
	}
}

func TestLogsSearch_Truncates(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "1-1.log", "hit\nhit\nhit\nhit\n")
	out := callJSON(t, c.handleLogsSearch, logsSearchParams{
		BottleID: diagBottleID, Pattern: "hit", MaxResults: 2,
	}).(logsSearchResult)
	if len(out.Matches) != 2 || !out.Truncated {
		t.Errorf("expected 2 matches + Truncated, got %d matches truncated=%v", len(out.Matches), out.Truncated)
	}
}

func TestLogsSearch_ExactLimitNotTruncated(t *testing.T) {
	// Matches landing EXACTLY on max_results must NOT be flagged
	// truncated — nothing was dropped. (Regression guard for the
	// false-positive the post-append limit check used to produce.)
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	writeLog(t, s, diagBottleID, "1-1.log", "hit\nhit\nmiss\n")
	out := callJSON(t, c.handleLogsSearch, logsSearchParams{
		BottleID: diagBottleID, Pattern: "hit", MaxResults: 2,
	}).(logsSearchResult)
	if len(out.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(out.Matches))
	}
	if out.Truncated {
		t.Error("Truncated must be false when matches == max_results exactly")
	}
}

func TestLogsSearch_PatternRequired(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	raw, _ := json.Marshal(logsSearchParams{BottleID: diagBottleID, Pattern: "   "})
	if _, err := c.handleLogsSearch(raw); err == nil {
		t.Error("expected error for blank pattern")
	}
}

func TestLogsSearch_BadRegex(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	raw, _ := json.Marshal(logsSearchParams{BottleID: diagBottleID, Pattern: "(", Regex: true})
	if _, err := c.handleLogsSearch(raw); err == nil {
		t.Error("expected compile error for bad regex")
	}
}

func TestLogsSearch_NoLogs(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	out := callJSON(t, c.handleLogsSearch, logsSearchParams{BottleID: diagBottleID, Pattern: "x"}).(logsSearchResult)
	if len(out.Matches) != 0 || out.FilesSearched != 0 {
		t.Errorf("expected zero matches/files, got %+v", out)
	}
	if out.Matches == nil {
		t.Error("Matches must never be null")
	}
}

// --- bottles.inspect ---------------------------------------------------

func TestBottlesInspect_Basic(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{
		ID:          diagBottleID,
		Name:        "Diablo II",
		CreatedAt:   time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		WineVersion: "wine-11.0",
		EnvOverrides: map[string]string{
			"DXVK_HUD":         "fps",
			"WINEDLLOVERRIDES": "d3d11=native,builtin;dxgi=native",
		},
	})
	// Materialize the prefix dir + a registry hive + a log.
	prefix, _ := s.PrefixDir(diagBottleID)
	if err := os.MkdirAll(prefix, 0o755); err != nil {
		t.Fatalf("mkdir prefix: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "user.reg"), []byte("WINE REGISTRY"), 0o644); err != nil {
		t.Fatalf("write user.reg: %v", err)
	}
	lp := writeLog(t, s, diagBottleID, "1-1.log", "hello\n")
	writeSidecar(t, lp, "run-xyz", 0)

	out := callJSON(t, c.handleBottlesInspect, bottlesInspectParams{ID: diagBottleID}).(bottlesInspectResult)
	if out.ID != diagBottleID || out.Name != "Diablo II" || out.WineVersion != "wine-11.0" {
		t.Errorf("scalar fields wrong: %+v", out)
	}
	if out.CreatedAt != "2026-06-01T12:00:00Z" {
		t.Errorf("CreatedAt = %q (want bottles.*-consistent second-precision)", out.CreatedAt)
	}
	if out.EnvOverrides["DXVK_HUD"] != "fps" {
		t.Errorf("env not surfaced: %+v", out.EnvOverrides)
	}
	if out.DLLOverrides["d3d11"] != "native,builtin" || out.DLLOverrides["dxgi"] != "native" {
		t.Errorf("dll overrides not parsed: %+v", out.DLLOverrides)
	}
	if out.HasUserReg != true || out.HasSystemReg != false {
		t.Errorf("reg presence wrong: user=%v system=%v", out.HasUserReg, out.HasSystemReg)
	}
	if len(out.LogFiles) != 1 || out.LogFiles[0].File != "1-1.log" || out.LogFiles[0].RunID != "run-xyz" {
		t.Errorf("log_files wrong: %+v", out.LogFiles)
	}
	if out.LogFiles[0].SizeBytes != int64(len("hello\n")) {
		t.Errorf("log size = %d", out.LogFiles[0].SizeBytes)
	}
	if out.DiskBytes != nil {
		t.Errorf("DiskBytes should be nil without include_disk, got %v", *out.DiskBytes)
	}
	if out.Processes == nil || out.LogFiles == nil {
		t.Error("slice fields must never be null")
	}
	// Wire contract: a clean exit (code 0) must stay visible, not be
	// dropped by omitempty.
	js, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(js), `"exit_code":0`) {
		t.Errorf("exit_code:0 should be present for an exited log; json=%s", js)
	}
}

func TestBottlesInspect_NotFound(t *testing.T) {
	c, _ := newDiagCore(t)
	raw, _ := json.Marshal(bottlesInspectParams{ID: "88888888-8888-8888-8888-888888888888"})
	if _, err := c.handleBottlesInspect(raw); !errors.Is(err, bottles.ErrBottleNotFound) {
		t.Errorf("expected ErrBottleNotFound, got %v", err)
	}
}

func TestBottlesInspect_IncludeDisk(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	prefix, _ := s.PrefixDir(diagBottleID)
	if err := os.MkdirAll(filepath.Join(prefix, "drive_c"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "drive_c", "a.bin"), make([]byte, 1234), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := callJSON(t, c.handleBottlesInspect, bottlesInspectParams{ID: diagBottleID, IncludeDisk: true}).(bottlesInspectResult)
	if out.DiskBytes == nil {
		t.Fatal("DiskBytes should be set with include_disk")
	}
	if *out.DiskBytes != 1234 {
		t.Errorf("DiskBytes = %d, want 1234", *out.DiskBytes)
	}
}

func TestBottlesInspect_EnvNeverNull(t *testing.T) {
	c, s := newDiagCore(t)
	seedBottle(t, s, &store.Bottle{ID: diagBottleID, Name: "b", CreatedAt: time.Now().UTC()})
	out := callJSON(t, c.handleBottlesInspect, bottlesInspectParams{ID: diagBottleID}).(bottlesInspectResult)
	if out.EnvOverrides == nil || out.DLLOverrides == nil {
		t.Error("env/dll override maps must never be null")
	}
}

// --- parseDLLOverrides -------------------------------------------------

func TestParseDLLOverrides(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]string
	}{
		{"", map[string]string{}},
		{"d3d11=native,builtin", map[string]string{"d3d11": "native,builtin"}},
		{"d3d11=native;dxgi=builtin", map[string]string{"d3d11": "native", "dxgi": "builtin"}},
		{"msvcp140=", map[string]string{"msvcp140": ""}}, // disabled
		{"d3d10,d3d11=native", map[string]string{"d3d10": "native", "d3d11": "native"}},
		{"weird-no-equals;dxgi=native", map[string]string{"dxgi": "native"}},
	}
	for _, c := range cases {
		got := parseDLLOverrides(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseDLLOverrides(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("parseDLLOverrides(%q)[%q] = %q, want %q", c.in, k, got[k], v)
			}
		}
	}
}
