#!/usr/bin/env bash
#
# build-wine-macos.sh — build a Mead-owned, Metal-backed Wine 11 from FREE
# sources and overlay Apple's GPTK 3.0 + DXMT to match the D4Mac donor.
#
# WHAT THIS PRODUCES
#   ~/projects/mead/scripts/wine/{bin,lib} — a relocatable, D3DMetal-capable
#   Wine 11.0 (CrossOver 26.1 lineage, the exact base D4Mac ships) that
#   replaces Mead's dependency on /Applications/D4Mac.app .../Wine.
#
# WHY THIS RENDERS (the render crux — RESOLVED)
#   The Metal graphics backend lives in dlls/winemac.drv (d3dmetal.c +
#   d3dmetal_objc.m, LGPL v2.1, CodeWeavers/Brendan Shanks). It is SHIPPED
#   IN the FREE LGPL CrossOver source tarball (crossover-sources-26.1.0),
#   not withheld as proprietary and not absent from upstream-but-stale
#   trees. Building winemac.drv from this source yields a winemac.so that
#   DEFINES macdrv_create_metal_device / macdrv_{get,set}_view_d3dmetal_
#   client_surface / WineMetalLayer:CAMetalLayer — the exact ABI Apple's
#   D3DMetal.framework binds to. Verified by symbol forensics: the D4Mac
#   donor's winemac.so defines those symbols; the abandoned "wine-cx 10.0"
#   build rendered nothing because its tree predated d3dmetal.c. We pin to
#   CX 26.1.0 (= D4Mac wine-11.0) so the built winemac.so matches.
#
# DESIGN: clean build + faithful overlay (NOT cxbuilder's --gptk/--dxvk)
#   We invoke cxbuilder with --no-gptk --no-dxvk to get a pristine
#   Metal-backed wine, then do our OWN overlay into lib/external. Reasons:
#     1. cxbuilder's --gptk hard-requires redist/.../d3d9.dll, which GPTK
#        3.0 DROPPED — its overlay would abort at validation. Skipping it
#        sidesteps the need to patch cxbuilder.sh.
#     2. cxbuilder puts D3DMetal under lib/gptk/x86_64-unix; Mead's
#        wine.Preamble() detects lib/external/D3DMetal.framework (the D4Mac
#        layout). Our overlay lands it exactly where Preamble looks.
#     3. We bundle DXMT + MoltenVK into lib/external to mirror the D4Mac
#        donor byte-for-byte. NOTE (verified against the working donor
#        prefix): the 32-bit PE32 Battle.net CEF launcher actually renders
#        via the CrossOver BUILTIN i386 d3d11 + the winemac Metal present
#        hook — NOT via DXMT. So the lib/external DXMT is donor-parity, not the
#        launcher's render path. (The in-GAME render path IS DXMT — see below.)
#     4. DXMT_MENU_FIX (default on): build a PATCHED DXMT v0.80 from source and
#        install it into the wine BUILTIN tree (lib/wine/x86_64-windows +
#        x86_64-unix/winemetal.so) so 64-bit games actually LOAD DXMT for
#        d3d11/dxgi/d3d10core. This is what paints the Warcraft III Reforged
#        in-game menu (a cross-process D3D11 shared texture D3DMetal can't do).
#        D3DMetal still serves d3d12; the i386 launcher tree is untouched.
#
#   Provenance of each overlaid piece (matches the donor exactly):
#     - D3DMetal.framework + libd3dshared.dylib + 64-bit d3d DLLs
#       (d3d10/11/12/dxgi/atidxx64/nvapi64 + nvngx-on-metalfx->nvngx):
#       from the FREE GPTK 3.0 redist (~/Downloads/Game_Porting_Toolkit_3.0.dmg).
#     - DXMT v0.72 tree (i386 + x86_64 d3d10core/d3d11/dxgi/winemetal +
#       x86_64-unix/winemetal.so) and libMoltenVK.dylib: these are NEITHER
#       in the GPTK redist NOR in the wine tarball. By default we copy them
#       from the D4Mac donor (guaranteed ABI-matched to the CX26 line that
#       renders). Set DXMT_SOURCE=upstream to fetch 3Shain/dxmt + MoltenVK
#       releases instead (untested ABI pairing — donor is preferred).
#
# IDEMPOTENT: re-runnable. Each phase is gated by a marker / existence
# check; pass --rebuild to force a clean wine recompile, --force-overlay to
# re-lay the GPTK/DXMT overlay from scratch.
#
# PREREQS (run once; see prereqCommands in the plan):
#   - Xcode Command Line Tools (clang)         : xcode-select --install
#   - Rosetta 2 (build runs under arch -x86_64) : softwareupdate --install-rosetta --agree-to-license
#   - The GPTK 3.0 dmg at ~/Downloads/Game_Porting_Toolkit_3.0.dmg
#   - (DXMT_SOURCE=donor, the default) D4Mac.app installed, OR
#     (DXMT_SOURCE=upstream) network access to GitHub releases.
#
# SCREEN-BLIND CAVEAT: this script verifies CAPABILITY (Metal symbols in
# winemac.so, correct overlay, wineboot succeeds). It CANNOT confirm a
# rendered pixel. Final render of the Battle.net CEF launcher must be
# confirmed by the USER (see verificationPlan).

set -euo pipefail

# ----------------------------------------------------------------------------
# Configuration (override via env)
# ----------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MEAD_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Pinned cxbuilder ref — HEAD 'fixup cx25', 2025-06-03. Verified on disk.
CXBUILDER_REPO="${CXBUILDER_REPO:-https://github.com/101arrowz/cxbuilder.git}"
CXBUILDER_REF="${CXBUILDER_REF:-c8bcb2bd2a9d24d1287d1f10a1af4741f59c0fcf}"

# CrossOver LGPL source = Wine 11.0 = D4Mac's exact base. Range-GET works;
# reference the tarball by explicit version (do NOT scrape the dir listing).
CX_VERSION="${CX_VERSION:-26.1.0}"
CX_SOURCE_URL="${CX_SOURCE_URL:-https://media.codeweavers.com/pub/crossover/source/crossover-sources-${CX_VERSION}.tar.gz}"

# GPTK 3.0 redist (Apple's free redistributable; nested dmg).
GPTK_DMG="${GPTK_DMG:-$HOME/Downloads/Game_Porting_Toolkit_3.0.dmg}"

