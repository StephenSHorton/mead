package meadcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/StephenSHorton/mead/internal/history"
)

// env.set / env.get / dll.override — the prefix-tweak surface.
//
// These are the operations the agent uses to actually FIX compat
// issues after reading process.logs. Read-only diagnostics would be
// neat; without write access to the prefix's environment, the agent
// loop dead-ends at "I see the problem but can't do anything about
// it." env.set + dll.override + winetricks.run between them cover
// ~80% of "the game won't start" fixes in practice.

type envSetParams struct {
	BottleID string `json:"bottle_id"`
	Key      string `json:"key"`
	// Value is what the env var should be set to. Empty value removes
	// the key from the bottle's overrides. (To set a literal empty
	// string, use the explicit env.unset form below, if added later.)
	Value string `json:"value"`
}

func (c *Core) handleEnvSet(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p envSetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	// Capture the prior value so undo can restore it (or re-delete when the
	// key was absent). Best-effort: a read failure just yields an empty
	// before, and the SetEnv below surfaces any real error (e.g. unknown
	// bottle) before we record anything.
	before := history.State{}
	if prior, err := c.Bottles.EnvOverrides(p.BottleID); err == nil {
		if v, ok := prior[p.Key]; ok {
			before = history.State{Present: true, Value: v}
		}
	}
	if err := c.Bottles.SetEnv(p.BottleID, p.Key, p.Value); err != nil {
		return nil, err
	}
	after := history.State{}
	if p.Value != "" { // empty value deletes the key — that's an absent after-state
		after = history.State{Present: true, Value: p.Value}
	}
	c.recordEnvSet(p.BottleID, p.Key, before, after)
	return pingResult{OK: true}, nil
}

type envGetParams struct {
	BottleID string `json:"bottle_id"`
}

type envGetResult struct {
	Env map[string]string `json:"env"`
}

func (c *Core) handleEnvGet(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p envGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	env, err := c.Bottles.EnvOverrides(p.BottleID)
	if err != nil {
		return nil, err
	}
	if env == nil {
		env = map[string]string{}
	}
	return envGetResult{Env: env}, nil
}

// dll.override is a convenience wrapper over env.set that maintains
// the WINEDLLOVERRIDES env var. Wine's format is semicolon-separated:
//
//	dll1=native,builtin;dll2=builtin;dll3=
//
// (empty after = disables the DLL entirely). Maintaining this string
// by hand is error-prone — agents have gotten the syntax wrong in
// every Wine wrapper I've seen — so we parse + recompose it here.

type dllOverrideParams struct {
	BottleID string `json:"bottle_id"`
	DLL      string `json:"dll"`
	// Mode is one of: "native", "builtin", "native,builtin",
	// "builtin,native", "disabled" (= the dll= empty form), or ""
	// (remove this DLL's override entirely).
	Mode string `json:"mode"`
}

func (c *Core) handleDLLOverride(raw json.RawMessage) (any, error) {
	if err := c.requireBottles(); err != nil {
		return nil, err
	}
	var p dllOverrideParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.DLL) == "" {
		return nil, fmt.Errorf("dll name is required")
	}

	current, err := c.Bottles.EnvOverrides(p.BottleID)
	if err != nil {
		return nil, err
	}
	// undo/redo for dll.override operate on the WHOLE WINEDLLOVERRIDES
	// string (a per-DLL delta can't be cleanly inverted — it loses whether
	// the DLL was previously present and in what mode).
	priorVal, priorPresent := current["WINEDLLOVERRIDES"]
	updated := setDLLOverride(priorVal, p.DLL, p.Mode)
	if err := c.Bottles.SetEnv(p.BottleID, "WINEDLLOVERRIDES", updated); err != nil {
		return nil, err
	}
	before := history.State{Present: priorPresent, Value: priorVal}
	after := history.State{}
	if updated != "" { // an empty result removes WINEDLLOVERRIDES entirely
		after = history.State{Present: true, Value: updated}
	}
	c.recordDLLOverride(p.BottleID, p.DLL, before, after)
	return pingResult{OK: true}, nil
}

// SetDLLOverrideExternal is the exported alias of setDLLOverride for
// the Wails binding layer (app.go). The MCP handler keeps the
// unexported name to avoid leaking the entire meadcore surface.
func SetDLLOverrideExternal(current, dll, mode string) string {
	return setDLLOverride(current, dll, mode)
}

// setDLLOverride parses a WINEDLLOVERRIDES string, sets (or removes)
// the entry for the given dll, and returns the recomposed string. The
// canonical form preserves entry order; new entries are appended.
//
// Exported via the package-internal symbol for testability — see
// handlers_env_test.go.
func setDLLOverride(current, dll, mode string) string {
	entries := []dllOverrideEntry{}
	seen := false
	for _, entry := range strings.Split(current, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		eq := strings.IndexByte(entry, '=')
		if eq == -1 {
			// Malformed — preserve verbatim so we don't lose data we
			// don't understand.
			entries = append(entries, dllOverrideEntry{raw: entry})
			continue
		}
		name := strings.TrimSpace(entry[:eq])
		value := entry[eq+1:]
		if strings.EqualFold(name, dll) {
			seen = true
			if mode == "" {
				continue // remove
			}
			entries = append(entries, dllOverrideEntry{name: dll, value: dllModeToValue(mode)})
			continue
		}
		entries = append(entries, dllOverrideEntry{name: name, value: value})
	}
	if !seen && mode != "" {
		entries = append(entries, dllOverrideEntry{name: dll, value: dllModeToValue(mode)})
	}

	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.raw != "" {
			parts = append(parts, e.raw)
			continue
		}
		parts = append(parts, e.name+"="+e.value)
	}
	return strings.Join(parts, ";")
}

type dllOverrideEntry struct {
	name  string
	value string
	raw   string // set when we couldn't parse, preserved verbatim
}

// dllModeToValue translates user-friendly mode strings to Wine's wire
// values. "disabled" → "" (the dll= empty form, which tells Wine to
// disable the DLL entirely). Others pass through unchanged after
// lowercasing because Wine matches case-insensitively.
func dllModeToValue(mode string) string {
	if strings.EqualFold(mode, "disabled") {
		return ""
	}
	return strings.ToLower(mode)
}

// --- winetricks.run -----------------------------------------------------

type winetricksRunParams struct {
	BottleID string `json:"bottle_id"`
	// Verb is the winetricks "verb" — d3dx9, dotnet48, vcrun2019, etc.
	// See `winetricks --help` for the canonical list. Mead doesn't
	// validate; we pass through verbatim so new verbs work without
	// requiring a Mead update.
	Verb string `json:"verb"`
}

func (c *Core) handleWinetricksRun(raw json.RawMessage) (any, error) {
	if err := c.requireApps(); err != nil {
		return nil, err
	}
	var p winetricksRunParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	proc, err := c.Apps.RunWinetricks(context.Background(), p.BottleID, p.Verb)
	if err != nil {
		return nil, err
	}
	return runIDResult{RunID: proc.ID()}, nil
}
