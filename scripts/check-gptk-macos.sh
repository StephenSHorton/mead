#!/usr/bin/env bash
# Mead expects Apple's Game Porting Toolkit (a Wine fork with D3DMetal)
# to be installed via Homebrew. This script verifies the setup and
# prints the path wine.Locator will pick up. Run it after a fresh
# clone (or after upgrading macOS) to confirm Mead can spawn Wine.
#
# Two install paths exist, both supported:
#
#  - apple/apple/game-porting-toolkit (formula): Apple's official tap.
#    Builds Wine from source. As of late 2026 this formula depends on
#    openssl@1.1 which has been retired from Homebrew core — installs
#    may fail until Apple updates the formula. Check Apple's tap for
#    the current state.
#
#  - gcenx/wine/game-porting-toolkit (cask): community-maintained,
#    ships a prebuilt Intel-arch binary (Rosetta 2 required on M-series
#    Macs). More reliable in practice; what we recommend today.
#
# Output is intentionally chatty — it's meant for human troubleshooting
# rather than scripting. Exit code 0 = ready to go; non-zero = setup
# work needed (the message says what).
set -euo pipefail

red()    { printf "\033[31m%s\033[0m\n" "$*"; }
green()  { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }
bold()   { printf "\033[1m%s\033[0m\n" "$*"; }

bold "Mead GPTK setup check"
echo

# 1. Homebrew itself.
if ! command -v brew >/dev/null 2>&1; then
  red "✗ Homebrew is not installed."
  echo "  Install it from https://brew.sh and rerun this script."
  exit 1
fi
green "✓ Homebrew: $(brew --version | head -1)"

# 2. Locate wine64. Three places to check, in priority order matching
# wine.Locator's resolution chain:
#  - the Apple tap's brew prefix
#  - $(brew --prefix)/bin (where the gcenx cask symlinks land)
#  - PATH
WINE_BIN=""
SOURCE=""
if PREFIX="$(brew --prefix game-porting-toolkit 2>/dev/null)" && [[ -x "$PREFIX/bin/wine64" ]]; then
  WINE_BIN="$PREFIX/bin/wine64"
  SOURCE="apple/apple formula"
elif [[ -x "$(brew --prefix)/bin/wine64" ]]; then
  WINE_BIN="$(brew --prefix)/bin/wine64"
  SOURCE="gcenx/wine cask (symlinked at $(brew --prefix)/bin)"
elif WHICH_WINE="$(command -v wine64 2>/dev/null)"; then
  WINE_BIN="$WHICH_WINE"
  SOURCE="PATH"
fi

if [[ -z "$WINE_BIN" ]]; then
  red "✗ wine64 not found in any of the expected locations."
  echo
  echo "  Easiest install (gcenx cask, no compile, requires Rosetta 2):"
  echo "    brew tap gcenx/wine"
  echo "    brew install --cask gcenx/wine/game-porting-toolkit"
  echo
  echo "  Or via Apple's tap (compiles from source, slower):"
  echo "    brew tap apple/apple"
  echo "    brew install apple/apple/game-porting-toolkit"
  exit 1
fi

green "✓ wine64 binary: $WINE_BIN"
echo "  source: $SOURCE"
echo "  $($WINE_BIN --version 2>/dev/null || echo '(--version failed)')"

# 3. winetricks (optional but recommended).
if command -v winetricks >/dev/null 2>&1; then
  green "✓ winetricks: $(command -v winetricks)"
else
  yellow "! winetricks is not installed (optional)."
  echo "  Install it with: brew install winetricks"
  echo "  Mead's winetricks.run MCP method will report ErrNotFound until you do."
fi

echo
bold "Result: Mead can locate Wine."
echo
echo "wine.Locator will pick this up via its PATH fallback. Override with"
echo "MEAD_WINE_PATH if you want to point at a different binary."