# DXMT + MoltenVK source. 'donor' = copy from D4Mac (default, ABI-safe).
# 'upstream' = fetch 3Shain/dxmt v0.72 + KhronosGroup/MoltenVK releases.
# NOTE: this only stages DXMT under lib/external (donor-parity) + MoltenVK + the
# i386 tree. It does NOT make wine LOAD DXMT — that's DXMT_MENU_FIX below.
DXMT_SOURCE="${DXMT_SOURCE:-donor}"
D4MAC_WINE="${D4MAC_WINE:-/Applications/D4Mac.app/Contents/SharedSupport/Wine}"
DXMT_VERSION="${DXMT_VERSION:-v0.72}"
DXMT_UPSTREAM_URL="${DXMT_UPSTREAM_URL:-https://github.com/3Shain/dxmt/releases/download/${DXMT_VERSION}/dxmt-${DXMT_VERSION}.tar.gz}"
MOLTENVK_UPSTREAM_URL="${MOLTENVK_UPSTREAM_URL:-https://github.com/KhronosGroup/MoltenVK/releases/latest/download/MoltenVK-macos.tar}"

# DXMT_MENU_FIX: build a PATCHED DXMT v0.80 from source and install it into the
# wine BUILTIN tree (lib/wine/x86_64-windows + x86_64-unix/winemetal.so) so wine
# actually LOADS DXMT for 64-bit d3d11/dxgi/d3d10core. The unmodified build only
# stages DXMT inertly under lib/external. Loading DXMT is what paints the Warcraft
# III Reforged in-game menu: BlizzardBrowser composites it via a cross-process
# D3D11 shared texture that Apple D3DMetal cannot provide. The patch
# (scripts/dxmt/wc3-menu.patch) is:
#   - a small Windows-like adapter LUID (BlizzardBrowser's --gpuluid parse overflowed
#     DXMT's huge bswap(registryID) LUID -> "Failed to find the GPU adapter"); and
#   - a no-op IDXGIKeyedMutex (CEF's accelerated shared texture requires the interface).
# EFFECT: switches the 64-bit d3d11/d3d10/dxgi backend from Apple D3DMetal to DXMT
# (D3DMetal still serves d3d12). The 32-bit launcher (i386 wined3d) is untouched.
# Set DXMT_MENU_FIX=0 to keep the stock all-D3DMetal backend (no in-game CEF menu).
DXMT_MENU_FIX="${DXMT_MENU_FIX:-1}"
DXMT_MENU_REPO="${DXMT_MENU_REPO:-https://github.com/3Shain/dxmt.git}"
DXMT_MENU_VERSION="${DXMT_MENU_VERSION:-v0.80}"   # MIT release the patch is based on
# Optional caches to skip the slow LLVM build / reuse an existing wine build dir.
DXMT_LLVM_PATH="${DXMT_LLVM_PATH:-}"
DXMT_WINE_BUILD="${DXMT_WINE_BUILD:-}"
DXMT_BUILD_DIR=""   # set by build_dxmt(), consumed by install_dxmt_route_c()

# Work + output dirs.
WORK_DIR="${WORK_DIR:-$SCRIPT_DIR/.wine-build}"
CXBUILDER_DIR="$WORK_DIR/cxbuilder"
WINE_SRC_DIR="$WORK_DIR/wine-src"          # extracted sources/wine
GPTK_REDIST_DIR="$WORK_DIR/gptk30"          # holds redist/
# Final bin/ + lib/ (gitignored). Defaults next to the script; override
# OUT_DIR to land it in a persistent checkout (e.g. ~/projects/mead/scripts/wine
# so it survives worktree cleanup and wine.Locator auto-detects it).
OUT_DIR="${OUT_DIR:-$SCRIPT_DIR/wine}"

# Build knobs.
REBUILD=0
FORCE_OVERLAY=0
for arg in "$@"; do
  case "$arg" in
    --rebuild)       REBUILD=1 ;;
    --force-overlay) FORCE_OVERLAY=1 ;;
    -h|--help)
      grep -E '^# ' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 1 ;;
  esac
done

