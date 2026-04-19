#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="$SCRIPT_DIR/lua-versions.txt"
INSTALL_DIR="$ROOT_DIR/.lua-runtimes"
BIN_DIR="$INSTALL_DIR/bin"
BUILD_DIR="$INSTALL_DIR/build"

detect_platform() {
    case "$(uname -s)" in
        Darwin*) echo "macosx" ;;
        Linux*)  echo "linux" ;;
        *)       echo "posix" ;;
    esac
}

human_size() {
    local bytes="$1"
    if (( bytes >= 1048576 )); then
        echo "$(( bytes / 1048576 ))M"
    elif (( bytes >= 1024 )); then
        echo "$(( bytes / 1024 ))K"
    else
        echo "${bytes}B"
    fi
}

now() { perl -MTime::HiRes=time -e 'printf "%f\n", time()'; }
elapsed_since() { perl -e "printf '%.1f', $(now) - $1"; }

# Run a command, suppress output on success, show it on failure.
run_quiet() {
    local log="$BUILD_DIR/build.log"
    if "$@" > "$log" 2>&1; then
        return 0
    else
        local rc=$?
        echo "  BUILD FAILED (exit $rc), output:"
        cat "$log"
        return $rc
    fi
}

PLATFORM="$(detect_platform)"
NJOBS="$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)"

mkdir -p "$BIN_DIR" "$BUILD_DIR"

# Quick check: skip entirely if all binaries exist
all_exist=true
while IFS=$' \t' read -r name _version _source; do
    [[ "$name" =~ ^#.*$ || -z "$name" ]] && continue
    if [[ ! -x "$BIN_DIR/$name" ]]; then
        all_exist=false
        break
    fi
done < "$MANIFEST"

if $all_exist; then
    echo "All Lua runtimes already built in $BIN_DIR"
    exit 0
fi

build_lua() {
    local name="$1" version="$2" url="$3"
    local src_dir="$BUILD_DIR/$name"
    local start_time

    if [[ -x "$BIN_DIR/$name" ]]; then
        local size; size="$(wc -c < "$BIN_DIR/$name")"
        echo "  $name ($version): already built ($(human_size "$size")), skipping"
        return
    fi

    start_time="$(now)"

    rm -rf "$src_dir"
    mkdir -p "$src_dir"
    curl -sL "$url" | tar xz --strip-components=1 -C "$src_dir"

    cd "$src_dir"

    if [[ "$version" == "5.0."* ]]; then
        # Lua 5.0 uses a different build system: edit config then make
        if [[ "$PLATFORM" == "linux" ]]; then
            sed -i 's/^#LOADLIB=.*/LOADLIB=-ldl/' config
            sed -i 's/^EXTRA_LIBS=.*/EXTRA_LIBS=-lm -ldl/' config
        fi
        run_quiet make -j"$NJOBS"
        cp bin/lua "$BIN_DIR/$name"
    else
        run_quiet make -j"$NJOBS" "$PLATFORM"
        cp src/lua "$BIN_DIR/$name"
    fi

    local size; size="$(wc -c < "$BIN_DIR/$name")"
    local reported; reported="$("$BIN_DIR/$name" -v 2>&1 | head -1)"
    echo "  $name ($version): done ($(elapsed_since "$start_time")s, $(human_size "$size")) [$reported]"
}

build_luajit() {
    local name="$1" version="$2" url="$3"
    local src_dir="$BUILD_DIR/$name"
    local start_time

    if [[ -x "$BIN_DIR/$name" ]]; then
        local size; size="$(wc -c < "$BIN_DIR/$name")"
        echo "  $name ($version): already built ($(human_size "$size")), skipping"
        return
    fi

    start_time="$(now)"

    rm -rf "$src_dir"
    mkdir -p "$src_dir"
    curl -sL "$url" | tar xz --strip-components=1 -C "$src_dir"

    cd "$src_dir"
    if [[ "$PLATFORM" == "macosx" ]]; then
        export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-11.0}"
    fi
    run_quiet make -j"$NJOBS"
    cp src/luajit "$BIN_DIR/$name"

    local size; size="$(wc -c < "$BIN_DIR/$name")"
    local reported; reported="$("$BIN_DIR/$name" -v 2>&1 | head -1)"
    echo "  $name ($version): done ($(elapsed_since "$start_time")s, $(human_size "$size")) [$reported]"
}

download_lune() {
    local name="$1" version="$2" url_template="$3"
    local start_time

    if [[ -x "$BIN_DIR/$name" ]]; then
        local size; size="$(wc -c < "$BIN_DIR/$name")"
        echo "  $name ($version): already installed ($(human_size "$size")), skipping"
        return
    fi

    start_time="$(now)"

    # Resolve platform-specific URL
    local arch; arch="$(uname -m)"
    case "$arch" in
        arm64) arch="aarch64" ;;
    esac
    local os_name
    case "$(uname -s)" in
        Darwin*) os_name="macos" ;;
        Linux*)  os_name="linux" ;;
        *)       echo "  $name: unsupported platform"; return 1 ;;
    esac
    local url="${url_template/\{os\}/$os_name}"
    url="${url/\{arch\}/$arch}"

    local zip_file="$BUILD_DIR/lune.zip"
    curl -sL "$url" -o "$zip_file"
    unzip -o -q "$zip_file" lune -d "$BIN_DIR"
    chmod +x "$BIN_DIR/lune"
    rm -f "$zip_file"

    local size; size="$(wc -c < "$BIN_DIR/$name")"
    local reported; reported="$("$BIN_DIR/$name" --version 2>&1 | head -1)"
    echo "  $name ($version): done ($(elapsed_since "$start_time")s, $(human_size "$size")) [$reported]"
}

echo "Building Lua runtimes -> $BIN_DIR"
echo "Platform: $PLATFORM"
echo ""

while IFS=$' \t' read -r name version source; do
    [[ "$name" =~ ^#.*$ || -z "$name" ]] && continue

    if [[ "$name" == "luajit" ]]; then
        build_luajit "$name" "$version" "$source"
    elif [[ "$name" == "lune" ]]; then
        download_lune "$name" "$version" "$source"
    else
        build_lua "$name" "$version" "$source"
    fi
done < "$MANIFEST"

echo ""
echo "Done. Add to PATH:"
echo "  export PATH=\"$BIN_DIR:\$PATH\""
