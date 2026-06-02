package meadcore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/StephenSHorton/mead/internal/runner"
)

// The Diagnostics surface gives the agent read-only visibility into a
// bottle's accumulated logs and current state — distinct from the
// process.* surface, which is keyed by a single live/recent RunID.
// logs.tail / logs.search aggregate across a bottle's whole run history;
// bottles.inspect is a one-shot snapshot of everything Mead knows about a
// bottle. All three are pure reads — no prefix is mutated.

// --- logs.tail ---------------------------------------------------------

// tailMaxLines caps logs.tail so a pathological request can't pull a
// whole 64 MiB log into memory as one giant JSON array.
const tailMaxLines = 5000

// tailDefaultLines is the line count when the caller omits `lines`.
const tailDefaultLines = 200

type logsTailParams struct {
	BottleID string `json:"bottle_id"`
	// Lines is the number of trailing lines to return. <=0 → default
	// (200); values above tailMaxLines are clamped.
	Lines int `json:"lines"`
	// RunID, when set, tails that one process's log instead of the
	// bottle's newest. The process must belong to BottleID.
	RunID string `json:"run_id"`
}

type logsTailResult struct {
	// File is the basename of the log that was tailed ("" when missing).
	File string `json:"file"`
	// Lines are the trailing lines, oldest-first; never null.
	Lines []string `json:"lines"`
	// Truncated is true when the log hit the 64 MiB cap, meaning output
	// past the cap was dropped — so these may not be the program's true
	// final lines.
	Truncated bool `json:"truncated"`
	// Missing is true when the bottle has no log files yet.
	Missing bool `json:"missing"`
}

func (c *Core) handleLogsTail(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p logsTailParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	n := p.Lines
	if n <= 0 {
		n = tailDefaultLines
	}
	if n > tailMaxLines {
		n = tailMaxLines
	}

	path, file, missing, err := c.resolveLogFile(p.BottleID, p.RunID)
	if err != nil {
		return nil, err
	}
	if missing {
		return logsTailResult{File: "", Lines: []string{}, Missing: true}, nil
	}

	lines, truncated, err := tailFile(path, n)
	if err != nil {
		// A file present at resolve time but gone now → treat as missing
		// rather than erroring; logs are transient by nature.
		if errors.Is(err, os.ErrNotExist) {
			return logsTailResult{File: "", Lines: []string{}, Missing: true}, nil
		}
		return nil, err
	}
	return logsTailResult{File: file, Lines: lines, Truncated: truncated}, nil
}

// tailMaxBytes bounds how far back tailFile reads. n lines of normal
// Wine output is far below this; the ceiling only bites a degenerate,
// newline-sparse log — where counting to n newlines would otherwise pull
// the whole file (up to the 64 MiB cappedWriter limit) into memory.
const tailMaxBytes = 16 << 20

