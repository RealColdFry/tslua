#!/usr/bin/env bash
# Regenerates internal/lualib/* from tslua's own transpile of TSTL's lualib
# TypeScript source (extern/tstl/src/lualib/). No longer depends on TSTL's
# prebuilt dist/lualib/ output; tslua now owns the runtime.
#
# Outputs:
#   internal/lualib/lualib_bundle.lua          (universal, Lua 5.1+)
#   internal/lualib/lualib_bundle_50.lua       (Lua 5.0)
#   internal/lualib/features/*.lua             (per-feature bodies, universal)
#   internal/lualib/features_50/*.lua          (per-feature bodies, 5.0)
#   internal/lualib/lualib_module_info.json    (feature graph, universal)
#   internal/lualib/lualib_module_info_50.json (feature graph, 5.0)
#
# Patches (patches.lua) are folded in by BuildBundleFromSource and
# BuildFeatureDataFromSource; no post-hoc shell injection.
#
# Usage: just update-lualib
# Prerequisites: extern/tstl submodule initialized (src/lualib/ must exist).
#                lua-types is vendored via extern/tstl/node_modules; for a
#                fresh checkout run `just tstl-setup` once.

set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -d "extern/tstl/src/lualib" ]; then
    echo "error: extern/tstl/src/lualib not found (init submodules)" >&2
    exit 1
fi
if [ ! -d "extern/tstl/node_modules/lua-types" ]; then
    echo "error: extern/tstl/node_modules/lua-types not found (run 'just tstl-setup')" >&2
    exit 1
fi

echo "building tslua..."
go build -o tslua ./cmd/tslua/

echo "transpiling lualib bundles..."
./tslua lualib --luaTarget universal > internal/lualib/lualib_bundle.lua
./tslua lualib --luaTarget 5.0       > internal/lualib/lualib_bundle_50.lua

echo "  internal/lualib/lualib_bundle.lua    ($(wc -c < internal/lualib/lualib_bundle.lua) bytes)"
echo "  internal/lualib/lualib_bundle_50.lua ($(wc -c < internal/lualib/lualib_bundle_50.lua) bytes)"

echo "generating per-feature files..."
go run ./scripts/gen-lualib-features
go run ./scripts/gen-lualib-features --target 5.0

echo "done"
