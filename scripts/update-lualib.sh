#!/usr/bin/env bash
# Copies TSTL's built lualib bundles into internal/lualib/ and applies
# tslua-specific patches (custom Lua functions not expressible in TS).
# Also generates per-feature .lua files and module info for selective inlining.
#
# Usage: just update-lualib
# Prerequisites: extern/tstl must be built (just tstl-setup)

set -euo pipefail

cd "$(dirname "$0")/.."

TSTL_LUALIB="extern/tstl/dist/lualib"
TARGET="internal/lualib"
PATCHES="$TARGET/patches.lua"

# Extract export names from patches.lua (lines like "local function __TS__Foo(")
patch_exports() {
    grep '^local function __TS__' "$PATCHES" | sed 's/local function \(__TS__[A-Za-z0-9_]*\).*/\1/' | sort -u
}

# Apply patches to a bundle: insert functions before "return {" and add exports
strip_lua_comments() {
    local file="$1"
    # Remove pure comment lines (-- ...) and collapse resulting blank lines
    sed -i '/^[[:space:]]*--/d' "$file"
    sed -i '/^$/N;/^\n$/d' "$file"
}

apply_patches() {
    local bundle="$1"
    local tmp
    tmp=$(mktemp)

    # Build export lines for the return table
    local exports=""
    for name in $(patch_exports); do
        exports="$exports  $name = $name,\n"
    done

    # 1. Insert patch functions before "return {"
    # 2. Insert export entries after "return {"
    awk -v patches="$PATCHES" -v exports="$exports" '
        /^return \{/ {
            # Insert patch file contents before return
            while ((getline line < patches) > 0) print line
            close(patches)
            print ""
            print $0
            # Insert exports after "return {"
            printf "%s", exports
            next
        }
        { print }
    ' "$bundle" > "$tmp"

    mv "$tmp" "$bundle"
}

# Apply patches to per-feature data: copy patches.lua as a feature file
# and add its entries to the module info JSON.
apply_patches_to_features() {
    local features_dir="$1"
    local info_file="$2"

    # Map iterators depend on the Map/Set features (they access internal structure)
    cp "$PATCHES" "$features_dir/TsluaIterators.lua"

    # Build the patch feature JSON entry
    local exports=""
    for name in $(patch_exports); do
        if [ -n "$exports" ]; then
            exports="$exports,\"$name\""
        else
            exports="\"$name\""
        fi
    done

    # Insert the patch feature entry into the module info JSON (before the closing })
    local tmp
    tmp=$(mktemp)
    sed '$d' "$info_file" > "$tmp"  # remove closing }
    # Ensure trailing comma on last real entry
    sed -i '' '$ s/}$/},/' "$tmp" 2>/dev/null || sed -i '$ s/}$/},/' "$tmp"
    echo "  \"TsluaIterators\": {\"exports\":[$exports],\"dependencies\":[\"Map\",\"Set\"]}" >> "$tmp"
    echo "}" >> "$tmp"
    mv "$tmp" "$info_file"
}

# --- Full bundles (for require mode) ---

if [ ! -f "$TSTL_LUALIB/universal/lualib_bundle.lua" ]; then
    echo "error: $TSTL_LUALIB/universal/lualib_bundle.lua not found. Run 'just tstl-setup' first." >&2
    exit 1
fi
cp "$TSTL_LUALIB/universal/lualib_bundle.lua" "$TARGET/lualib_bundle.lua"
apply_patches "$TARGET/lualib_bundle.lua"
strip_lua_comments "$TARGET/lualib_bundle.lua"
echo "  updated $TARGET/lualib_bundle.lua"

if [ -f "$TSTL_LUALIB/5.0/lualib_bundle.lua" ]; then
    cp "$TSTL_LUALIB/5.0/lualib_bundle.lua" "$TARGET/lualib_bundle_50.lua"
    apply_patches "$TARGET/lualib_bundle_50.lua"
    strip_lua_comments "$TARGET/lualib_bundle_50.lua"
    echo "  updated $TARGET/lualib_bundle_50.lua"
fi

# --- Per-feature files (for inline mode) ---

echo "  generating per-feature files..."
# Create placeholder dirs so //go:embed doesn't fail during compilation
mkdir -p "$TARGET/features" "$TARGET/features_50"
[ "$(ls -A "$TARGET/features" 2>/dev/null)" ] || echo '-- placeholder' > "$TARGET/features/placeholder.lua"
[ "$(ls -A "$TARGET/features_50" 2>/dev/null)" ] || echo '-- placeholder' > "$TARGET/features_50/placeholder.lua"
# Create placeholder JSON files if missing
[ -f "$TARGET/lualib_module_info.json" ] || echo '{}' > "$TARGET/lualib_module_info.json"
[ -f "$TARGET/lualib_module_info_50.json" ] || echo '{}' > "$TARGET/lualib_module_info_50.json"
go run ./scripts/gen-lualib-features
go run ./scripts/gen-lualib-features --target 5.0

# Apply patches as a synthetic feature
apply_patches_to_features "$TARGET/features" "$TARGET/lualib_module_info.json"
apply_patches_to_features "$TARGET/features_50" "$TARGET/lualib_module_info_50.json"
echo "  updated per-feature files"

echo "done"
