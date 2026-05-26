package meadcore

import (
	"encoding/json"
	"fmt"

	"github.com/StephenSHorton/mead/internal/runner"
)

// processSummary is the JSON shape returned by process.list and
// process.get. Stable contract — extend by adding fields, not by
// changing existing types.
type processSummary struct {
	RunID     runner.RunID `json:"run_id"`
	BottleID  string       `json:"bottle_id,omitempty"`
	Argv      []string     `json:"argv"`
	StartedAt string       `json:"started_at"`
	LogPath   string       `json:"log_path,omitempty"`
	Exited    bool         `json:"exited"`
	ExitedAt  string       `json:"exited_at,omitempty"`
	ExitCode  int          `json:"exit_code,omitempty"`
	RunErr    string       `json:"run_err,omitempty"`
}

func processToSummary(p *runner.Process) processSummary {
	s := processSummary{
		RunID:     p.ID(),
		BottleID:  p.BottleID(),
		Argv:      p.Argv(),
		StartedAt: p.StartedAt().UTC().Format("2006-01-02T15:04:05.000Z"),
		LogPath:   p.LogPath(),
		Exited:    p.Exited(),
		ExitCode:  p.ExitCode(),
		RunErr:    p.RunErr(),
	}
	if t := p.ExitedAt(); !t.IsZero() {
		s.ExitedAt = t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return s
}

type processListParams struct {
	// BottleID is optional. When set, only processes tagged with this
	// bottle are returned — useful for "show me what's running in
	// Bottle X." Empty = all known processes.
	BottleID string `json:"bottle_id"`
}

type processListResult struct {
	Processes []processSummary `json:"processes"`
}

func (c *Core) handleProcessList(raw json.RawMessage) (any, error) {
	var p processListParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
	}
	ps := c.Runner.List()
	out := make([]processSummary, 0, len(ps))
	for _, proc := range ps {
		if p.BottleID != "" && proc.BottleID() != p.BottleID {
			continue
		}
		out = append(out, processToSummary(proc))
	}
	return processListResult{Processes: out}, nil
}

type processGetParams struct {
	RunID runner.RunID `json:"run_id"`
}

func (c *Core) handleProcessGet(raw json.RawMessage) (any, error) {
	var p processGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, ok := c.Runner.Get(p.RunID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", runner.ErrProcessNotFound, p.RunID)
	}
	return processToSummary(proc), nil
}

type processKillParams struct {
	RunID runner.RunID `json:"run_id"`
}

func (c *Core) handleProcessKill(raw json.RawMessage) (any, error) {
	var p processKillParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, ok := c.Runner.Get(p.RunID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", runner.ErrProcessNotFound, p.RunID)
	}
	if err := proc.Kill(); err != nil {
		return nil, err
	}
	return pingResult{OK: true}, nil
}

type processLogsParams struct {
	RunID runner.RunID `json:"run_id"`
	// Offset is the byte offset to start reading from. Zero = start of
	// log file. Polling callers pass the previous response's
	// NextOffset back here.
	Offset int64 `json:"offset"`
	// Limit caps the bytes returned. Zero = "read everything from the
	// offset to EOF". For interactive polling, 64 KiB is reasonable.
	Limit int `json:"limit"`
}

type processLogsResult struct {
	Bytes      string `json:"bytes"`
	NextOffset int64  `json:"next_offset"`
	Exited     bool   `json:"exited"`
}

// handleProcessLogs is THE diagnostic surface — when an install or
// launch fails, the agent calls this in a loop to read the log,
// reason about the error, and decide what to do next (winetricks,
// retry, ask the user). Returns bytes as a UTF-8 string for JSON
// safety; binary log content is rare in Wine output but if encountered
// gets re-encoded by the JSON layer.
func (c *Core) handleProcessLogs(raw json.RawMessage) (any, error) {
	var p processLogsParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, ok := c.Runner.Get(p.RunID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", runner.ErrProcessNotFound, p.RunID)
	}
	data, next, exited, err := proc.Logs(p.Offset, p.Limit)
	if err != nil {
		return nil, err
	}
	return processLogsResult{
		Bytes:      string(data),
		NextOffset: next,
		Exited:     exited,
	}, nil
}
