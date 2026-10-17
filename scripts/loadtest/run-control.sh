#!/usr/bin/env bash
# Runs the control-plane k6 scenarios against an already-running
# `make dev-up` stack, seeded via `make loadtest-seed`.
#
# Usage: scripts/loadtest/run-control.sh [scenario]
#   scenario: smoke | console_read | catalog_import | websocket_journey | all (default)
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
	echo "── control: $1 ──────────────────────────────────────────────"
	k6 run "loadtest/control/$1.js"
}

case "$SCENARIO" in
smoke | console_read | catalog_import | websocket_journey)
	run "$SCENARIO"
	;;
all)
	run smoke
	run console_read
	run catalog_import
	run websocket_journey
	;;
*)
	echo "unknown scenario: $SCENARIO" >&2
	exit 1
	;;
esac
