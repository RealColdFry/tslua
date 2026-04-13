#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)
PATCHES_DIR="$ROOT/patches/tsgo"
DIR="$ROOT/extern/typescript-go"
BASE=$(git -C "$ROOT" ls-tree HEAD -- extern/typescript-go | awk '{print $3}')

if [ "${1:-}" = "--unapply" ]; then
    git -C "$DIR" checkout --force --detach "$BASE" --quiet
    echo "tsgo reset to upstream base $(git -C "$DIR" rev-parse --short HEAD)."
    exit 0
fi

if [ ! -d "$PATCHES_DIR" ]; then
    echo "No tsgo patches directory found, skipping."
    exit 0
fi

patches=("$PATCHES_DIR"/*.patch)
if [ ${#patches[@]} -eq 0 ]; then
    echo "No tsgo patches to apply."
    exit 0
fi

echo "Applying ${#patches[@]} tsgo patch(es)..."

# Ensure git identity exists (needed for git am in CI)
if ! git -C "$DIR" config user.name >/dev/null 2>&1; then
    git -C "$DIR" config user.name "tslua-patches"
    git -C "$DIR" config user.email "patches@localhost"
fi

# Reset to the registered submodule commit (upstream base)
git -C "$DIR" checkout --force --detach "$BASE" --quiet

for patch in "${patches[@]}"; do
    if git -C "$DIR" am --quiet "$patch" >/dev/null 2>&1; then
        echo "  applied: $(basename "$patch")"
    else
        echo "  CONFLICT: $(basename "$patch")"
        echo "  Aborting. Resolve manually or remove the patch file."
        git -C "$DIR" am --abort
        exit 1
    fi
done

echo "Done. tsgo at $(git -C "$DIR" rev-parse --short HEAD) (upstream + ${#patches[@]} patches)."
