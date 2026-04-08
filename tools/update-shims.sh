#!/usr/bin/env -S bash -euo pipefail

# Regenerate all shim packages from the local typescript-go source.
# Usage: ./tools/update-shims.sh
#
# This script:
# 1. Resets extern/typescript-go to the committed submodule pointer
# 2. Applies patches on top
# 3. Updates shim go.mod files to point at the (unpatched) tsgo commit
# 4. Regenerates shim.go files via gen_shims
# 5. Verifies the build

cd "$(git rev-parse --show-toplevel)"

# Reset tsgo to the clean submodule pointer, then apply patches
echo "resetting extern/typescript-go to submodule pointer..."
cd extern/typescript-go
git checkout -q "$(git -C ../.. rev-parse HEAD:extern/typescript-go)"
TSGO_COMMIT="$(git rev-parse HEAD)"
if ls ../../patches/tsgo/*.patch &>/dev/null; then
    echo "applying patches..."
    git am --3way --no-gpg-sign -q ../../patches/tsgo/*.patch
fi
cd ../..

echo "updating shim go.mod files (tsgo @ ${TSGO_COMMIT:0:12})..."
find ./shim -type f -name 'go.mod' -execdir go get "github.com/microsoft/typescript-go@$TSGO_COMMIT" \; -execdir go mod tidy \; 2>&1 | grep -v '^go: ' || true
go mod tidy 2>&1 | grep -v '^go: ' || true

echo "generating shims..."
go run ./tools/gen_shims

echo "verifying build..."
go build ./...

echo "done."
