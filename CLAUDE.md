# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```
wails build                               # production build → build/bin/mead.app
wails dev                                 # dev build with Vite HMR
go vet ./...
go test ./...
cd frontend && npm run check              # svelte-check (typecheck)
cd frontend && npm run build              # vite build (also part of wails build)
scripts/build-wine-macos.sh               # build the Mead-owned Wine 11 + GPTK 3.0 (see below)
```

### Mead-owned Wine

`scripts/build-wine-macos.sh` builds a Metal-backed **Wine 11 + Apple GPTK 3.0** from FREE LGPL CrossOver sources via [cxbuilder](https://github.com/101arrowz/cxbuilder), overlaying Apple's GPTK 3.0 redist (D3DMetal) to match the D4Mac donor — so Mead no longer depends on D4Mac's binary. Output lands in `scripts/wine/` (gitignored; ~hundreds of MB). The Metal backend is genuinely in the free source (`dlls/winemac.drv/d3dmetal*.{c,m}`, LGPL). `internal/wine` auto-detects the result. Run `~35 min`, `~10 GB` scratch (point `WORK_DIR` outside the repo). On macOS 26 (Tahoe) the script patches cxbuilder's host-OS→bottle map (it stops at macOS 15). Render of a real CEF app is screen-blind to the agent — the user confirms pixels.

The resolved binary lives at `build/bin/mead.app/Contents/MacOS/mead`. v0.1 is macOS-only (`darwin/arm64` by default; cross to `darwin/amd64` with `wails build -platform darwin/amd64`).

## Scope (v0.1)

Locked 2026-05-26. Hard scope discipline applies — don't drift.

- **Target**: gamers running Windows games on macOS.
- **Wine source**: bundled Apple Game Porting Toolkit (GPTK). v0.1 currently falls back to `wine64` on PATH; the GPTK bundling is a v0.1 milestone not yet shipped.
- **Bottle UX**: first-class — the user explicitly creates bottles, then installs apps INTO a bottle. This shape is what makes the MCP surface clean ("install Foo.exe into bottle-X" instead of "install Foo.exe somewhere").
- **Distribution**: self-built only for v0.1. No code-signing / notarization until v0.2.

Anti-scope (deliberately NOT in v0.1):

- CrossTies-equivalent community compat database
- Multi-Wine-version per bottle
- Steam / cloud sync / self-update
- Anything non-macOS

## Architecture

Single Wails v2 executable. Go owns the system layer (Wine processes, bottle filesystem, MCP bridge); TypeScript + Svelte owns the GUI. Two control surfaces — the GUI and external MCP clients — converge on the same `meadcore.Core` singleton.

```
                ┌──────────── Go process ─────────────┐
   MCP client ──┤ bridge (JSON-RPC / NDJSON) ─────────┤
                │                                     │
   GUI (Wails)──┤ App (bindings) ──┐                  │
                │                  ├─► meadcore.Core ─┤
                │                  │   ├─► Store      │
                │                  │   ├─► Wine       │
                │                  │   ├─► Bottles    │
                │                  │   └─► Runner     │
                └─────────────────────────────────────┘
