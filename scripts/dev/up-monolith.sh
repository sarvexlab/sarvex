#!/usr/bin/env bash
# Starts the same local dev stack as up.sh, but with the three planes
# hosted by a single `sarvex --service=all` process instead of three
# separate ones (see cmd/root.go's All mode). Same infra, same ports, same
# routes — this only changes how many app processes are running, so
# anything that talks to :8081/:8082/:8083 (console-real, curl, sim.sh)
# works identically against either topology. Use down.sh/status.sh as
# usual; they already know how to find whichever mode is running.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

bash scripts/dev/infra-up.sh

export MONGO_CONNECTION_STRING="${MONGO_CONNECTION_STRING:-$MONGO_URI_DEFAULT}"
export REDIS_ADDRESS="${REDIS_ADDRESS:-$REDIS_ADDR_DEFAULT}"
export REDIS_PASSWORD="${REDIS_PASSWORD:-}"
export NATS_URL="${NATS_URL:-$NATS_URL_DEFAULT}"
export VAULT_ADDR="${VAULT_ADDR:-$VAULT_ADDR_DEFAULT}"
export VAULT_TOKEN="${VAULT_TOKEN:-$VAULT_TOKEN_DEFAULT}"
export VAULT_TRANSIT_KEY_NAME="${VAULT_TRANSIT_KEY_NAME:-$VAULT_TRANSIT_KEY_NAME_DEFAULT}"
export SIM_STORE_BASE_URL="http://localhost:${SIM_PORT}"
export SYNC_PLANE_BASE_URL="http://localhost:${SYNC_PORT}/v1"
export SERVING_BASE_URL="http://localhost:${SERVING_PORT}/ucp"
export VALIDATION_TEST_HARNESS_BASE_URL="http://localhost:${SERVING_PORT}"
export CONTROL_BASE_URL="http://localhost:${CONTROL_PORT}"
export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-$OTEL_EXPORTER_OTLP_ENDPOINT_DEFAULT}"
unset KINDE_ISSUER_URL
write_dev_env

echo "── Building binaries ─────────────────────────────────────────────"
go build -o "$DEVBIN/sarvex" .
go build -o "$DEVBIN/sim" ./cmd/sim
echo "  ✓ built .devbin/sarvex, .devbin/sim"

start() {
	local name="$1" pidfile="$DEVLOGS/$1.pid" logfile="$DEVLOGS/$1.log"
	shift
	if pid_alive "$pidfile"; then
		echo "  • $name already running (pid $(cat "$pidfile"))"
		return 0
	fi
	: >"$logfile"
	nohup "$@" >"$logfile" 2>&1 &
	echo $! >"$pidfile"
	echo "  • $name started (pid $(cat "$pidfile"), log: $logfile)"
}

echo "── Starting sim serve (fake WooCommerce store) ───────────────────"
start sim "$DEVBIN/sim" serve --addr ":${SIM_PORT}"
wait_tcp 127.0.0.1 "$SIM_PORT" "sim serve" 15

echo "── Starting sarvex (--service=all: serving+control+sync in one process) ──"
# --configPath is the parent of configs/{serving,control,sync} — each
# plane's entry.go loads its own configs.<env>.json from configPath/<plane
# name>, same as split-deployment does per-plane. See cmd/root.go's
# runAllPlanes.
start monolith "$DEVBIN/sarvex" --service=all --configPath=./configs --env=local \
	--controlPort="$CONTROL_PORT" --syncPort="$SYNC_PORT" --servingPort="$SERVING_PORT"
wait_tcp 127.0.0.1 "$CONTROL_PORT" "control (monolith)" 20
wait_tcp 127.0.0.1 "$SYNC_PORT" "sync (monolith)" 20
wait_tcp 127.0.0.1 "$SERVING_PORT" "serving (monolith)" 20

cat <<EOF

✓ Stack is up (monolith: one sarvex process hosting all three planes):
    sim serve      http://localhost:${SIM_PORT}   (log: .devlogs/sim.log)
    control plane  http://localhost:${CONTROL_PORT}/v1  (log: .devlogs/monolith.log)
    sync plane     http://localhost:${SYNC_PORT}/v1  (log: .devlogs/monolith.log)
    serving plane  http://localhost:${SERVING_PORT}/ucp  (log: .devlogs/monolith.log)
    otel-collector OTLP gRPC :${OTEL_GRPC_HOST_PORT}, HTTP :${OTEL_HTTP_HOST_PORT}  (docker compose -f deployments/docker-compose.yaml logs otel-collector)

Next steps: same as \`make dev-up\` — see there, or scripts/dev/up.sh. The
ports are identical, so the walkthrough (dev-token, creating a store,
sim.sh seed, pairing) works unchanged against the monolith.

Stop everything: scripts/dev/down.sh (or \`make dev-down\`)
Check status:    scripts/dev/status.sh (or \`make dev-status\`)
EOF
