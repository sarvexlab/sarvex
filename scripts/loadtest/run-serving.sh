#!/usr/bin/env bash
# Runs the serving-plane k6 scenarios against an already-running
# `make dev-up` stack, seeded via `make loadtest-seed`.
#
# Usage: scripts/loadtest/run-serving.sh [scenario]
#   scenario: smoke | catalog_read | checkout_flow | checkout_negative | all (default)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

if ! command -v k6 >/dev/null 2>&1; then
	echo "k6 is required — https://k6.io/docs/get-started/installation/" >&2
	exit 1
fi
if [ ! -f loadtest/.manifest.json ]; then
	echo "no loadtest/.manifest.json found — run \`make loadtest-seed\` first" >&2
	exit 1
fi

SCENARIO="${1:-all}"

run() {
	echo "── serving: $1 ──────────────────────────────────────────────"
	k6 run "loadtest/serving/$1.js"
}

case "$SCENARIO" in
smoke | catalog_read | checkout_flow | checkout_negative)
	run "$SCENARIO"
	;;
all)
	run smoke
	run catalog_read
	run checkout_flow
	run checkout_negative
	;;
*)
	echo "unknown scenario: $SCENARIO" >&2
	exit 1
	;;
esac