```

### Go side (`internal/`)

- `internal/meadcore` — the app singleton. `Core` owns the four subsystems below; `RegisterAll(b, c)` in `handlers.go` is the **single registration point** for every MCP method. Adding a new MCP tool means: write `handle<Foo>`, add `reg("foo.bar", handleFoo)` to `RegisterAll`, and add the matching method to the underlying manager.
- `internal/bridge` — wire transport only (JSON-RPC 2.0 over NDJSON on 127.0.0.1, ephemeral port, per-pid lockfile, token auth on `params._token`). Ported from wc3-forge, parameterized via `bridge.Config{AppName}` so it carries no Mead-specific assumptions. Lockfile dir defaults to `$HOME/.mead/mcp/`, overridable via `MEAD_MCP_LOCK_DIR`.
- `internal/bottles` — bottle lifecycle (create, clone, list, get, delete). Composes `store` (metadata persistence), `wine` (binary location), and `runner` (process spawning for `wineboot`, and `cp -Rc` for clonefile-backed clones). Does NOT own wire transport or directly call into Wine itself.
- `internal/wine` — locates the Wine binary, reports its version. Resolution order: `MEAD_WINE_PATH` env, the **Mead-owned Wine** built by `scripts/build-wine-macos.sh` (`<repo>/scripts/wine/bin/wine`, found by walking up from the binary then at `~/projects/mead`), bundled GPTK at `<app>/Contents/Resources/wine/bin/wine64`, the gcenx `game-porting-toolkit` Homebrew **cask** (`/Applications/Game Porting Toolkit.app/...` or `~/Applications`), the Apple `game-porting-toolkit` Homebrew **formula**, PATH lookup.
- `internal/runner` — process supervisor. Spawns Wine processes against a bottle, captures stdout/stderr into per-bottle log files, tracks PIDs for kill/list, emits events for both the GUI and MCP. Every operation that mutates the user environment ultimately goes through Runner — centralizing logging and the agent's `process_logs` / `process_kill` surface.
- `internal/store` — file-backed persistence under `$HOME/Library/Application Support/Mead/`. Bottle metadata, app shortcuts, install history, env overrides.

The top-level `app.go` + `main.go` are the Wails surface: they construct the Core, wire it into the bridge, and expose a tiny TS-visible API (`BridgePort`, `BridgeTokenShort`) so the frontend can show connection info in the Agent Console.

### Frontend (`frontend/src/`)

Svelte + TS + Vite, compiled into `frontend/dist/` and embedded into the Go binary via `//go:embed all:frontend/dist`. v0.1 has a minimal `App.svelte` that just renders the bridge port + token; bottle list / install UI lands as the underlying packages get bodies.

`frontend/wailsjs/` is **generated** by `wails build` — never hand-edit. After changing any Go method bound to the Wails `App` struct, re-run `wails build` (or `wails generate module`) so the TS bindings under `frontend/wailsjs/go/main/App.js` regenerate.

## MCP surface (planned)

Single registration point: `meadcore.RegisterAll`. Naming convention: `<noun>.<verb>` (e.g. `bottles.list`, `apps.install`).

| Surface | Tools (✓ implemented, ◯ stub-returning) |
|---|---|
| Liveness | ✓ `bridge.ping`, ✓ `bridge.version` |
| Wine | ✓ `wine.version` (resolves env / bundled GPTK / gcenx cask / Homebrew formula / PATH) |
| Bottles | ✓ `bottles.list`, ✓ `bottles.create`, ✓ `bottles.clone`, ✓ `bottles.get`, ✓ `bottles.delete` |
| Apps | ✓ `apps.install`, ✓ `apps.launch`, ✓ `apps.uninstall`, ◯ `apps.list` (returns [] — auto-discovery is v0.3) |
| Processes | ✓ `process.list`, ✓ `process.get`, ✓ `process.kill`, ✓ `process.logs` (offset-based polling) |
| Prefix tweaks | ✓ `env.set`, ✓ `env.get`, ✓ `dll.override`, ✓ `winetricks.run`, ✓ `registry.get`, ✓ `registry.set` |
| Diagnostics | ✓ `logs.tail`, ✓ `logs.search` (bottle-keyed, across run history; distinct from `process.logs`' single-RunID byte polling), ✓ `bottle.inspect` (one-shot snapshot: metadata, env/dll overrides, log inventory, live processes, optional `include_disk` size walk) |

Planned (land as the underlying packages get bodies):

| Surface | Tools |
|---|---|
| History | `history.undo`, `history.redo`, `history.list` — undo covers `env.set` / `dll.override` / `registry.set` / `bottles.create` / `bottles.clone`; `winetricks.run` / `apps.install` / `apps.uninstall` / `bottles.delete` are recorded but not reversible (no clean inverse short of a full prefix snapshot) |

The agent debug loop (the marquee feature) is complete: Claude reads `process.logs`, identifies the issue, calls `env.set` / `dll.override` / `winetricks.run` to fix, retries via `apps.launch`. Both halves — diagnose AND repair — are wired and verified end-to-end via real JSON-RPC.

## Cross-cutting events

Wails event names are constants in `app.go` (currently empty — wire as they're needed). When adding a new event, declare it as a string constant alongside the existing ones; the frontend imports the matching string.

## Sibling project

Mead's wire contract is identical to [wc3-forge](https://github.com/StephenSHorton/wc3-forge)'s. wc3-forge can dogfood Mead — install WC3 into a Mead bottle, point `WC3FORGE_WC3_PATH` at the prefix's `drive_c/Program Files (x86)/Warcraft III`, have Claude debug compat in a loop.
