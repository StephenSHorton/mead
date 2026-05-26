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
./scripts/check-gptk-macos.sh    # one-time setup verification
wails build
```

The bundle lands at `build/bin/mead.app`.

### Wine setup (one-time)

Mead spawns Apple's Game Porting Toolkit (GPTK) — a Wine fork with
D3DMetal — for everything it does inside a bottle. Install via Apple's
Homebrew tap:

```bash
brew tap apple/apple http://github.com/apple/homebrew-apple
brew install apple/apple/game-porting-toolkit
brew install winetricks      # optional but recommended
```

Then run `./scripts/check-gptk-macos.sh` to verify Mead can find them.
`wine.Locator` resolves binaries in this order: `MEAD_WINE_PATH` env →
bundled GPTK at `<app>/Contents/Resources/wine/bin/wine64` → Homebrew
GPTK → PATH lookup. v0.2 doesn't bundle GPTK into the .app — users
install it themselves via brew. Bundling is a v0.3 distribution concern.

## Talking to the bridge

When Mead is running, an MCP JSON-RPC bridge listens on a 127.0.0.1
port (visible in the app's main view) with a per-pid lockfile at
`~/.mead/mcp/<pid>.lock`. Mead's wire contract is the same as
[wc3-forge](https://github.com/StephenSHorton/wc3-forge)'s, so any MCP
client written against that bridge connects unchanged.

## Using Mead with wc3-forge

[wc3-forge](https://github.com/StephenSHorton/wc3-forge) is Mead's
sibling project — a Warcraft III map editor that reads CASC assets
from your WC3 install. If you install WC3 into a Mead bottle, point
wc3-forge at it:

```bash
# 1. Create a bottle in Mead (via the GUI, or via MCP from Claude Code).
# 2. Install WC3 into the bottle (Battle.net installer / native macOS
#    Battle.net client / your method of choice).
# 3. Point wc3-forge at the bottle's WC3 install:

export WC3FORGE_WC3_PATH=~/Library/Application\ Support/Mead/bottles/<bottle-id>/prefix/drive_c/Program\ Files\ \(x86\)/Warcraft\ III
~/projects/wc3-forge/build/bin/wc3-forge.app/Contents/MacOS/wc3-forge
```

The bottle ID is visible in Mead's UI or via `bottles.list` over MCP.
The path layout is exercised by `TestIntegration_WC3PrefixPath_MatchesWC3ForgeExpectation`
in `internal/apps/`, which is the load-bearing contract between the
two projects — if either side moves that path, the test fails.

When apps run into compat issues (missing DLL, registry tweak needed,
etc.), Claude Code can drive the diagnostic loop via Mead's MCP:
`process.logs <run_id>` to read what wine printed, decide what's
wrong, call `winetricks.run` or set an env override, retry. That
agent debug loop is the whole point.

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
