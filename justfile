import? 'justfile.local'

set positional-arguments := true

export PATH := absolute_path(".lua-runtimes/bin") + ":" + env("PATH")

# tslua development commands

# First-time setup: check deps, init submodules, install packages
setup:
    ./scripts/setup.sh

# Build Lua runtimes from source (cached in .lua-runtimes/)
lua-setup:
    ./scripts/build-lua.sh

# Build the tslua binary
build:
    go build -ldflags="-s -w" -o tslua ./cmd/tslua/

# Build WASM and copy to website/src/assets/wasm (Vite fingerprints these)
wasm:
    mkdir -p website/src/assets/wasm
    GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o website/src/assets/wasm/tslua.wasm ./cmd/wasm/
    cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" website/src/assets/wasm/

# Run tslua on a project (pass args after --)
run *ARGS: build
    ./tslua {{ ARGS }}

# Compare tslua vs TSTL: AST, codegen, eval
compare *ARGS: build
    @node --require tsx/cjs scripts/compare.ts "$@"

# Quick smoke test: run Go unit tests
test:
    gotestsum --format short ./internal/transpiler/ ./internal/lua/...

# Run Go unit tests (fast, no external deps)
gotest:
    gotestsum --format short ./internal/transpiler/ ./internal/lua/...

luatest *ARGS:
    gotestsum --format short ./internal/luatest/ {{ ARGS }}

# Generate migrated TSTL tests (if missing)
[private]
tstlgen:
    @if [ ! -f internal/tstltest/loops_test.go ]; then echo "Generating test files..."; npm run migrate; fi

# Run migrated TSTL tests (eval + codegen)
# Usage: just tstltest
#        just tstltest -run TestEval_        (runtime only)
#        just tstltest -run TestCodegen_     (codegen only)
#        just tstltest -run _Loops           (one suite, both modes)

# just tstltest -run TestEval_Loops   (one suite, eval only)
tstltest *ARGS: tstlgen
    FORCE_COLOR=1 gotestsum --format short ./internal/tstltest/ {{ ARGS }}

# Run migrated TSTL eval tests with optimized emit mode
tstltest-optimized *ARGS: tstlgen
    FORCE_COLOR=1 gotestsum --format short ./internal/tstltest/ -run TestEval_ -emit-mode=optimized {{ ARGS }}

# Run Lua runtime benchmarks (time + memory, tstl vs optimized)
bench *ARGS:
    go run ./cmd/luabench/ {{ ARGS }}

# Run benchmarks with transpiled Lua output
bench-lua:
    go run ./cmd/luabench/ --show-lua

# Run all 100%-passing Go tests
testall: tstlgen
    FORCE_COLOR=1 gotestsum --format short \
        ./internal/transpiler/ \
        ./internal/lua/... \
        ./internal/luatest/ \
        ./internal/resolve/ \
        ./internal/tstltest/ -skip TestCodegen_

# Run tests with coverage profiling
coverage:
    gotestsum --format short -- \
        -coverpkg=./internal/transpiler/,./internal/lua/,./internal/lualib/,./internal/lualibinfo/,./internal/sourcemap/ \
        -coverprofile=coverage.out \
        -covermode=atomic \
        ./internal/transpiler/ ./internal/lua/... ./internal/luatest/ ./internal/tstltest/ \
        -skip TestCodegen_
    go tool cover -func=coverage.out | grep '^total:'

# Format code (Go + TS)
fmt:
    goimports -w cmd/ internal/
    npx oxfmt scripts/ website/

# Check formatting without modifying files
fmt-check:
    goimports -l cmd/ internal/ | grep . && exit 1 || true
    npx oxfmt --check scripts/ website/

# Lint code (Go + TS)
lint:
    golangci-lint run
    npm run typecheck
    cd website && npx astro check
    npx oxlint scripts/ website/

# Migrate TSTL spec file(s) to Go tests
# Usage: just migrate extern/tstl/test/unit/builtins/math.spec.ts

# just migrate-all    (regenerate all existing test files)
migrate SPEC:
    node --require tsx/cjs scripts/migrate/cli.ts {{ SPEC }}

# Regenerate all migrated TSTL tests
migrate-all:
    node --require tsx/cjs scripts/migrate/cli.ts

# Copy TSTL's built lualib bundles and apply tslua patches
update-lualib:
    ./scripts/update-lualib.sh

# Setup TSTL test suite (one-time)
tstl-setup:
    #!/usr/bin/env bash
    set -euo pipefail
    cd extern/tstl && npm ci && npm run build
    cd "{{ justfile_directory() }}"
    # tsver=$(node -p "require('./extern/tstl/package.json').devDependencies.typescript")
    # npm pkg set "devDependencies.typescript=$tsver"
    # npm install

