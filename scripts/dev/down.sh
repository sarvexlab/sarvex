#!/usr/bin/env bash
# Stops the processes started by up.sh (sim serve, control plane, sync
# plane, serving plane) or, if that's what's running instead, by
# up-monolith.sh (sim serve, one sarvex --service=all process). Never
# touches the Mongo/Redis/NATS/Vault/otel-collector Docker containers —
# they're cheap to leave running and keep their data; stop them with
# `docker compose -f deployments/docker-compose.yaml down` (add `-v` to
# also delete their volumes).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

stop() {
	local name="$1" pidfile="$DEVLOGS/$1.pid"
	if ! pid_alive "$pidfile"; then
		echo "  • $name not running"
		rm -f "$pidfile"
		return 0
	fi
	local pid
	pid="$(cat "$pidfile")"
	kill "$pid" 2>/dev/null || true
	# The control/sync entry points give themselves up to a 10s graceful
	# shutdown budget (echo's e.Shutdown context) — wait a little past that
	# before falling back to SIGKILL.
	for _ in $(seq 1 24); do
		kill -0 "$pid" 2>/dev/null || break
		sleep 0.5
	done
	if kill -0 "$pid" 2>/dev/null; then
		echo "  • $name (pid $pid) didn't exit, killing"
		kill -9 "$pid" 2>/dev/null || true
	fi
	rm -f "$pidfile"
	echo "  • $name stopped"
}

echo "── Stopping dev stack ────────────────────────────────────────────"
# monolith.pid only exists if up-monolith.sh was used instead of up.sh;
# stop() no-ops cleanly when a pidfile is absent, so it's safe to always
# try both — whichever topology is actually running gets torn down.
stop monolith
stop serving
stop sync
stop control
stop sim
echo "  (Mongo/Redis/NATS/Vault/otel-collector containers left running — see script header to stop them)"