// tailFile returns up to n trailing lines of the file at path plus
// whether the runner truncation marker is present. It seeks backward in
// 64 KiB chunks, stopping once it has n+1 newlines, hits BOF, or reaches
// the tailMaxBytes ceiling — so memory is bounded to ~min(n lines,
// tailMaxBytes) rather than the whole (up-to-64 MiB) file. Byte-based,
// not bufio.Scanner, so an over-long Wine line is never silently dropped.
func tailFile(path string, n int) ([]string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := fi.Size()
	if size == 0 {
		return []string{}, false, nil
	}

	const chunk = 64 * 1024
	var buf []byte
	pos := size
	hitCeiling := false
	// Read backward until we have more than n newlines (guaranteeing n
	// full lines once a trailing newline is dropped), hit BOF, or reach
	// the byte ceiling.
	for pos > 0 && bytes.Count(buf, []byte{'\n'}) <= n {
		if len(buf) >= tailMaxBytes {
			hitCeiling = true
			break
		}
		readSize := int64(chunk)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		b := make([]byte, readSize)
		if _, err := f.ReadAt(b, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, false, err
		}
		buf = append(b, buf...)
	}

	text := strings.TrimSuffix(string(buf), "\n")
	truncated := strings.Contains(text, runner.TruncationMarker)
	lines := strings.Split(text, "\n")
	// If the byte ceiling cut the read mid-line, the first element is a
	// partial line — drop it so we never return a truncated head line
	// (unless it's the only thing we have).
	if hitCeiling && len(lines) > 1 {
		lines = lines[1:]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, truncated, nil
}

// --- logs.search -------------------------------------------------------

// searchMaxResults caps logs.search so a broad pattern over a large
// history can't return an unbounded match array.
const searchMaxResults = 1000

// searchDefaultResults is the cap when the caller omits `max_results`.
const searchDefaultResults = 200

type logsSearchParams struct {
	BottleID string `json:"bottle_id"`
	Pattern  string `json:"pattern"`
	// Regex selects Go regexp matching; false = literal substring.
	Regex bool `json:"regex"`
	// IgnoreCase folds case (substring) / prepends (?i) (regex).
	IgnoreCase bool `json:"ignore_case"`
	// MaxResults caps returned matches; <=0 → 200, clamped at 1000.
	MaxResults int `json:"max_results"`
	// RunID, when set, restricts the search to that one process's log.
	RunID string `json:"run_id"`
}

type logMatch struct {
	File       string `json:"file"`
	RunID      string `json:"run_id,omitempty"`
	LineNumber int    `json:"line_number"`
	Line       string `json:"line"`
}

type logsSearchResult struct {
	Matches []logMatch `json:"matches"`
	// Truncated is true when MaxResults was hit and more matches exist.
	Truncated     bool `json:"truncated"`
	FilesSearched int  `json:"files_searched"`
}

func (c *Core) handleLogsSearch(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p logsSearchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Pattern) == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	limit := p.MaxResults
	if limit <= 0 {
		limit = searchDefaultResults
	}
	if limit > searchMaxResults {
		limit = searchMaxResults
	}

	// Build the matcher.
	var match func(string) bool
	if p.Regex {
		expr := p.Pattern
		if p.IgnoreCase {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
		match = re.MatchString
	} else if p.IgnoreCase {
		needle := strings.ToLower(p.Pattern)
		match = func(s string) bool { return strings.Contains(strings.ToLower(s), needle) }
	} else {
		needle := p.Pattern
		match = func(s string) bool { return strings.Contains(s, needle) }
	}

	// Resolve the file set: one process's log, or all the bottle's logs.
	logsDir, err := c.logsDir(p.BottleID)
	if err != nil {
		return nil, err
	}
	var files []string
	if p.RunID != "" {
		path, file, missing, err := c.resolveLogFile(p.BottleID, p.RunID)
		if err != nil {
			return nil, err
		}
		if !missing {
			files = []string{file}
			logsDir = filepath.Dir(path)
		}
	} else {
		files, err = c.Store.LogFiles(p.BottleID)
		if err != nil {
			return nil, err
		}
	}

	result := logsSearchResult{Matches: []logMatch{}}
	for _, file := range files {
		path := filepath.Join(logsDir, file)
		runID := ""
		if info, ok := runner.ReadLogInfo(path); ok {
			runID = string(info.RunID)
		}
		done, err := searchFile(path, match, runID, file, limit, &result)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // log rotated/removed mid-scan; skip it
			}
			return nil, err
		}
		result.FilesSearched++
		if done {
			result.Truncated = true
			break
		}
	}
	return result, nil
}

// searchFile streams one log file line-by-line, appending matches to
// result until the limit is reached. Returns done=true when the limit
// was hit (so the caller stops and flags truncation). Uses a bufio
// Reader rather than Scanner so a Wine line longer than 64 KiB doesn't
// error out the scan.
func searchFile(path string, match func(string) bool, runID, file string, limit int, result *logsSearchResult) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	lineNo := 0
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			lineNo++
			trimmed := strings.TrimRight(line, "\n")
			if match(trimmed) {
				if len(result.Matches) >= limit {
					// Buffer is already full AND another match exists →
					// genuinely truncated. (Checking here, not right after
					// the append, is what keeps an exactly-at-limit result
					// from being falsely flagged truncated.)
					return true, nil
				}
				result.Matches = append(result.Matches, logMatch{
					File:       file,
					RunID:      runID,
					LineNumber: lineNo,
					Line:       trimmed,
				})
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
	}
}

