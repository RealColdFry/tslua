#!/usr/bin/env bash
set -euo pipefail

err=0

check_required() {
    if ! command -v "$1" &>/dev/null; then
        echo "ERROR: $1 is required but not found"
        err=1
    else
        echo "  ok: $1 ($(command -v "$1"))"
    fi
}

check_optional() {
    if ! command -v "$1" &>/dev/null; then
        echo "  WARN: $1 not found — needed for $2"
    else
        echo "  ok: $1 ($(command -v "$1"))"
    fi
}

echo "Checking dependencies..."
check_required go
check_required node
check_required npm
check_optional luajit "just tstltest / tstl-test"
check_optional lua5.1 "just tstltest with Lua 5.1 target"
check_optional goimports "just fmt"
check_optional golangci-lint "just lint"

if [ "$err" -ne 0 ]; then
    echo ""
    echo "Install missing required tools before continuing."
    exit 1
fi

echo ""
echo "Initializing submodules..."
git submodule update --init extern/typescript-go
git submodule update --init extern/tstl

echo ""
echo "Applying tsgo patches..."
./scripts/apply-tsgo-patches.sh

echo ""
echo "Applying TSTL patches..."
./scripts/apply-tstl-patches.sh

echo ""
echo "Installing npm dependencies..."
npm install

echo ""
echo "Setting up TSTL test suite..."
just tstl-setup

echo ""
echo "Done! Try: just build"
