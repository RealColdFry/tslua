#!/usr/bin/env bash
set -euo pipefail
shopt -s nullglob

ROOT=$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)
PATCHES_DIR="$ROOT/extern/tstl-patches"
DIR="$ROOT/extern/tstl"
BASE=$(git -C "$ROOT" ls-tree HEAD -- extern/tstl | awk '{print $3}')

# Ensure git identity exists (needed for git am in CI)
if ! git -C "$DIR" config user.name >/dev/null 2>&1; then
    git -C "$DIR" config user.name "tslua-patches"
    git -C "$DIR" config user.email "patches@localhost"
fi

# Always reset to the registered submodule commit (upstream base) first.
git -C "$DIR" checkout --force --detach "$BASE" --quiet

if [ "${1:-}" = "--unapply" ]; then
    echo "TSTL reset to upstream base $(git -C "$DIR" rev-parse --short HEAD)."
    exit 0
fi

patches=()
if [ -d "$PATCHES_DIR" ]; then
    patches=("$PATCHES_DIR"/*.patch)
fi

if [ ${#patches[@]} -eq 0 ]; then
    echo "No TSTL patches to apply. TSTL at $(git -C "$DIR" rev-parse --short HEAD) (upstream base)."
    exit 0
fi

echo "Applying ${#patches[@]} TSTL patch(es)..."

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

echo "Done. TSTL at $(git -C "$DIR" rev-parse --short HEAD) (upstream + ${#patches[@]} patches)."
