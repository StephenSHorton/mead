package meadcore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/StephenSHorton/mead/internal/bottles"
	"github.com/StephenSHorton/mead/internal/bridge"
)

// RegisterAll wires every MCP method Mead exposes onto the given bridge.
// This is the single registration point — adding a new agent-driven
// operation means a new `reg("name", handler)` line here, plus the
// handler body below. The bridge itself stays domain-free.
//
// Naming convention: dot-separated <noun>.<verb>, e.g. `bottles.list`,
// `apps.install`, `process.kill`. Matches the wc3-forge bridge style.
func RegisterAll(b *bridge.Bridge, c *Core) {
	reg := func(method string, h bridge.Handler) { b.Register(method, h) }

	// --- Liveness / introspection -----------------------------------------
	reg("bridge.ping", c.handlePing)
	reg("bridge.version", c.handleVersion)

	// --- Wine -------------------------------------------------------------
	reg("wine.version", c.handleWineVersion)

	// --- Bottles ----------------------------------------------------------
	reg("bottles.list", c.handleBottlesList)
	reg("bottles.create", c.handleBottlesCreate)
	reg("bottles.clone", c.handleBottlesClone)
	reg("bottles.get", c.handleBottlesGet)
	reg("bottles.delete", c.handleBottlesDelete)

	// --- Apps -------------------------------------------------------------
	reg("apps.install", c.handleAppsInstall)
	reg("apps.launch", c.handleAppsLaunch)
	reg("apps.uninstall", c.handleAppsUninstall)
	reg("apps.list", c.handleAppsList)

	// --- Processes (the diagnostic surface) ------------------------------
	reg("process.list", c.handleProcessList)
	reg("process.get", c.handleProcessGet)
	reg("process.kill", c.handleProcessKill)
	reg("process.logs", c.handleProcessLogs)

	// --- Prefix tweaks (the repair surface) ------------------------------
	reg("env.set", c.handleEnvSet)
	reg("env.get", c.handleEnvGet)
	reg("dll.override", c.handleDLLOverride)
	reg("winetricks.run", c.handleWinetricksRun)

	// --- Registry (the repair surface, cont.) ----------------------------
	reg("registry.get", c.handleRegistryGet)
	reg("registry.set", c.handleRegistrySet)

	// --- Diagnostics (read-only visibility) ------------------------------
	reg("logs.tail", c.handleLogsTail)
	reg("logs.search", c.handleLogsSearch)
	reg("bottle.inspect", c.handleBottleInspect)
}

// pingResult is the response shape for bridge.ping — kept stable across
// versions so MCP clients can rely on it for connection probing.
type pingResult struct {
	OK bool `json:"ok"`
}

func (c *Core) handlePing(_ json.RawMessage) (any, error) {
	return pingResult{OK: true}, nil
}

type versionResult struct {
	Bridge string `json:"bridge"`
	App    string `json:"app"`
}

func (c *Core) handleVersion(_ json.RawMessage) (any, error) {
	return versionResult{Bridge: bridge.Version, App: AppVersion}, nil
}

type wineVersionResult struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func (c *Core) handleWineVersion(_ json.RawMessage) (any, error) {
	path, err := c.Wine.Path()
	if err != nil {
		return wineVersionResult{}, err
	}
	ver, err := c.Wine.Version()
	if err != nil {
		return wineVersionResult{Path: path}, err
	}
	return wineVersionResult{Path: path, Version: ver}, nil
}

type bottlesListResult struct {
	Bottles []bottleSummary `json:"bottles"`
}

type bottleSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at,omitempty"`
	WineVersion string `json:"wine_version,omitempty"`
}

func toSummary(b *bottles.Bottle) bottleSummary {
	return bottleSummary{
		ID:          b.ID,
		Name:        b.Name,
		CreatedAt:   b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		WineVersion: b.WineVersion,
	}
}

func (c *Core) handleBottlesList(_ json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	bs, err := c.Bottles.List()
	if err != nil {
		return nil, err
	}
	out := make([]bottleSummary, 0, len(bs))
	for _, b := range bs {
		out = append(out, toSummary(b))
	}
	return bottlesListResult{Bottles: out}, nil
}

type bottlesCreateParams struct {
	Name string `json:"name"`
}

func (c *Core) handleBottlesCreate(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p bottlesCreateParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
	}
	// wineboot --init runs synchronously inside the handler; with no
	// streaming surface yet, an MCP client effectively blocks until the
	// prefix is materialized (typically 5-15s on a warm install).
	// Background context: cancellation is currently per-bridge-shutdown
	// only, which is the right granularity for v0.1.
	b, err := c.Bottles.Create(context.Background(), p.Name)
	if err != nil {
		return nil, err
	}
	return toSummary(b), nil
}

type bottlesCloneParams struct {
	// SourceID is the bottle to clone FROM. Spelled source_id (rather
	// than the bare `id` the other Bottles handlers use) because a clone
	// involves two bottles and `id` would be ambiguous about which.
	SourceID string `json:"source_id"`
	// Name is the new bottle's display name; must be non-empty + unique,
	// same rules as bottles.create.
	Name string `json:"name"`
}

func (c *Core) handleBottlesClone(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p bottlesCloneParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	// Like bottles.create, the prefix copy runs synchronously inside the
	// handler. On an APFS volume the clonefile copy is near-instant; a
	// cross-volume fallback copy can take longer, blocking the caller.
	b, err := c.Bottles.Clone(context.Background(), p.SourceID, p.Name)
	if err != nil {
		return nil, err
	}
	return toSummary(b), nil
}

type bottlesGetParams struct {
	ID string `json:"id"`
}

func (c *Core) handleBottlesGet(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p bottlesGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	b, err := c.Bottles.Get(p.ID)
	if err != nil {
		if errors.Is(err, bottles.ErrBottleNotFound) {
			return nil, err
		}
		return nil, err
	}
	return toSummary(b), nil
}

type bottlesDeleteParams struct {
	ID string `json:"id"`
}

func (c *Core) handleBottlesDelete(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p bottlesDeleteParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if err := c.Bottles.Delete(p.ID); err != nil {
		return nil, err
	}
	return pingResult{OK: true}, nil
}

// AppVersion is bumped per release. Wired into bridge.version so agents
// can detect breaking changes without parsing the user-agent.
const AppVersion = "0.0.1"
