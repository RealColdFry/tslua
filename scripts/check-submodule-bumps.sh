#!/usr/bin/env bash
set -euo pipefail

ROOT=$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)

bold='\033[1m'
red='\033[31m'
green='\033[32m'
yellow='\033[33m'
cyan='\033[36m'
reset='\033[0m'

check_tsgo() {
    local dir="$ROOT/extern/typescript-go"
    local current=$(git -C "$ROOT" ls-tree HEAD extern/typescript-go | awk '{print $3}')
    git -C "$dir" fetch --quiet origin 2>/dev/null || return

    local latest=$(git -C "$dir" rev-parse origin/main 2>/dev/null)
    local current_short=$(git -C "$dir" rev-parse --short "$current")
    local latest_short=$(git -C "$dir" rev-parse --short "$latest")
    local current_desc=$(git -C "$dir" describe --tags "$current" 2>/dev/null || echo "$current_short")

    printf "${bold}typescript-go${reset} (microsoft/typescript-go)\n"
    printf "  pinned:  %s\n" "$current_desc"

    if [ "$current" = "$latest" ]; then
        printf "  ${green}up to date with upstream main${reset}\n"
    else
        local behind=$(git -C "$dir" rev-list --count "$current".."$latest" 2>/dev/null || echo "?")
        local latest_desc=$(git -C "$dir" describe --tags "$latest" 2>/dev/null || echo "$latest_short")
        printf "  latest:  %s\n" "$latest_desc"
        printf "  ${yellow}%s commit(s) behind upstream main${reset}\n" "$behind"
        git -C "$dir" log --oneline --no-decorate "$current".."$latest" | head -20 | sed 's/^/    /'
        printf "  ${cyan}run:${reset} git -C extern/typescript-go log --oneline %s..%s\n" "$current_short" "$latest_short"
    fi

    # Show active patches
    local patches_dir="$ROOT/patches/tsgo"
    if [ -d "$patches_dir" ]; then
        local patch_files=("$patches_dir"/*.patch)
        if [ ${#patch_files[@]} -gt 0 ] && [ -e "${patch_files[0]}" ]; then
            printf "  patches: %d\n" "${#patch_files[@]}"
            for f in "${patch_files[@]}"; do
                printf "    %s\n" "$(basename "$f")"
            done
        fi
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
check_tsgo
echo ""
check_tstl
echo ""