# ----------------------------------------------------------------------------
# Logging helpers
# ----------------------------------------------------------------------------
log()  { printf '\033[1;34m[build-wine]\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m[build-wine]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[build-wine]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m[build-wine] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# ----------------------------------------------------------------------------
# Phase 0: preconditions
# ----------------------------------------------------------------------------
preflight() {
  log "preflight checks"
  [[ "$(uname -s)" == "Darwin" ]] || die "macOS only"
  [[ "$(uname -m)" == "arm64" ]]  || warn "not Apple Silicon; build still forced x86_64 by cxbuilder"

  # macOS 15+ required: GPTK 3.0 D3DMetal dlopens /System .../D3DMetal.framework.
  local osmajor
  osmajor="$(sw_vers -productVersion | cut -d. -f1)"
  [[ "$osmajor" -ge 15 ]] || die "macOS 15+ required for GPTK 3.0 D3DMetal (have $(sw_vers -productVersion))"

  command -v clang >/dev/null   || die "clang not found; run: xcode-select --install"
  command -v git >/dev/null     || die "git not found"
  command -v curl >/dev/null    || die "curl not found"
  command -v hdiutil >/dev/null || die "hdiutil not found"
  command -v ditto >/dev/null   || die "ditto not found"
  # cxbuilder's microbrew dependency fetcher hard-requires python (it shells
  # `command -v python || command -v python3`). Fail fast with a clear message
  # rather than dying deep in the build.
  command -v python3 >/dev/null || command -v python >/dev/null || \
    die "python3 not found (required by cxbuilder microbrew); install via xcode-select or brew"

  # Rosetta 2: the build runs under `arch -x86_64`.
  if ! /usr/bin/arch -x86_64 /usr/bin/true >/dev/null 2>&1; then
    die "Rosetta 2 missing; run: softwareupdate --install-rosetta --agree-to-license"
  fi

  [[ -f "$GPTK_DMG" ]] || die "GPTK 3.0 dmg not found at $GPTK_DMG (set GPTK_DMG=...)"

  if [[ "$DXMT_SOURCE" == "donor" ]]; then
    [[ -d "$D4MAC_WINE/lib/external/dxmt" ]] || \
      die "DXMT_SOURCE=donor but $D4MAC_WINE/lib/external/dxmt not found. Install D4Mac or set DXMT_SOURCE=upstream."
  fi

  mkdir -p "$WORK_DIR"
  ok "preflight OK (macOS $(sw_vers -productVersion), Rosetta present)"
}

# ----------------------------------------------------------------------------
# Phase 1: fetch cxbuilder pinned
# ----------------------------------------------------------------------------
fetch_cxbuilder() {
  if [[ -d "$CXBUILDER_DIR/.git" ]]; then
    local have; have="$(git -C "$CXBUILDER_DIR" rev-parse HEAD 2>/dev/null || echo none)"
    if [[ "$have" == "$CXBUILDER_REF" ]]; then
      ok "cxbuilder already at $CXBUILDER_REF"
      return
    fi
    log "cxbuilder at $have; checking out $CXBUILDER_REF"
  else
    log "cloning cxbuilder"
    git clone "$CXBUILDER_REPO" "$CXBUILDER_DIR"
  fi
  git -C "$CXBUILDER_DIR" fetch --depth 1 origin "$CXBUILDER_REF" 2>/dev/null || git -C "$CXBUILDER_DIR" fetch origin
  git -C "$CXBUILDER_DIR" checkout -q "$CXBUILDER_REF"
  ok "cxbuilder pinned to $CXBUILDER_REF"
}

# ----------------------------------------------------------------------------
# Phase 1b: teach cxbuilder's dependency fetcher about macOS 16+/26 (Tahoe)
#   cxbuilder maps the host macOS version to a Homebrew bottle codename, but
#   only through macOS 15 (sequoia, cxbuilder.sh case ~546-552); newer hosts
#   fall to "unknown" and the build aborts ("failed to load bottle info ...,
#   system type unknown"). We map macOS 16+ / 26 (Tahoe) to sequoia, which
#   resolves to arm64_sequoia build-tool bottles + sonoma x86_64 runtime
#   bottles — the exact tags cxbuilder's pinned mingw-w64 override AND
#   Homebrew both publish (there are NO tahoe bottles in cxbuilder's
#   override). x86_64 sonoma libs run fine on macOS 26 under Rosetta. We
#   patch the clone in-place (idempotent) since cxbuilder exposes no env
#   override for the codename.
# ----------------------------------------------------------------------------
patch_cxbuilder_host_os() {
  local cx="$CXBUILDER_DIR/cxbuilder.sh"
  if grep -q 'mead: macOS 16+/26' "$cx"; then
    ok "cxbuilder host-OS map already patched for Tahoe"
    return
  fi
  if ! grep -qE '15\.\*\) sys_info="sequoia";;' "$cx"; then
    warn "cxbuilder host-OS map changed shape; skipping Tahoe patch (build may fail on macOS 26)"
    return
  fi
  log "patching cxbuilder host-OS map: macOS 16+/26 (Tahoe) -> sequoia bottles"
  perl -0pi -e 's/^(\s*)15\.\*\) sys_info="sequoia";;$/$&\n${1}1[6-9].*|2[0-9].*) sys_info="sequoia";; # mead: macOS 16+\/26 (Tahoe) -> sequoia bottles/m' "$cx" \
    || die "failed to patch cxbuilder host-OS map"
  grep -q 'mead: macOS 16+/26' "$cx" || die "cxbuilder host-OS patch did not take"
  ok "cxbuilder host-OS map patched"
}

# ----------------------------------------------------------------------------
# Phase 1c: build Wine without its OpenGL backend
#   Wine 11 makes EGL (ANGLE's libEGL) MANDATORY for --with-opengl on macOS
#   (EGL_LIBS=-lEGL, hard `configure: error: EGL 64-bit development files not
#   found`; the legacy OpenGL.framework path is gone). There is no libEGL on
#   a stock macOS (no Homebrew `angle`, none in the GPTK 3.0 redist, and even
#   D4Mac ships no libEGL despite having opengl32.so — Apple's GPTK supplies
#   ANGLE out-of-tree). cxbuilder hardcodes --with-opengl, so the build dies
#   at configure. We drop it: Mead's render paths don't use wine's GL —
#   Battle.net's CEF uses bundled SwiftShader (CPU), and D3D goes through
#   D3DMetal / wined3d→Vulkan(MoltenVK), never opengl32. The only thing lost
#   is wine's GL for OpenGL-native Windows games (revisit by sourcing ANGLE).
# ----------------------------------------------------------------------------
patch_cxbuilder_disable_opengl() {
  local cx="$CXBUILDER_DIR/cxbuilder.sh"
  if grep -q -- '--without-opengl' "$cx"; then
    ok "cxbuilder already builds --without-opengl"
    return
  fi
  grep -q -- '--with-opengl' "$cx" || { warn "cxbuilder has no --with-opengl flag; skipping"; return; }
  log "patching cxbuilder: --with-opengl -> --without-opengl (no libEGL/ANGLE on this host)"
  perl -pi -e 's/--with-opengl\b/--without-opengl/g' "$cx"
  grep -q -- '--without-opengl' "$cx" || die "opengl patch did not take"
  ok "cxbuilder opengl disabled"
}

# ----------------------------------------------------------------------------
# Phase 2: fetch + extract Wine 11 (CrossOver 26.1.0) source
# ----------------------------------------------------------------------------
fetch_wine_src() {
  if [[ -f "$WINE_SRC_DIR/VERSION" ]]; then
    ok "wine source already extracted ($(cat "$WINE_SRC_DIR/VERSION"))"
    return
  fi
  log "downloading + extracting CrossOver $CX_VERSION wine source (149 MB)"
  mkdir -p "$WINE_SRC_DIR"
  # Download to a FILE first (with retry), verify size, THEN extract. Piping
  # curl straight into tar means a mid-stream cut silently corrupts the
  # extract; a file lets curl --retry resume and lets us sanity-check size.
  local tgz="$WORK_DIR/crossover-sources-${CX_VERSION}.tar.gz"
  if [[ ! -f "$tgz" ]]; then
    curl -fSL --retry 3 --retry-delay 2 -A 'Mozilla/5.0' -o "$tgz.partial" "$CX_SOURCE_URL" \
      || die "failed to download wine source from $CX_SOURCE_URL"
    mv -f "$tgz.partial" "$tgz"
  fi
  local sz; sz="$(stat -f%z "$tgz" 2>/dev/null || echo 0)"
  [[ "$sz" -gt 100000000 ]] || die "downloaded tarball suspiciously small ($sz bytes); delete $tgz and re-run"
  # --strip-components=2 unwraps sources/wine -> repo root of the wine tree.
  tar -zx --strip-components=2 -C "$WINE_SRC_DIR" -f "$tgz" sources/wine \
    || die "failed to extract sources/wine from $tgz"
  [[ -f "$WINE_SRC_DIR/VERSION" ]] || die "extraction produced no VERSION file"

  # Render-crux gate: the built winemac.so renders ONLY if the Metal bridge
  # source is present. Verify BEFORE the long build. Empty => infeasible.
  log "verifying Metal D3DMetal bridge is present in winemac.drv source"
  local md="$WINE_SRC_DIR/dlls/winemac.drv"
  if ! grep -rqs 'd3dmetal_client_surface' "$md" \
     || ! grep -rqs 'macdrv_create_metal_device' "$md"; then
    die "RENDER CRUX FAILURE: winemac.drv source has NO d3dmetal Metal bridge.
         A free-sources build CANNOT render. Stop and keep depending on D4Mac.
         (This is the exact dead end the abandoned wine-cx 10.0 build hit.)"
  fi
  ok "Metal bridge present ($(cat "$WINE_SRC_DIR/VERSION")); render is feasible"
}

# ----------------------------------------------------------------------------
# Phase 2b: apply cxbuilder's version-gated source patches
#   cxbuilder.sh itself does NOT apply patches/ — its CI does, BEFORE invoking
#   cxbuilder.sh. We mirror that. Each patch carries a `# apply_to:` regex;
#   we apply only those matching CX_VERSION. For the pinned 26.1.0 ALL of
#   cxbuilder's current patches (gated 24.*/25.*) SKIP — so this is a no-op
#   today, but it keeps the CX_VERSION knob safe to turn later.
# ----------------------------------------------------------------------------
apply_patches() {
  local marker="$WINE_SRC_DIR/.mead-patches-applied"
  if [[ -f "$marker" ]]; then ok "wine source patches already applied"; return; fi
  local pdir="$CXBUILDER_DIR/patches"
  if [[ ! -d "$pdir" ]]; then ok "no cxbuilder patches/ dir; nothing to apply"; touch "$marker"; return; fi
  log "applying version-gated cxbuilder patches for CX $CX_VERSION"
  local p applied=0
  shopt -s nullglob
  for p in "$pdir"/*.patch "$pdir"/*.diff; do
    local rng; rng="$(grep -m1 -E '# *apply_to:' "$p" | sed -E 's/.*apply_to: *//' | tr -d '[:space:]')"
    if [[ -n "$rng" && ! "$CX_VERSION" =~ $rng ]]; then
      log "  skip $(basename "$p") (apply_to=$rng)"
      continue
    fi
    log "  apply $(basename "$p")"
    git apply --recount --unsafe-paths --directory="$WINE_SRC_DIR" "$p" \
      || die "patch failed to apply: $p"
    applied=$((applied + 1))
  done
  shopt -u nullglob
  touch "$marker"
  ok "patches applied: $applied (others skipped by version gate)"
}

# ----------------------------------------------------------------------------
# Phase 3: extract GPTK 3.0 redist (read-only mount; ditto out; never mutate)
# ----------------------------------------------------------------------------
extract_gptk_redist() {
  if [[ -d "$GPTK_REDIST_DIR/redist/lib/external/D3DMetal.framework" ]]; then
    ok "GPTK 3.0 redist already extracted"
    return
  fi
  log "extracting GPTK 3.0 redist from $GPTK_DMG (read-only)"
  local outer inner
  outer="$(mktemp -d)"; inner="$(mktemp -d)"
  # Mount read-only; -nobrowse keeps it out of Finder. RO mount, no shadow.
  hdiutil attach "$GPTK_DMG" -nobrowse -readonly -mountpoint "$outer" >/dev/null \
    || die "failed to mount $GPTK_DMG"
  trap 'hdiutil detach "$outer" >/dev/null 2>&1 || true; hdiutil detach "$inner" >/dev/null 2>&1 || true' RETURN

  local nested
  nested="$(/bin/ls "$outer"/*.dmg 2>/dev/null | head -1)" \
    || die "no nested dmg inside GPTK dmg"
  [[ -n "$nested" ]] || die "no nested 'Evaluation environment ... .dmg' found in $outer"
  hdiutil attach "$nested" -nobrowse -readonly -mountpoint "$inner" >/dev/null \
    || die "failed to mount nested $nested"

  [[ -d "$inner/redist" ]] || die "nested dmg has no redist/ ($inner)"
  rm -rf "$GPTK_REDIST_DIR"
  mkdir -p "$GPTK_REDIST_DIR"
  # ditto copies OUT of the read-only mount, preserving framework symlinks.
  ditto "$inner/redist" "$GPTK_REDIST_DIR/redist" || die "ditto redist failed"

  # GPTK 3.0 renamed nvngx.dll -> nvngx-on-metalfx.dll. Normalize to nvngx.dll
  # so our overlay can treat it uniformly (donor ships plain nvngx.dll).
  local g="$GPTK_REDIST_DIR/redist/lib/wine/x86_64-windows"
  if [[ -f "$g/nvngx-on-metalfx.dll" && ! -f "$g/nvngx.dll" ]]; then
    cp -p "$g/nvngx-on-metalfx.dll" "$g/nvngx.dll"
  fi
  ok "GPTK 3.0 redist staged at $GPTK_REDIST_DIR/redist (source dmg untouched)"
}

# ----------------------------------------------------------------------------
# Phase 4: build Wine via cxbuilder (clean — no gptk/dxvk overlay here)
# ----------------------------------------------------------------------------
build_wine() {
  if [[ -x "$OUT_DIR/bin/wine" && "$REBUILD" -eq 0 ]]; then
    ok "wine already built at $OUT_DIR/bin/wine ($("$OUT_DIR/bin/wine" --version 2>/dev/null || echo '?'))"
    return
  fi
  log "building Wine $CX_VERSION via cxbuilder (~35 min, ~10 GB; runs under Rosetta)"
  local rebuild_flag=""
  [[ "$REBUILD" -eq 1 ]] && rebuild_flag="--rebuild"

  # --no-gptk --no-dxvk: produce a pristine Metal-backed wine. We overlay
  # GPTK 3.0 + DXMT ourselves (Phase 5) to match the D4Mac lib/external
  # layout that Mead's Preamble detects. cxbuilder fetches its own Intel
  # Homebrew bottles (microbrew) — no system brew needed.
  #   -x = --no-prompt (NON-INTERACTIVE): without it, cxbuilder blocks on a
  #        `read -r` prompt ("Found existing Wine build. Clean and continue?")
  #        on any rebuild and stalls the unattended build forever. CI passes
  #        it too. -v = verbose.
  # CFLAGS for the NATIVE (macOS clang, x86_64-via-Rosetta) compiles: CI's
  # -O3 + x86-64-v2 baseline for parity, plus the macOS-26.4 fix.
  #   -Wno-error=unguarded-availability-new: the macOS 26.4 SDK newly marks
  #   some libc APIs (e.g. fdclosedir) as @26.4, but cxbuilder's deployment
  #   target defaults to <major>.0 (26.0), so third-party deps that call
  #   them unguarded (libinotify-kqueue) fail under -Werror. The symbols
  #   exist at runtime on this host, so downgrade that one warning.
  export CFLAGS="${CFLAGS:--O3 -march=x86-64-v2 -maes -mpclmul -Wno-error=unguarded-availability-new}"
  # CRITICAL: do NOT reuse $CFLAGS for the cross compile. The mingw PE
  # cross-compiler is GCC (i686/x86_64-w64-mingw32-gcc), which (a) rejects
  # the clang-only -Wno-error=unguarded-availability-new ("cc1: no option
  # -Wunguarded-availability-new") and (b) can't take -march=x86-64-v2 for
  # the 32-bit i686 target. Either makes wine's "i686-w64-mingw32-gcc
  # works" probe fail -> "i386 PE cross-compiler not found". Leave
  # CROSSCFLAGS unset so wine uses its working cross defaults (what the
  # cxbuilder CI artifacts build with for the PE DLLs).
  unset CROSSCFLAGS
  sh "$CXBUILDER_DIR/cxbuilder.sh" \
    -x \
    -v \
    --wine "$WINE_SRC_DIR" \
    --no-gptk \
    --no-dxvk \
    $rebuild_flag \
    --out "$OUT_DIR" \
    || die "cxbuilder failed (inspect $OUT_DIR/.cxbuilder logs)"

  [[ -x "$OUT_DIR/bin/wine" ]] || die "cxbuilder produced no bin/wine"
  ok "wine built: $("$OUT_DIR/bin/wine" --version)"
}

# ----------------------------------------------------------------------------
# Phase 5: overlay GPTK 3.0 + DXMT into lib/external (match D4Mac donor)
# ----------------------------------------------------------------------------
overlay() {
  local marker="$OUT_DIR/lib/external/.mead-overlay-done"
  if [[ -f "$marker" && "$FORCE_OVERLAY" -eq 0 ]]; then
    ok "overlay already applied (pass --force-overlay to redo)"
    return
  fi
  log "overlaying GPTK 3.0 + DXMT into lib/external"

  local ext="$OUT_DIR/lib/external"
  local win64="$OUT_DIR/lib/wine/x86_64-windows"
  local unix64="$OUT_DIR/lib/wine/x86_64-unix"
  local redist="$GPTK_REDIST_DIR/redist"
  mkdir -p "$ext"

  # 5a. D3DMetal.framework + libd3dshared.dylib (from GPTK 3.0 redist).
  rm -rf "$ext/D3DMetal.framework"
  ditto "$redist/lib/external/D3DMetal.framework" "$ext/D3DMetal.framework"
  cp -p "$redist/lib/external/libd3dshared.dylib" "$ext/libd3dshared.dylib"

  # 5b. 64-bit d3d builtins: overwrite cxbuilder's wined3d builtins with the
  # GPTK d3dmetal variants (this IS the "d3dmetal swap" — d3d11/d3d12/dxgi
  # become the Apple D3DMetal ones, as on the donor). nvngx normalized in
  # Phase 3. NOTE the overlay set matches the D4Mac donor EXACTLY: d3d10 and
  # d3d10core stay WINE BUILTINS (the donor does not overlay them; d3d10
  # rides on dxgi+d3d11), so they are deliberately absent from this list.
  local d
  for d in d3d11 d3d12 dxgi atidxx64 nvapi64 nvngx; do
    if [[ -f "$redist/lib/wine/x86_64-windows/$d.dll" ]]; then
      # Preserve the wine builtin alongside (.wined3d) like cxbuilder does.
      [[ -f "$win64/$d.dll" && ! -f "$win64/$d.dll.wined3d" ]] && \
        mv -f "$win64/$d.dll" "$win64/$d.dll.wined3d"
      cp -p "$redist/lib/wine/x86_64-windows/$d.dll" "$win64/$d.dll"
    fi
  done

  # 5c. x86_64-unix .so thunks -> symlink to libd3dshared.dylib (donor layout:
  # atidxx64/d3d11/d3d12/dxgi/nvapi64/nvngx .so all point at the shared dylib).
  for d in atidxx64 d3d11 d3d12 dxgi nvapi64 nvngx; do
    [[ -e "$unix64/$d.so" && ! -e "$unix64/$d.so.wined3d" ]] && \
      mv -f "$unix64/$d.so" "$unix64/$d.so.wined3d" 2>/dev/null || true
    ln -sf "../../external/libd3dshared.dylib" "$unix64/$d.so"
  done

  # 5d. DXMT v0.72 tree + libMoltenVK.dylib (the 32-bit CEF path + MoltenVK).
  overlay_dxmt_and_moltenvk

  # 5e. Re-sign overlaid Mach-O so Gatekeeper/library-validation is happy.
  #     (DLLs are PE, skipped; only dylibs/frameworks/.so need ad-hoc sigs.)
  log "re-signing overlaid Mach-O (ad-hoc)"
  codesign -f -s - "$ext/libd3dshared.dylib" 2>/dev/null || true
  codesign -f -s - "$ext/libMoltenVK.dylib"  2>/dev/null || true
  codesign -f -s - --deep "$ext/D3DMetal.framework" 2>/dev/null || true
  if [[ -f "$ext/dxmt/x86_64-unix/winemetal.so" ]]; then
    codesign -f -s - "$ext/dxmt/x86_64-unix/winemetal.so" 2>/dev/null || true
  fi

  # 5f. Strip the macOS quarantine attribute from the overlaid Apple libs.
  # ditto/cp out of the GPTK dmg + the D4Mac donor carries
  # com.apple.quarantine; a non-notarized self-built Wine then trips
  # Gatekeeper on the FIRST dlopen of libd3dshared.dylib / D3DMetal.framework
  # ("Apple could not verify … is free of malware"), which BLOCKS the load
  # (and the dialog's default "Move to Trash" would delete the lib). The
  # symbols are exactly the same Apple D3DMetal bits D4Mac ships — they're
  # safe; the quarantine is just the downloaded-from-the-dmg provenance.
  # Strip it from the whole output tree. Errors are tolerated on purpose:
  # cxbuilder's own on-machine-built dylibs are read-only and carry NO
  # quarantine, so `xattr -d` errors harmlessly on them — and under
  # `set -e` we must not let that abort the build.
  log "stripping com.apple.quarantine from the overlaid libs"
  xattr -rd com.apple.quarantine "$OUT_DIR" 2>/dev/null || true

  touch "$marker"
  ok "overlay complete; lib/external now matches the D4Mac donor (de-quarantined)"
}

overlay_dxmt_and_moltenvk() {
  local ext="$OUT_DIR/lib/external"
  if [[ "$DXMT_SOURCE" == "donor" ]]; then
    log "copying DXMT + libMoltenVK from D4Mac donor (ABI-matched)"
    rm -rf "$ext/dxmt"
    ditto "$D4MAC_WINE/lib/external/dxmt" "$ext/dxmt"
    cp -p "$D4MAC_WINE/lib/external/libMoltenVK.dylib" "$ext/libMoltenVK.dylib"
  else
    log "fetching DXMT $DXMT_VERSION + MoltenVK from upstream releases"
    local tmp; tmp="$(mktemp -d)"
    # DXMT release tarball -> populate dxmt/{version,LICENSE,*-windows,x86_64-unix}.
    curl -fSL "$DXMT_UPSTREAM_URL" | tar -zx -C "$tmp" \
      || die "failed to fetch DXMT from $DXMT_UPSTREAM_URL"
    rm -rf "$ext/dxmt"; mkdir -p "$ext/dxmt"
    # DXMT release layouts vary; copy the dxmt/ subtree if present, else top.
    if [[ -d "$tmp/dxmt" ]]; then ditto "$tmp/dxmt" "$ext/dxmt"
    else ditto "$tmp" "$ext/dxmt"; fi
    # MoltenVK release: dylib lives under MoltenVK/dynamic/dylib/macOS/.
    local mvtmp; mvtmp="$(mktemp -d)"
    curl -fSL "$MOLTENVK_UPSTREAM_URL" | tar -x -C "$mvtmp" \
      || die "failed to fetch MoltenVK from $MOLTENVK_UPSTREAM_URL"
    local mvk
    mvk="$(/usr/bin/find "$mvtmp" -name libMoltenVK.dylib -type f | head -1)"
    [[ -n "$mvk" ]] || die "libMoltenVK.dylib not found in MoltenVK release"
    cp -p "$mvk" "$ext/libMoltenVK.dylib"
    warn "DXMT_SOURCE=upstream: ABI pairing with CX26 winemac.so is UNVERIFIED."
  fi
}

# ----------------------------------------------------------------------------
# Phase 5b: build PATCHED DXMT v0.80 from source (the WC3 in-game menu fix)
#   Builds DXMT's d3d11/dxgi/d3d10core/winemetal for x86_64 from 3Shain/dxmt
#   @ DXMT_MENU_VERSION + scripts/dxmt/wc3-menu.patch, against a static LLVM 15
#   (cross-compiled) and the wine build dir cxbuilder just produced (winebuild +
#   headers). Heavy the first time (~30 min for LLVM); both LLVM and the DXMT
#   build cache, so reruns are fast. Skipped entirely when DXMT_MENU_FIX=0.
# ----------------------------------------------------------------------------
build_dxmt() {
  [[ "$DXMT_MENU_FIX" == "1" ]] || { ok "DXMT_MENU_FIX=0 — skipping patched DXMT build (stock D3DMetal d3d11)"; return; }

  local patch="$SCRIPT_DIR/dxmt/wc3-menu.patch"
  local nativefile="$SCRIPT_DIR/dxmt/build-native-x86_64.txt"
  [[ -f "$patch" ]]      || die "DXMT patch missing: $patch"
  [[ -f "$nativefile" ]] || die "DXMT native file missing: $nativefile"
  for t in meson ninja cmake git x86_64-w64-mingw32-gcc; do
    command -v "$t" >/dev/null || die "DXMT_MENU_FIX=1 needs '$t' (brew install meson ninja cmake mingw-w64); or set DXMT_MENU_FIX=0"
  done

  local dxdir="$WORK_DIR/dxmt"
  mkdir -p "$dxdir"

  # 1. LLVM 15 (static, x86_64) — the long pole. DXMT links it statically, so a
  #    brew dylib won't do; build from source. Cache-aware (skip if present).
  local llvm="${DXMT_LLVM_PATH:-$dxdir/toolchains/llvm}"
  if [[ -d "$llvm/lib" && -e "$llvm/bin/llvm-config" ]]; then
    ok "LLVM 15 toolchain present at $llvm (cached)"
  else
    log "building LLVM 15.0.7 static x86_64 (~30 min, cached after first run)"
    [[ -d "$dxdir/llvm-project/.git" ]] || \
      git clone --depth 1 --branch llvmorg-15.0.7 https://github.com/llvm/llvm-project.git "$dxdir/llvm-project" \
        || die "LLVM clone failed"
    cmake -G Ninja -B "$dxdir/llvm-build" -S "$dxdir/llvm-project/llvm" \
      -DCMAKE_INSTALL_PREFIX="$llvm" -DCMAKE_OSX_ARCHITECTURES=x86_64 \
      -DLLVM_HOST_TRIPLE=x86_64-apple-darwin -DLLVM_ENABLE_ASSERTIONS=On \
      -DLLVM_ENABLE_ZSTD=Off -DCMAKE_BUILD_TYPE=Release \
      -DLLVM_TARGETS_TO_BUILD="" -DLLVM_BUILD_TOOLS=Off \
      || die "LLVM cmake configure failed"
    cmake --build "$dxdir/llvm-build"  || die "LLVM build failed"
    cmake --install "$dxdir/llvm-build" || die "LLVM install failed"
    ok "LLVM 15 built + installed to $llvm"
  fi

  # 2. DXMT source @ the patch base, with the WC3-menu patch applied (idempotent:
  #    reset tracked files first, then apply).
  local src="$dxdir/dxmt-src"
  if [[ ! -d "$src/.git" ]]; then
    log "cloning DXMT ($DXMT_MENU_VERSION)"
    git clone "$DXMT_MENU_REPO" "$src" || die "DXMT clone failed"
  fi
  git -C "$src" fetch --tags --depth 1 origin "$DXMT_MENU_VERSION" 2>/dev/null || git -C "$src" fetch --tags origin
  git -C "$src" checkout -q "$DXMT_MENU_VERSION" || die "DXMT checkout $DXMT_MENU_VERSION failed"
  git -C "$src" submodule update --init --recursive || die "DXMT submodule init failed"
  git -C "$src" checkout -q -- .   # drop any previous patch hunks
  git -C "$src" apply "$patch" || die "failed to apply $patch onto DXMT $DXMT_MENU_VERSION"
  cp -p "$nativefile" "$src/build-native-x86_64.txt"
  ok "DXMT $DXMT_MENU_VERSION checked out + wc3-menu.patch applied"

  # 3. locate the wine build dir (winebuild marks it) from the cxbuilder build.
  local winebuild
  winebuild="${DXMT_WINE_BUILD:-$(/usr/bin/find "$WORK_DIR" -type f -name winebuild -path '*tools/winebuild*' 2>/dev/null | head -1)}"
  [[ -n "$winebuild" ]] || die "could not locate the wine build dir (winebuild) under $WORK_DIR — build wine first"
  local wine_build_path
  if [[ -d "$winebuild" ]]; then wine_build_path="$winebuild"            # caller passed the dir
  else wine_build_path="$(cd "$(dirname "$winebuild")/../.." && pwd)"; fi # .../wine/build
  log "DXMT wine_build_path: $wine_build_path"

  # 4. meson cross-build (x86_64 windows DLLs + winemetal.so).
  ( cd "$src" && rm -rf build && \
    meson setup --cross-file build-win64.txt --native-file build-native-x86_64.txt \
      -Dnative_llvm_path="$llvm" -Dwine_build_path="$wine_build_path" --buildtype release build \
    && meson compile -C build ) \
    || die "DXMT meson build failed"

  for d in src/d3d11/d3d11.dll src/dxgi/dxgi.dll src/d3d10/d3d10core.dll \
           src/winemetal/winemetal.dll src/winemetal/unix/winemetal.so; do
    [[ -f "$src/build/$d" ]] || die "DXMT build produced no $d"
  done
  DXMT_BUILD_DIR="$src/build"
  ok "patched DXMT built: $DXMT_BUILD_DIR"
}

# ----------------------------------------------------------------------------
# Phase 5c: install patched DXMT into the wine builtin tree (Route C)
#   DXMT's PEs are "Wine builtin"-signed, so wine loads them ONLY from
#   lib/wine/<arch>-windows (+ winemetal.so in <arch>-unix), NOT the exe dir.
#   Copying them there makes the 64-bit game + CEF menu load DXMT. The 32-bit
#   tree (i386 wined3d, the Battle.net launcher) is deliberately left untouched.
# ----------------------------------------------------------------------------
install_dxmt_route_c() {
  [[ "$DXMT_MENU_FIX" == "1" ]] || return
  [[ -n "$DXMT_BUILD_DIR" && -d "$DXMT_BUILD_DIR" ]] || die "install_dxmt_route_c: DXMT not built (build_dxmt ran?)"
  local win64="$OUT_DIR/lib/wine/x86_64-windows" unix64="$OUT_DIR/lib/wine/x86_64-unix"
  [[ -d "$win64" && -d "$unix64" ]] || die "wine install tree missing ($win64) — build wine + overlay first"

  log "installing patched DXMT into the wine builtin tree (Route C)"
  cp -p "$DXMT_BUILD_DIR/src/d3d11/d3d11.dll"             "$win64/d3d11.dll"
  cp -p "$DXMT_BUILD_DIR/src/dxgi/dxgi.dll"               "$win64/dxgi.dll"
  cp -p "$DXMT_BUILD_DIR/src/d3d10/d3d10core.dll"         "$win64/d3d10core.dll"
  cp -p "$DXMT_BUILD_DIR/src/winemetal/winemetal.dll"     "$win64/winemetal.dll"
  cp -p "$DXMT_BUILD_DIR/src/winemetal/unix/winemetal.so" "$unix64/winemetal.so"
  # winemetal.so is a Mach-O unixlib; any edit invalidates its adhoc sig -> re-sign.
  codesign -f -s - "$unix64/winemetal.so" 2>/dev/null || warn "codesign winemetal.so failed (dlopen may be blocked)"
  ok "patched DXMT installed (Route C): 64-bit d3d11/dxgi/d3d10core -> DXMT, i386 launcher untouched"
}

# ----------------------------------------------------------------------------
# Phase 6: wine64 compat symlink + entitlements (Locator + Preamble)
# ----------------------------------------------------------------------------
finalize_loader() {
  log "finalizing loader (wine64 symlink + entitlements)"
  # Mead's Locator resolves wine64 by name in most fallbacks, but the
  # unified WOW64 build (and D4Mac) ship only `wine`. Provide the symlink.
  if [[ -x "$OUT_DIR/bin/wine" && ! -e "$OUT_DIR/bin/wine64" ]]; then
    ln -sf wine "$OUT_DIR/bin/wine64"
  fi

  # The loader must carry the same entitlements D4Mac's wine does, so
  # Preamble's DYLD_FALLBACK_LIBRARY_PATH survives exec into the hardened
  # process and D3D titles' JIT/shader paths work. cxbuilder ad-hoc signs
  # with NO entitlements, so apply them explicitly. (Verified against the
  # D4Mac donor's `codesign -d --entitlements -`: it carries all six below.)
  local ent; ent="$(mktemp -t mead-wine-ent).plist"
  cat > "$ent" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>com.apple.security.app-sandbox</key><false/>
  <key>com.apple.security.cs.allow-dyld-environment-variables</key><true/>
  <key>com.apple.security.cs.disable-library-validation</key><true/>
  <key>com.apple.security.cs.disable-executable-page-protection</key><true/>
  <key>com.apple.security.cs.allow-jit</key><true/>
  <key>com.apple.security.cs.allow-unsigned-executable-memory</key><true/>
</dict>
</plist>
PLIST
  # Sign the REAL wine (not the symlink) with our entitlements, ad-hoc.
  codesign -f -s - --entitlements "$ent" --options runtime "$OUT_DIR/bin/wine" 2>/dev/null \
    || codesign -f -s - --entitlements "$ent" "$OUT_DIR/bin/wine" \
    || die "failed to codesign wine loader with entitlements"
  rm -f "$ent"
  ok "loader finalized"
}

# ----------------------------------------------------------------------------
# Phase 7: capability verification (NOT a render — screen-blind)
# ----------------------------------------------------------------------------
verify() {
  log "verifying build capability"
  local wine="$OUT_DIR/bin/wine"
  local unix64="$OUT_DIR/lib/wine/x86_64-unix"

  # 7a. version
  local v; v="$("$wine" --version 2>/dev/null || echo unknown)"
  [[ "$v" == wine-11* ]] || warn "unexpected wine version: $v (wanted wine-11.x)"
  log "wine --version: $v"

  # 7b. THE render crux, checked on the BINARY: winemac.so must link Metal
  # and DEFINE the d3dmetal bridge symbols. Capture otool/nm output ONCE into
  # vars and match with bash ==/=~ (no pipe): `nm … | grep -q` trips
  # set -o pipefail — grep -q exits on first match, nm gets SIGPIPE and exits
  # non-zero, so the pipeline reports failure even though the symbol IS there.
  local otoolout symsout d3dmetal_re='_macdrv_(get|set)_view_d3dmetal_client_surface'
  otoolout="$(otool -L "$unix64/winemac.so" 2>/dev/null || true)"
  symsout="$(nm "$unix64/winemac.so" 2>/dev/null || true)"
  if [[ "$otoolout" == *Metal.framework* ]]; then
    ok "winemac.so links Metal.framework"
  else
    die "winemac.so does NOT link Metal.framework — build is not Metal-backed"
  fi
  if [[ "$symsout" == *_macdrv_create_metal_device* && "$symsout" =~ $d3dmetal_re ]]; then
    ok "winemac.so DEFINES the d3dmetal client-surface bridge (the make-or-break)"
  else
    die "winemac.so is MISSING the d3dmetal bridge symbols — will render nothing"
  fi

  # 7c. Preamble detection key: lib/external/D3DMetal.framework present.
  [[ -e "$OUT_DIR/lib/external/D3DMetal.framework/D3DMetal" ]] \
    || die "D3DMetal.framework not where Preamble looks (lib/external)"
  ok "D3DMetal.framework present where wine.Preamble() detects it"

  # 7c2. Quarantine must be stripped from the overlaid Apple libs (step 5f),
  # else Gatekeeper blocks the first dlopen. Check the load-bearing ones.
  # Capture xattr output into a var and match with bash (no `| grep -q`,
  # which trips set -o pipefail when grep exits early and xattr gets SIGPIPE).
  local q=0 qa
  for f in lib/external/libd3dshared.dylib lib/external/D3DMetal.framework/Versions/A/D3DMetal lib/external/libMoltenVK.dylib; do
    qa="$(xattr "$OUT_DIR/$f" 2>/dev/null || true)"
    if [[ "$qa" == *com.apple.quarantine* ]]; then
      warn "$f still quarantined — Gatekeeper will block it on first load"
      q=1
    fi
  done
  [[ "$q" -eq 0 ]] && ok "overlaid Apple libs are de-quarantined (no Gatekeeper block)"

  # 7d. Overlay forensics — prove the Apple D3DMetal DLLs actually landed
  # (not plain wined3d), and that the unix .so thunks point at libd3dshared.
  # This is the make-or-break check on the OVERLAYED artifact: the donor's
  # working prefix shows these exact Apple DLLs (every one carries a
  # 'D3DMetalDLLsBase' build-path string); a plain wined3d build would not.
  local win64="$OUT_DIR/lib/wine/x86_64-windows"
  # When DXMT_MENU_FIX=1, install_dxmt_route_c has replaced d3d11/dxgi with DXMT
  # (verified separately in 7e); only d3d12 stays Apple D3DMetal. Otherwise all
  # three must be D3DMetal.
  local d3dmetal_dlls="d3d11 d3d12 dxgi"
  [[ "$DXMT_MENU_FIX" == "1" ]] && d3dmetal_dlls="d3d12"
  for d in $d3dmetal_dlls; do
    if grep -qa 'D3DMetalDLLsBase' "$win64/$d.dll" 2>/dev/null; then
      ok "$d.dll is Apple D3DMetal (overlay landed)"
    else
      die "$d.dll is NOT Apple D3DMetal — the GPTK overlay did not apply; would render via plain wined3d (no Metal)"
    fi
  done
  for d in $d3dmetal_dlls; do
    local tgt; tgt="$(readlink "$unix64/$d.so" 2>/dev/null || echo '')"
    [[ "$tgt" == *libd3dshared.dylib ]] \
      || warn "$d.so does not symlink to libd3dshared.dylib (got '$tgt')"
  done

  # 7e. DXMT/MoltenVK present (matches the donor's lib/external for parity).
  # NOTE: under Mead's current code the 32-bit (PE32) Battle.net CEF launcher
  # renders via the CrossOver BUILTIN i386 d3d11 (lib/wine/i386-windows) +
  # the winemac Metal present hook — NOT via DXMT. DXMT is bundled to mirror
  # the donor's lib/external, but nothing in Mead loads it today (Preamble
  # sets no d3d11=native override / WINEDLLPATH). So these are parity checks,
  # not render-critical for the launcher.
  [[ -f "$OUT_DIR/lib/wine/i386-windows/d3d11.dll" ]] \
    || die "i386 builtin d3d11.dll missing — the 32-bit CEF launcher would have no d3d11"
  [[ -f "$OUT_DIR/lib/external/dxmt/i386-windows/d3d11.dll" ]] \
    || warn "DXMT i386 d3d11.dll missing (parity-only; not used by the launcher under Mead)"
  [[ -f "$OUT_DIR/lib/external/libMoltenVK.dylib" ]] \
    || warn "libMoltenVK.dylib missing (DXMT's Vulkan-on-Metal backend; parity-only)"

  # 7f. wineboot a scratch prefix (proves the loader + WOW64 init work), then
  # confirm wineboot DEPLOYS the overlaid Apple d3d DLL (not a stale builtin):
  # system32/d3d12.dll md5 must equal the overlaid lib/wine/x86_64-windows
  # one — the exact thing the working D4Mac prefix shows.
  local prefix; prefix="$(mktemp -d)/scratch-prefix"
  log "booting scratch prefix $prefix (no window expected; init only)"
  if env WINEPREFIX="$prefix" \
         DYLD_FALLBACK_LIBRARY_PATH="$OUT_DIR/lib/external:/usr/local/lib:/usr/lib" \
         WINEDLLOVERRIDES="winemenubuilder.exe=d;mscoree=d;mshtml=d" \
         WINEDEBUG=fixme-all \
         "$wine" wineboot --init >/dev/null 2>&1; then
    ok "wineboot succeeded"
    [[ -e "$prefix/drive_c/windows/syswow64/d3d11.dll" ]] && \
      ok "syswow64/d3d11.dll present (32-bit d3d path wired)"
    if [[ -e "$prefix/drive_c/windows/system32/d3d12.dll" ]]; then
      local m1 m2
      m1="$(md5 -q "$prefix/drive_c/windows/system32/d3d12.dll" 2>/dev/null || echo a)"
      m2="$(md5 -q "$win64/d3d12.dll" 2>/dev/null || echo b)"
      if [[ "$m1" == "$m2" ]]; then
        ok "wineboot deployed the Apple D3DMetal d3d12.dll into system32 (md5 match)"
      else
        warn "system32/d3d12.dll md5 != overlaid d3d12.dll — wineboot deployed a different DLL"
      fi
    fi
  else
    warn "wineboot failed — inspect manually with WINEDEBUG=+loaddll"
  fi

  # 7e. patched DXMT install (Route C) — winemetal.so in x86_64-unix is the marker
  # the stock build never produces; dxgi.dll should be the multi-MB DXMT, not the
  # small D3DMetal/wined3d one.
  if [[ "$DXMT_MENU_FIX" == "1" ]]; then
    if [[ -f "$OUT_DIR/lib/wine/x86_64-unix/winemetal.so" ]]; then
      local dxgisz; dxgisz="$(stat -f%z "$OUT_DIR/lib/wine/x86_64-windows/dxgi.dll" 2>/dev/null || echo 0)"
      if [[ "$dxgisz" -gt 5000000 ]]; then
        ok "patched DXMT loaded in the builtin tree (winemetal.so + DXMT dxgi ${dxgisz}B) — WC3 menu fix active"
      else
        warn "winemetal.so present but dxgi.dll is only ${dxgisz}B — DXMT may not have installed (expected multi-MB)"
      fi
    else
      warn "DXMT_MENU_FIX=1 but lib/wine/x86_64-unix/winemetal.so missing — Route C install did not run"
    fi
  fi

  ok "capability verification complete"
  cat <<EOF

================================================================================
 BUILD READY: $OUT_DIR
   wine: $v   (Metal-backed, d3dmetal bridge present)
   lib/external: D3DMetal.framework + libd3dshared + libMoltenVK + dxmt/
   d3d11/dxgi/d3d10core: $([[ "$DXMT_MENU_FIX" == "1" ]] && echo "PATCHED DXMT (WC3 menu fix)" || echo "stock D3DMetal")

 NEXT — USER RENDER CONFIRMATION (orchestrator is screen-blind):
   1. Point Mead at this build:
        export MEAD_WINE_PATH="$OUT_DIR/bin/wine"
      (or rely on wine.go's new scripts/wine/bin/wine candidate — see plan)
   2. Launch the Battle.net CEF launcher through Mead and VISUALLY confirm a
      window renders. Reuse ~/mead-scratch/mcp.sh for the MCP launch flow.
   Only a rendered pixel proves success — symbol presence proves capability.
================================================================================
EOF
}

# ----------------------------------------------------------------------------
main() {
  preflight
  fetch_cxbuilder
  patch_cxbuilder_host_os
  patch_cxbuilder_disable_opengl
  fetch_wine_src
  apply_patches
  extract_gptk_redist
  build_wine
  build_dxmt
  overlay
  install_dxmt_route_c
  finalize_loader
  verify
}

main "$@"