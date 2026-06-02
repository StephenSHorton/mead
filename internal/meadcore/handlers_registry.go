package meadcore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/StephenSHorton/mead/internal/history"
)

// registry.get / registry.set — reading and writing a bottle's Windows
// registry. The third leg of the prefix-repair surface alongside env.set
// and dll.override: many Wine compat knobs (CSMT, renderer selection,
// per-app settings) live in the registry rather than the environment, so
// the agent needs to both inspect and change them to close the loop.

type registryGetParams struct {
	BottleID string `json:"bottle_id"`
	// Key is the full Windows registry path, e.g.
	// `HKEY_CURRENT_USER\Software\Wine\Direct3D`.
	Key string `json:"key"`
	// Value, when non-empty, narrows the read to that single named value
	// within Key. Empty reads the whole key (its values + immediate
	// subkeys).
	Value string `json:"value"`
}

func (c *Core) handleRegistryGet(raw json.RawMessage) (any, error) {
	if err := c.requireRegistry(); err != nil {
		return nil, err
	}
	var p registryGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	res, err := c.Registry.Query(context.Background(), p.BottleID, p.Key, p.Value)
	if err != nil {
		return nil, err
	}
	return res, nil
}

type registrySetParams struct {
	BottleID string `json:"bottle_id"`
	Key      string `json:"key"`
	// Value is the value NAME to write. Empty creates the key itself with
	// no value written.
	Value string `json:"value"`
	// Type is the REG_* token (REG_SZ, REG_DWORD, REG_EXPAND_SZ,
	// REG_BINARY, REG_QWORD, REG_MULTI_SZ, REG_NONE). Empty defaults to
	// REG_SZ. Ignored when Value is empty.
	Type string `json:"type"`
	// Data is the value's data. Ignored when Value is empty.
	Data string `json:"data"`
}

func (c *Core) handleRegistrySet(raw json.RawMessage) (any, error) {
	if err := c.requireRegistry(); err != nil {
		return nil, err
	}
	var p registrySetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	// Read the prior state BEFORE writing so undo can restore (or delete)
	// it. This adds a `reg query` ahead of the `reg add`; the cost buys
	// reversibility. When the prior state can't be cleanly captured —
	// a no-op key-create over an existing key, or a transient query
	// failure — recordable is false and we simply DON'T track this set:
	// it stays invisible to undo (same as winetricks/apps), rather than
	// becoming a non-undoable barrier that would wedge the rest of the
	// undo stack.
	before, recordable := c.registrySetBefore(p.BottleID, p.Key, p.Value)
	if err := c.Registry.Set(context.Background(), p.BottleID, p.Key, p.Value, p.Type, p.Data); err != nil {
		return nil, err
	}
	if recordable {
		after := history.State{Present: true}
		if p.Value != "" {
			t := strings.ToUpper(strings.TrimSpace(p.Type))
			if t == "" {
				t = "REG_SZ"
			}
			after.Type = t
			after.Data = p.Data
		}
		c.recordRegistrySet(p.BottleID, p.Key, p.Value, before, after)
	}
	return pingResult{OK: true}, nil
}
