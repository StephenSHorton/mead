#!/usr/bin/env bash
# Mead expects Apple's Game Porting Toolkit (a Wine fork with D3DMetal)
# to be installed via Homebrew. This script verifies the setup and
# prints the path wine.Locator will pick up. Run it after a fresh
# clone (or after upgrading macOS) to confirm Mead can spawn Wine.
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
  echo
  echo "  Install it from https://brew.sh and rerun this script."
  exit 1
fi
green "✓ Homebrew: $(brew --version | head -1)"

# 2. game-porting-toolkit formula.
if ! brew --prefix game-porting-toolkit >/dev/null 2>&1; then
  red "✗ game-porting-toolkit formula is not installed."
  echo
  echo "  Install it with:"
  echo "    brew tap apple/apple"
  echo "    brew install apple/apple/game-porting-toolkit"
  echo
  echo "  (Apple maintains GPTK in their own tap. The install is large"
  echo "  — expect 1-2 GB plus a long build.)"
  exit 1
fi
GPTK_PREFIX="$(brew --prefix game-porting-toolkit)"
green "✓ game-porting-toolkit: $GPTK_PREFIX"

# 3. The wine64 binary inside it.
WINE_BIN="$GPTK_PREFIX/bin/wine64"
if [[ ! -x "$WINE_BIN" ]]; then
  red "✗ wine64 not found at $WINE_BIN"
  echo
  echo "  The formula is installed but its bin/wine64 is missing."
  echo "  Try: brew reinstall apple/apple/game-porting-toolkit"
  exit 1
fi
green "✓ wine64 binary: $WINE_BIN"
echo "  $($WINE_BIN --version 2>/dev/null || echo '(--version failed)')"

# 4. winetricks (optional but recommended).
if brew --prefix winetricks >/dev/null 2>&1; then
  WT_BIN="$(brew --prefix winetricks)/bin/winetricks"
  if [[ -x "$WT_BIN" ]]; then
    green "✓ winetricks: $WT_BIN"
  else
    yellow "! winetricks formula present but binary missing at $WT_BIN"
  fi
else
  yellow "! winetricks is not installed (optional)."
  echo "  Install it with: brew install winetricks"
  echo "  Mead's winetricks.run MCP method will report ErrNotFound until you do."
fi

echo
bold "Result: Mead can locate Wine."
echo
echo "wine.Locator will pick up the brew install via its Homebrew fallback."
echo "Override with MEAD_WINE_PATH if you want to point at a different binary."
