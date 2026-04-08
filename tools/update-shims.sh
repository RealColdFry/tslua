#!/usr/bin/env -S bash -euxo pipefail

# Regenerate all shim packages from the local typescript-go source.
# Usage: ./tools/update-shims.sh
#
# This script:
# 1. Applies patches to extern/typescript-go
# 2. Updates shim go.mod files to point at the current tsgo commit
# 3. Regenerates shim.go files via gen_shims

pushd extern/typescript-go
TSGO_COMMIT="$(git rev-parse HEAD)"
git am --3way --no-gpg-sign ../../patches/tsgo/*.patch
popd

find ./shim -type f -name 'go.mod' -execdir go get -x "github.com/microsoft/typescript-go@$TSGO_COMMIT" \; -execdir go mod tidy -v \;
go mod tidy

go run ./tools/gen_shims

go build ./...
