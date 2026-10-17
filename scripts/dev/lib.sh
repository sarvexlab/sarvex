#!/usr/bin/env bash
# Shared config/helpers for scripts/dev/*.sh. Not meant to be run directly.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEVBIN="$ROOT_DIR/.devbin"
DEVLOGS="$ROOT_DIR/.devlogs"
DEV_ENV_FILE="$DEVBIN/dev.env"

CONTROL_PORT=8081
SYNC_PORT=8082
SIM_PORT=4106
SERVING_PORT=8083

# The backing services come from deployments/docker-compose.yaml — the same
# Mongo, Redis, NATS JetStream and Vault a self-hosted deployment runs.
COMPOSE_FILE="$ROOT_DIR/deployments/docker-compose.yaml"
MONGO_CONTAINER="mongodb"
MONGO_HOST_PORT=27017
REDIS_HOST_PORT=6379
NATS_HOST_PORT=4222
VAULT_HOST_PORT=8200
OTEL_GRPC_HOST_PORT=4317
OTEL_HTTP_HOST_PORT=4318
# rs0 is a single-node replica set initiated (deployments/docker-compose.yaml,
# mongodb-rs-init) with its member host set to the docker-network name
# "mongodb" — unresolvable from these natively-run Go binaries. Without
# directConnection=true, the driver's topology discovery tries to monitor
# that advertised address and gets stuck in ReplicaSetNoPrimary forever
# ("lookup mongodb ... no such host"). directConnection skips replica-set
# member discovery and talks directly to the seed address instead.
MONGO_URI_DEFAULT="mongodb://admin:password@localhost:${MONGO_HOST_PORT}/?directConnection=true"
REDIS_ADDR_DEFAULT="localhost:${REDIS_HOST_PORT}"
NATS_URL_DEFAULT="nats://localhost:${NATS_HOST_PORT}"
VAULT_ADDR_DEFAULT="http://localhost:${VAULT_HOST_PORT}"
# Local-only root token and Transit key created by deployments/vault/local-entrypoint.sh.
VAULT_TOKEN_DEFAULT="sarvex-dev-token"
VAULT_TRANSIT_KEY_NAME_DEFAULT="sarvex-jwk-signing"
# Native processes (not containers), so localhost — not a compose service name.
OTEL_EXPORTER_OTLP_ENDPOINT_DEFAULT="http://localhost:${OTEL_GRPC_HOST_PORT}"

mkdir -p "$DEVBIN" "$DEVLOGS"

# tcp_open HOST PORT — true if something is accepting TCP connections there.
tcp_open() {
	(exec 3<>"/dev/tcp/$1/$2") 2>/dev/null && exec 3>&- 3<&-
}

# wait_tcp HOST PORT LABEL TIMEOUT_SECS — polls until tcp_open succeeds or times out.
wait_tcp() {
	local host="$1" port="$2" label="$3" timeout="${4:-20}"
	local waited=0
	until tcp_open "$host" "$port"; do
		sleep 1
		waited=$((waited + 1))
		if [ "$waited" -ge "$timeout" ]; then
			echo "  ✗ $label did not open $host:$port within ${timeout}s" >&2
			return 1
		fi
	done
	return 0
}

# pid_alive PIDFILE — true if the PID file exists and names a live process.
pid_alive() {
	local pidfile="$1"
	[ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null
}

# compose ARGS... — docker compose against the shared compose file.
compose() {
	docker compose -f "$COMPOSE_FILE" "$@"
}

# wait_healthy SERVICE TIMEOUT_SECS — polls a compose service's healthcheck.
wait_healthy() {
	local svc="$1" timeout="${2:-60}" waited=0 id status
	while :; do
		id="$(compose ps -q "$svc" 2>/dev/null || true)"
		status=""
		if [ -n "$id" ]; then
			status="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null || true)"
		fi
		case "$status" in healthy | running) return 0 ;; esac
		sleep 1
		waited=$((waited + 1))
		if [ "$waited" -ge "$timeout" ]; then
			echo "  ✗ $svc not healthy within ${timeout}s (status: ${status:-none})" >&2
			return 1
		fi
	done
}

# wait_mongo_primary TIMEOUT_SECS — polls until the single-node replica set
# (initiated by the mongodb-rs-init one-shot) has elected itself primary.
wait_mongo_primary() {
	local timeout="${1:-60}" waited=0
	until compose exec -T mongodb mongosh --quiet -u admin -p password --authenticationDatabase admin \
		--eval 'db.hello().isWritablePrimary' 2>/dev/null | grep -q true; do
		sleep 1
		waited=$((waited + 1))
		if [ "$waited" -ge "$timeout" ]; then
			echo "  ✗ mongo replica set has no primary after ${timeout}s" >&2
			return 1
		fi
	done
}

# write_dev_env — persists the resolved env vars a standalone `.devbin/sim`
# invocation (seed/mutate/fix/status, run later in a different terminal)
# needs to agree with the currently-running stack. Overwritten on every
# dev-up run. Use scripts/dev/sim.sh rather than sourcing this by hand.
write_dev_env() {
	cat >"$DEV_ENV_FILE" <<EOF
MONGO_CONNECTION_STRING=$MONGO_CONNECTION_STRING
REDIS_ADDRESS=$REDIS_ADDRESS
REDIS_PASSWORD=$REDIS_PASSWORD
NATS_URL=$NATS_URL
VAULT_ADDR=$VAULT_ADDR
VAULT_TOKEN=$VAULT_TOKEN
VAULT_TRANSIT_KEY_NAME=$VAULT_TRANSIT_KEY_NAME
SIM_STORE_BASE_URL=$SIM_STORE_BASE_URL
SYNC_PLANE_BASE_URL=$SYNC_PLANE_BASE_URL
EOF
}
