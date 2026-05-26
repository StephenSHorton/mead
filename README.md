<h1 align="center">Mead</h1>

<p align="center">
  <i>Agent-driven Wine for macOS.</i>
</p>

<p align="center">
  <img src="docs/hero.png" alt="Mead — a viking-styled mead horn with macOS and Windows desktops layered behind it" width="900">
</p>

Mead is a macOS app that wraps Apple's Game Porting Toolkit and exposes
Wine bottle management to Claude Code over MCP. The GUI and the agent
share the same control surface — every bottle create, app install,
prefix tweak, and process launch flows through the same code, with one
undo stack and one event stream.

**Status:** pre-alpha skeleton. The bridge wire transport and package
architecture are in place; bottle CRUD, app install, and process
supervision are stubs returning `ErrNotImplemented`. See
[CLAUDE.md](./CLAUDE.md) for the planned MCP surface.

## Why

Whisky shipped this shape (Wine GUI on macOS) and the maintainer burned
out keeping up with Wine + macOS churn. CrossOver charges $74/yr. Neither
is agent-driven. Mead's unique angle is letting Claude debug compat
issues in a tight loop — read logs, mutate prefix, retry — instead of
the user Googling DXVK env vars at midnight.

## Building

```bash
git clone https://github.com/StephenSHorton/mead
cd mead
wails build
```

The bundle lands at `build/bin/mead.app`. v0.1 doesn't bundle a Wine
binary yet — the `wine.Locator` stub falls back to `wine64` on PATH, so
install GPTK via `brew install game-porting-toolkit` to exercise the
real path once it lands.

## Talking to the bridge

When Mead is running, an MCP JSON-RPC bridge listens on a 127.0.0.1
port (visible in the app's main view) with a per-pid lockfile at
`~/.mead/mcp/<pid>.lock`. Mead's wire contract is the same as
[wc3-forge](https://github.com/StephenSHorton/wc3-forge)'s, so any MCP
client written against that bridge connects unchanged.

## Architecture

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

Two control surfaces (the GUI and external MCP clients) share one
`meadcore.Core`. Adding a new feature means adding it on both surfaces
at once — see [CLAUDE.md](./CLAUDE.md) for the conventions.

## License

GPL-3.0-or-later. See [LICENSE](./LICENSE).