// --- bottles.inspect ---------------------------------------------------

type bottlesInspectParams struct {
	ID string `json:"id"`
	// IncludeDisk adds the (expensive) recursive prefix size walk.
	IncludeDisk bool `json:"include_disk"`
}

type logFileInfo struct {
	File      string `json:"file"`
	RunID     string `json:"run_id,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	StartedAt string `json:"started_at,omitempty"`
	Exited    bool   `json:"exited"`
	// ExitCode is meaningful only when Exited is true; for a still-running
	// or not-yet-recorded log it serializes as 0 and should be ignored.
	// (No omitempty, unlike processSummary, so a clean exit_code:0 stays
	// visible — bottles.inspect is meant to be a definitive snapshot.)
	ExitCode int `json:"exit_code"`
}

type bottlesInspectResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at,omitempty"`
	WineVersion string `json:"wine_version,omitempty"`
	// EnvOverrides is the persisted env map; never null (→ {}).
	EnvOverrides map[string]string `json:"env_overrides"`
	// DLLOverrides is env_overrides["WINEDLLOVERRIDES"] parsed into a
	// dll→mode map for convenience; never null (→ {}).
	DLLOverrides map[string]string `json:"dll_overrides"`
	PrefixPath   string            `json:"prefix_path"`
	LogsPath     string            `json:"logs_path"`
	// HasSystemReg / HasUserReg report whether the prefix's registry
	// hives exist — a cheap "is this prefix initialized" signal.
	HasSystemReg bool             `json:"has_system_reg"`
	HasUserReg   bool             `json:"has_user_reg"`
	Processes    []processSummary `json:"running_processes"`
	LogFiles     []logFileInfo    `json:"log_files"`
	// DiskBytes is the recursive logical size of the prefix; null unless
	// include_disk was set. On APFS this OVERSTATES disk use for cloned
	// bottles (clonefile sharing isn't reflected in a size walk).
	DiskBytes *int64 `json:"disk_bytes"`
	// History summarizes this bottle's reversible mutations in the undo/redo
	// journal. null when the journal is unavailable (degraded startup).
	History *bottleHistoryInfo `json:"history"`
}

type bottleHistoryInfo struct {
	UndoDepth int    `json:"undo_depth"`
	RedoDepth int    `json:"redo_depth"`
	// LastOp is the most recent op for this bottle still on the undo stack
	// ("" if none — including when all of the bottle's ops are currently
	// undone and sitting on the redo stack).
	LastOp string `json:"last_op,omitempty"`
}

