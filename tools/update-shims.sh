#!/usr/bin/env -S bash -euxo pipefail

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
pushd extern/typescript-go
git checkout "$(git -C ../.. rev-parse HEAD:extern/typescript-go)"
TSGO_COMMIT="$(git rev-parse HEAD)"
if ls ../../patches/tsgo/*.patch &>/dev/null; then
    git am --3way --no-gpg-sign ../../patches/tsgo/*.patch
fi
popd

find ./shim -type f -name 'go.mod' -execdir go get -x "github.com/microsoft/typescript-go@$TSGO_COMMIT" \; -execdir go mod tidy -v \;
go mod tidy

go run ./tools/gen_shims

go build ./...
