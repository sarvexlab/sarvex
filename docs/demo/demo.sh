#!/usr/bin/env bash
# Sarvex demo: onboard a fictitious merchant, then have a shopping agent buy from it.
# Prereq: `make infra-up && make dev-up` (see README quick start).
# Overrides: MERCHANT_NAME, STORE_ID, SKU, PACE (seconds between steps, default 1).
set -euo pipefail
cd "$(dirname "$0")/../.."

MERCHANT_NAME="${MERCHANT_NAME:-Acme Widgets}"
PACE="${PACE:-1}"
bold=$'\033[1m'; dim=$'\033[2m'; green=$'\033[32m'; reset=$'\033[0m'
step() { printf '\n%s== %s ==%s\n' "$bold" "$*" "$reset"; sleep "$PACE"; }
run()  { printf '%s$ %s%s\n' "$dim" "$*" "$reset"; "$@"; }

[ -d cmd/sarvex ] || { echo "cmd/sarvex is missing from this checkout" >&2; exit 1; }
command -v go >/dev/null || { echo "go is required" >&2; exit 1; }

step "1/3  Check the local stack"
run make dev-status

step "2/3  Onboard merchant \"$MERCHANT_NAME\""
out=$(mktemp); trap 'rm -f "$out"' EXIT
run go run ./cmd/sarvex onboard merchant "$MERCHANT_NAME" | tee "$out"
STORE_ID="${STORE_ID:-$(grep -oiE 'store[_ -]?id[:= ]+[A-Za-z0-9_-]+' "$out" | head -1 | grep -oE '[A-Za-z0-9_-]+$' || true)}"
SKU="${SKU:-$(grep -oiE 'sku[:= ]+[A-Za-z0-9_-]+' "$out" | head -1 | grep -oE '[A-Za-z0-9_-]+$' || true)}"
[ -n "$STORE_ID" ] && [ -n "$SKU" ] || { echo "Could not read store id / sku from onboard output; re-run with STORE_ID=... SKU=..." >&2; exit 1; }

step "3/3  Agent checkout: store=$STORE_ID sku=$SKU"
run go run ./cmd/sarvex agent checkout run --store "$STORE_ID" --sku "$SKU"

printf '\n%sDone: agent completed a checkout against %s.%s\n' "$green" "$MERCHANT_NAME" "$reset"
