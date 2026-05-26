package meadcore

import (
	"encoding/json"

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

	// More methods land as their domain packages get bodies. See CLAUDE.md
	// for the full planned surface.
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
	// Both calls return ErrNotImplemented until wine.Locator is wired up.
	// The handler still returns a well-formed result for the not-yet-
	// resolved case so agent clients can distinguish "Wine not located
	// yet" from a transport error.
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
	WineVersion string `json:"wine_version,omitempty"`
}

func (c *Core) handleBottlesList(_ json.RawMessage) (any, error) {
	bs, err := c.Bottles.List()
	if err != nil {
		return nil, err
	}
	out := make([]bottleSummary, 0, len(bs))
	for _, b := range bs {
		out = append(out, bottleSummary{ID: b.ID, Name: b.Name, WineVersion: b.WineVersion})
	}
	return bottlesListResult{Bottles: out}, nil
}

type bottlesCreateParams struct {
	Name string `json:"name"`
}

func (c *Core) handleBottlesCreate(raw json.RawMessage) (any, error) {
	var p bottlesCreateParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
	}
	b, err := c.Bottles.Create(p.Name)
	if err != nil {
		return nil, err
	}
	return bottleSummary{ID: b.ID, Name: b.Name, WineVersion: b.WineVersion}, nil
}

// AppVersion is bumped per release. Wired into bridge.version so agents
// can detect breaking changes without parsing the user-agent.
const AppVersion = "0.0.1"
