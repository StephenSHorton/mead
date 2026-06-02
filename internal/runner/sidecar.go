package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sidecar is the on-disk JSON representation of a Process. Written
// next to each spawned process's log file at <logPath>.json so the
// runner can rediscover and re-register processes across Mead
// restarts. The shape is intentionally flat + stable; extensions go
// at the end so older Mead versions parse newer files without
// failing on unknown fields.
//
// Files are atomically replaced (write-temp + rename) so a partially-
// written sidecar can never confuse the discovery scan.
type sidecar struct {
	ID        RunID    `json:"id"`
	BottleID  string   `json:"bottle_id,omitempty"`
	Argv      []string `json:"argv"`
	StartedAt string   `json:"started_at"`
	LogPath   string   `json:"log_path"`
	PID       int      `json:"pid"`

	// Mutable after spawn — updated by the reaper or by adoption when
	// the runner notices a process exited while Mead was offline.
	Exited   bool   `json:"exited"`
	ExitedAt string `json:"exited_at,omitempty"`
	ExitCode int    `json:"exit_code"`
	RunErr   string `json:"run_err,omitempty"`
}

// sidecarPathFor returns the canonical sidecar path for a given log
// file. Convention: append ".json" — this works regardless of the log
// filename scheme (timestamp-based today; could shift to UUID-based
// later without changing this code).
func sidecarPathFor(logPath string) string {
	return logPath + ".json"
}

// writeSidecar atomically writes the sidecar JSON. Atomic so a crash
// mid-write can't leave a half-parsed file the discovery scan would
// reject. The parent directory is assumed to exist (caller is
// responsible — typically the log file was already created there).
func writeSidecar(metaPath string, s *sidecar) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sidecar: %w", err)
	}
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp sidecar: %w", err)
	}
	if err := os.Rename(tmp, metaPath); err != nil {
		return fmt.Errorf("rename sidecar into place: %w", err)
	}
	return nil
}

// LogInfo is the subset of a process's sidecar metadata the diagnostics
// surface exposes — the run-id mapping for logs.search and the per-log
// inventory for bottles.inspect. Times are parsed; an unset time is the
// zero value.
type LogInfo struct {
	RunID     RunID
	BottleID  string
	StartedAt time.Time
	Exited    bool
	ExitedAt  time.Time
	ExitCode  int
}

// ReadLogInfo loads the sidecar written next to logPath (<logPath>.json)
// and returns its metadata. ok is false when the sidecar is absent or
// unparseable — callers treat that as "unknown" rather than an error,
// since a log file can exist without (or just before) its sidecar.
func ReadLogInfo(logPath string) (LogInfo, bool) {
	s, err := readSidecar(sidecarPathFor(logPath))
	if err != nil {
		return LogInfo{}, false
	}
	return LogInfo{
		RunID:     s.ID,
		BottleID:  s.BottleID,
		StartedAt: parseTime(s.StartedAt),
		Exited:    s.Exited,
		ExitedAt:  parseTime(s.ExitedAt),
		ExitCode:  s.ExitCode,
	}, true
}

// readSidecar loads a sidecar by path.
func readSidecar(metaPath string) (*sidecar, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var s sidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode sidecar %s: %w", filepath.Base(metaPath), err)
	}
	return &s, nil
}

// fmtTime formats a time.Time the way the sidecar writes it.
// Centralized so adoption + the reaper agree.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// parseTime parses what fmtTime wrote. Empty string returns zero time.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return time.Time{}
	}
	return t
}