# Run TSTL tests against tslua (applies patch, starts server, runs, cleans up)
tstl-test *ARGS: build
    #!/usr/bin/env bash
    set -e
    ROOT="{{ justfile_directory() }}"
    go build -o "$ROOT/tslua-client" "$ROOT/cmd/tslua-client/"
    cd "$ROOT/extern/tstl" && git apply "$ROOT/extern/tstl-test-util.patch"
    mkdir -p "$ROOT/tmp"
    SOCKET="$ROOT/tmp/tslua-jest.sock"
    rm -f "$SOCKET"
    "$ROOT/tslua" server --socket "$SOCKET" &
    SERVER_PID=$!
    trap 'kill $SERVER_PID 2>/dev/null; rm -f "$SOCKET"; cd "$ROOT/extern/tstl" && git checkout -- test/util.ts' EXIT
    for i in $(seq 1 50); do [ -S "$SOCKET" ] && break; sleep 0.1; done
    WORKERS=$(( $(getconf _NPROCESSORS_ONLN) / 2 ))
    [ "$WORKERS" -lt 1 ] && WORKERS=1
    cd "$ROOT/extern/tstl" && TSTL_GO=1 npx jest --no-coverage --maxWorkers="$WORKERS" {{ ARGS }} || true

# Run TSTL tests against tslua, save verbose output to file
tstl-test-save FILE *ARGS: build
    #!/usr/bin/env bash
    set -e
    ROOT="{{ justfile_directory() }}"
    go build -o "$ROOT/tslua-client" "$ROOT/cmd/tslua-client/"
    cd "$ROOT/extern/tstl" && git apply "$ROOT/extern/tstl-test-util.patch"
    mkdir -p "$ROOT/tmp"
    SOCKET="$ROOT/tmp/tslua-jest.sock"
    rm -f "$SOCKET"
    "$ROOT/tslua" server --socket "$SOCKET" &
    SERVER_PID=$!
    trap 'kill $SERVER_PID 2>/dev/null; rm -f "$SOCKET"; cd "$ROOT/extern/tstl" && git checkout -- test/util.ts' EXIT
    for i in $(seq 1 50); do [ -S "$SOCKET" ] && break; sleep 0.1; done
    WORKERS=$(( $(getconf _NPROCESSORS_ONLN) / 2 ))
    [ "$WORKERS" -lt 1 ] && WORKERS=1
    cd "$ROOT/extern/tstl" && TSTL_GO=1 npx jest --no-coverage --verbose --maxWorkers="$WORKERS" {{ ARGS }} 2>&1 | tee "$ROOT/{{ FILE }}" || true

# Run TSTL Jest suite with native TSTL (no tslua)
tstl-jest *ARGS:
    cd extern/tstl && npx jest --no-coverage {{ ARGS }}

# Check if submodules can be bumped
bump-check:
    ./scripts/check-submodule-bumps.sh

# Bump extern/tstl submodule to latest upstream master
tstl-bump:
    cd extern/tstl && git fetch origin && git checkout origin/master

# Bump extern/typescript-go submodule to latest upstream main
tsgo-bump:
    cd extern/typescript-go && git fetch origin && git checkout origin/main

# Regenerate shim/ from extern/typescript-go (applies patches, runs gen_shims)
shim:
    ./tools/update-shims.sh

# Reset extern/tstl to the committed submodule pointer (discards patches/local changes)
tstl-reset:
    git submodule update -- extern/tstl

# Apply TSTL patches (unmerged PRs) on top of submodule
tstl-patches:
    ./scripts/apply-tstl-patches.sh

# Apply GitHub ruleset from .github/rulesets/protect-main.json
ruleset action="sync":
    #!/usr/bin/env bash
    set -euo pipefail
    repo="RealColdFry/tslua"
    file=".github/rulesets/protect-main.json"
    if [[ "{{ action }}" == "sync" ]]; then
        id=$(gh api "repos/$repo/rulesets" --jq '.[0].id // empty' 2>/dev/null || true)
        if [[ -n "$id" ]]; then
            echo "Updating ruleset $id..."
            gh api "repos/$repo/rulesets/$id" -X PUT --input "$file"
        else
            echo "Creating ruleset..."
            gh api "repos/$repo/rulesets" -X POST --input "$file"
        fi
    elif [[ "{{ action }}" == "list" ]]; then
        gh api "repos/$repo/rulesets"
    else
        echo "Usage: just ruleset [sync|list]"
        exit 1
    fi

# Show unmigrated TSTL unit test specs
unmigrated:
    #!/usr/bin/env bash
    comm -23 \
        <(find extern/tstl/test/unit -name '*.spec.ts' | sed 's|.*/||; s/\.spec\.ts//' | sort -u) \
        <(ls internal/tstltest/*_test.go | sed 's|.*/||; s/_codegen_test\.go//; s/_test\.go//' | sort -u)
