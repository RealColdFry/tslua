#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)

bold='\033[1m'
red='\033[31m'
green='\033[32m'
yellow='\033[33m'
cyan='\033[36m'
reset='\033[0m'

check_tsgolint() {
    local dir="$ROOT/extern/tsgolint"
    local current=$(git -C "$ROOT" ls-tree HEAD extern/tsgolint | awk '{print $3}')
    git -C "$dir" fetch --quiet origin

    local latest=$(git -C "$dir" rev-parse origin/main 2>/dev/null || git -C "$dir" rev-parse origin/master)
    local current_short=$(git -C "$dir" rev-parse --short "$current")
    local latest_short=$(git -C "$dir" rev-parse --short "$latest")
    local current_desc=$(git -C "$dir" describe --tags "$current" 2>/dev/null || echo "$current_short")

    printf "${bold}tsgolint${reset} (oxc-project/tsgolint)\n"
    printf "  pinned:  %s\n" "$current_desc"

    if [ "$current" = "$latest" ]; then
        printf "  ${green}up to date${reset}\n"
    else
        local behind=$(git -C "$dir" rev-list --count "$current".."$latest")
        local latest_desc=$(git -C "$dir" describe --tags "$latest" 2>/dev/null || echo "$latest_short")
        printf "  latest:  %s\n" "$latest_desc"
        printf "  ${yellow}%d commit(s) behind${reset}\n" "$behind"
        printf "  new upstream commits:\n"
        git -C "$dir" log --oneline --no-decorate "$current".."$latest" | sed 's/^/    /'
        printf "  ${cyan}run:${reset} git -C extern/tsgolint diff %s..%s\n" "$current_short" "$latest_short"
    fi

    # Check inner typescript-go submodule
    check_tsgo "$dir" "$current" "$latest"
}

check_tsgo() {
    local tsgolint_dir="$1"
    local tsgolint_pinned="$2"
    local tsgolint_latest="$3"
    local tsgo_dir="$tsgolint_dir/typescript-go"

    # Get the typescript-go commit pinned by our tsgolint commit
    local tsgo_current=$(git -C "$tsgolint_dir" ls-tree "$tsgolint_pinned" typescript-go | awk '{print $3}')
    if [ -z "$tsgo_current" ]; then
        return
    fi

    git -C "$tsgo_dir" fetch --quiet origin 2>/dev/null || return

    local tsgo_current_short=$(git -C "$tsgo_dir" rev-parse --short "$tsgo_current")

    # Get typescript-go commit that latest tsgolint points to
    local tsgo_in_latest=$(git -C "$tsgolint_dir" ls-tree "$tsgolint_latest" typescript-go 2>/dev/null | awk '{print $3}')
    # Also check latest upstream typescript-go main
    local tsgo_upstream=$(git -C "$tsgo_dir" rev-parse origin/main 2>/dev/null)

    printf "\n${bold}  typescript-go${reset} (microsoft/typescript-go)\n"
    local tsgo_current_desc=$(git -C "$tsgo_dir" describe --tags "$tsgo_current" 2>/dev/null || echo "$tsgo_current_short")
    printf "    pinned:  %s\n" "$tsgo_current_desc"

    # Behind tsgolint latest
    if [ -n "$tsgo_in_latest" ] && [ "$tsgo_current" != "$tsgo_in_latest" ]; then
        local behind_tsgolint=$(git -C "$tsgo_dir" rev-list --count "$tsgo_current".."$tsgo_in_latest" 2>/dev/null || echo "?")
        local tsgo_in_latest_desc=$(git -C "$tsgo_dir" describe --tags "$tsgo_in_latest" 2>/dev/null || git -C "$tsgo_dir" rev-parse --short "$tsgo_in_latest")
        local tsgo_in_latest_short=$(git -C "$tsgo_dir" rev-parse --short "$tsgo_in_latest")
        printf "    in latest tsgolint: %s\n" "$tsgo_in_latest_desc"
        printf "    ${yellow}%s commit(s) behind latest tsgolint${reset}\n" "$behind_tsgolint"
        git -C "$tsgo_dir" log --oneline --no-decorate "$tsgo_current".."$tsgo_in_latest" 2>/dev/null | sed 's/^/      /'
        printf "    ${cyan}run:${reset} git -C extern/tsgolint/typescript-go diff %s..%s\n" "$tsgo_current_short" "$tsgo_in_latest_short"
    elif [ -n "$tsgo_in_latest" ]; then
        printf "    ${green}up to date with latest tsgolint${reset}\n"
    fi

    # Behind upstream main
    if [ -n "$tsgo_upstream" ] && [ "$tsgo_current" != "$tsgo_upstream" ]; then
        local behind_upstream=$(git -C "$tsgo_dir" rev-list --count "$tsgo_current".."$tsgo_upstream" 2>/dev/null || echo "?")
        local tsgo_upstream_short=$(git -C "$tsgo_dir" rev-parse --short "$tsgo_upstream")
        printf "    ${yellow}%s commit(s) behind upstream main${reset}\n" "$behind_upstream"
        printf "    ${cyan}run:${reset} git -C extern/tsgolint/typescript-go log --oneline %s..%s\n" "$tsgo_current_short" "$tsgo_upstream_short"
    elif [ -n "$tsgo_upstream" ]; then
        printf "    ${green}up to date with upstream main${reset}\n"
    fi
}

check_tstl() {
    local dir="$ROOT/extern/tstl"
    local patches="$ROOT/extern/tstl-patches"
    local current=$(git -C "$ROOT" ls-tree HEAD extern/tstl | awk '{print $3}')
    git -C "$dir" fetch --quiet origin

    local latest=$(git -C "$dir" rev-parse origin/master)
    local current_short=$(git -C "$dir" rev-parse --short "$current")

    printf "${bold}tstl${reset} (TypeScriptToLua/TypeScriptToLua)\n"
    printf "  pinned:  %s\n" "$current_short"

    if [ "$current" = "$latest" ]; then
        printf "  ${green}up to date with upstream master${reset}\n"
    else
        local behind=$(git -C "$dir" rev-list --count "$current".."$latest")
        printf "  ${yellow}%d commit(s) behind upstream master${reset}\n" "$behind"
        printf "  new upstream commits:\n"
        git -C "$dir" log --oneline --no-decorate "$current".."$latest" | sed 's/^/    /'
    fi

    # Show active patches
    if [ -d "$patches" ]; then
        local patch_files=("$patches"/*.patch)
        if [ ${#patch_files[@]} -gt 0 ] && [ -e "${patch_files[0]}" ]; then
            local applied=$(git -C "$dir" rev-list --count "$current"..HEAD 2>/dev/null || echo 0)
            if [ "$applied" -eq "${#patch_files[@]}" ]; then
                printf "  ${green}%d patch(es) applied${reset}\n" "$applied"
            elif [ "$applied" -eq 0 ]; then
                printf "  ${red}%d patch(es) not applied${reset} — run ./scripts/apply-tstl-patches.sh\n" "${#patch_files[@]}"
            else
                printf "  ${yellow}%d/%d patch(es) applied${reset} — run ./scripts/apply-tstl-patches.sh\n" "$applied" "${#patch_files[@]}"
            fi
            for f in "${patch_files[@]}"; do
                printf "    %s\n" "$(basename "$f")"
            done
        fi
    fi
}

echo ""
check_tsgolint
echo ""
check_tstl
echo ""