func (c *Core) handleBottlesInspect(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p bottlesInspectParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}

	// Get enforces the UUIDIsh guard and maps a missing bottle to
	// ErrBottleNotFound for the agent.
	b, err := c.Bottles.Get(p.ID)
	if err != nil {
		return nil, err
	}

	env, err := c.Bottles.EnvOverrides(p.ID)
	if err != nil {
		return nil, err
	}
	if env == nil {
		env = map[string]string{}
	}

	prefixPath, err := c.Store.PrefixDir(p.ID)
	if err != nil {
		return nil, err
	}
	bottleDir, err := c.Store.BottleDir(p.ID)
	if err != nil {
		return nil, err
	}

	out := bottlesInspectResult{
		ID:           b.ID,
		Name:         b.Name,
		WineVersion:  b.WineVersion,
		EnvOverrides: env,
		DLLOverrides: parseDLLOverrides(env["WINEDLLOVERRIDES"]),
		PrefixPath:   prefixPath,
		LogsPath:     filepath.Join(bottleDir, "logs"),
		HasSystemReg: fileExists(filepath.Join(prefixPath, "system.reg")),
		HasUserReg:   fileExists(filepath.Join(prefixPath, "user.reg")),
		Processes:    []processSummary{},
		LogFiles:     []logFileInfo{},
	}
	if !b.CreatedAt.IsZero() {
		// Match the bottles.* surface's created_at shape (toSummary in
		// handlers.go) so the same field reads identically across
		// bottles.get and bottles.inspect. CreatedAt is stored UTC.
		out.CreatedAt = b.CreatedAt.Format("2006-01-02T15:04:05Z")
	}

	for _, proc := range c.Runner.List() {
		if proc.BottleID() == p.ID && !proc.Exited() {
			out.Processes = append(out.Processes, processToSummary(proc))
		}
	}

	files, err := c.Store.LogFiles(p.ID)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		path := filepath.Join(bottleDir, "logs", file)
		lf := logFileInfo{File: file}
		if fi, statErr := os.Stat(path); statErr == nil {
			lf.SizeBytes = fi.Size()
		}
		if info, ok := runner.ReadLogInfo(path); ok {
			lf.RunID = string(info.RunID)
			lf.Exited = info.Exited
			lf.ExitCode = info.ExitCode
			if !info.StartedAt.IsZero() {
				lf.StartedAt = info.StartedAt.UTC().Format("2006-01-02T15:04:05.000Z")
			}
		}
		out.LogFiles = append(out.LogFiles, lf)
	}

	if p.IncludeDisk {
		size, err := dirSize(prefixPath)
		if err != nil {
			return nil, err
		}
		out.DiskBytes = &size
	}

	if c.History != nil {
		ud, rd, last := c.History.BottleSummary(p.ID)
		out.History = &bottleHistoryInfo{UndoDepth: ud, RedoDepth: rd, LastOp: last}
	}

	return out, nil
}

// --- shared helpers ----------------------------------------------------

// resolveLogFile picks the log file for a bottle-keyed read. With a
// runID it returns that one process's log (erroring if the process is
// unknown or belongs to another bottle); otherwise it returns the
// bottle's newest log. missing is true when there's nothing to read.
func (c *Core) resolveLogFile(bottleID, runID string) (path, file string, missing bool, err error) {
	if runID != "" {
		proc, ok := c.Runner.Get(runner.RunID(runID))
		if !ok {
			return "", "", false, fmt.Errorf("%w: %s", runner.ErrProcessNotFound, runID)
		}
		if proc.BottleID() != bottleID {
			return "", "", false, fmt.Errorf("process %s belongs to a different bottle", runID)
		}
		lp := proc.LogPath()
		return lp, filepath.Base(lp), false, nil
	}
	files, err := c.Store.LogFiles(bottleID)
	if err != nil {
		return "", "", false, err
	}
	if len(files) == 0 {
		return "", "", true, nil
	}
	newest := files[len(files)-1] // sorted ascending == chronological
	dir, err := c.logsDir(bottleID)
	if err != nil {
		return "", "", false, err
	}
	return filepath.Join(dir, newest), newest, false, nil
}

// logsDir returns the bottle's logs/ path without creating it (a pure
// read companion to store.LogsDir, which mkdir-ps).
func (c *Core) logsDir(bottleID string) (string, error) {
	dir, err := c.Store.BottleDir(bottleID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs"), nil
}

// parseDLLOverrides turns a WINEDLLOVERRIDES string ("dll=mode;dll2=")
// into a dll→mode map. Entries without an '=' are skipped (malformed or
// the rare bare form); an empty input yields an empty (non-nil) map.
func parseDLLOverrides(s string) map[string]string {
	out := map[string]string{}
	for _, entry := range strings.Split(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eq := strings.IndexByte(entry, '=')
		if eq < 0 {
			continue
		}
		// Wine allows "dll1,dll2=mode"; expand so each dll is its own key.
		mode := entry[eq+1:]
		for _, name := range strings.Split(entry[:eq], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out[name] = mode
			}
		}
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dirSize sums the logical sizes of every regular file under root. A
// missing root is reported as 0 bytes (a freshly-created bottle whose
// prefix walk races a delete), not an error.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}
