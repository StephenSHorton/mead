package meadcore

import (
	"context"
	"encoding/json"

	"github.com/StephenSHorton/mead/internal/apps"
	"github.com/StephenSHorton/mead/internal/runner"
)

// runIDResult is the result shape every spawn-style handler returns —
// just the RunID, so the agent can immediately poll process.logs to
// watch what the spawned process is doing. The full process details
// are available via process.get.
type runIDResult struct {
	RunID runner.RunID `json:"run_id"`
}

type appsInstallParams struct {
	BottleID      string `json:"bottle_id"`
	InstallerPath string `json:"installer_path"`
}

func (c *Core) handleAppsInstall(raw json.RawMessage) (any, error) {
	if err := c.requireApps(); err != nil {
		return nil, err
	}
	var p appsInstallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, err := c.Apps.Install(context.Background(), p.BottleID, p.InstallerPath)
	if err != nil {
		return nil, err
	}
	return runIDResult{RunID: proc.ID()}, nil
}

type appsLaunchParams struct {
	BottleID string `json:"bottle_id"`
	ExePath  string `json:"exe_path"`
}

func (c *Core) handleAppsLaunch(raw json.RawMessage) (any, error) {
	if err := c.requireApps(); err != nil {
		return nil, err
	}
	var p appsLaunchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, err := c.Apps.Launch(context.Background(), p.BottleID, p.ExePath)
	if err != nil {
		return nil, err
	}
	return runIDResult{RunID: proc.ID()}, nil
}

type appsListParams struct {
	BottleID string `json:"bottle_id"`
}

type appsListResult struct {
	Apps []apps.App `json:"apps"`
}

func (c *Core) handleAppsList(raw json.RawMessage) (any, error) {
	if err := c.requireApps(); err != nil {
		return nil, err
	}
	var p appsListParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	as, err := c.Apps.List(p.BottleID)
	if err != nil {
		return nil, err
	}
	if as == nil {
		as = []apps.App{}
	}
	return appsListResult{Apps: as}, nil
}
