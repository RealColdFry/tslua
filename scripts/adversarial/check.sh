#!/bin/bash
# Adversarial test harness wrapper.
# Usage: echo 'snippet' | ./scripts/adversarial/check.sh
#    or: ./scripts/adversarial/check.sh -e 'snippet'
#    or: ./scripts/adversarial/check.sh snippet.ts
set -euo pipefail
cd "$(dirname "$0")/../.."
exec node --require tsx/cjs scripts/adversarial/check.ts "$@"
