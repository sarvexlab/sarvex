#!/usr/bin/env bash
# Starts the full local dev stack needed to exercise Sarvex V1 end to end:
# Mongo + Redis + NATS JetStream + Vault + a local OTel Collector (via
# deployments/docker-compose.yaml, see infra-up.sh), the persistent fake WooCommerce store
# (`cmd/sim serve`), the control plane, and the sync plane — in that
# dependency order, with KINDE_ISSUER_URL left unset so each plane's
# in-process pairing/heartbeat simulator activates (dev mode).
#
# Those are the same backing services a self-hosted Kubernetes deployment
# runs — NATS for the event bus, Vault for signing keys (Transit) and
# connector credentials (KV v2) — so local dev exercises the production code
# paths and nothing is emulated.
#
# Idempotent: re-running skips anything already up. See down.sh to stop,
# status.sh to check, token.sh to mint a bearer token for curl testing.
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
# The endpoint-health probe identity signs its requests and advertises its own
# profile; the serving plane fetches that URL to verify the signature and
# negotiate. Left unset the URL is relative, negotiation fails with
# profile_unreachable, and no store can ever verify. Origin only — entry.go
# appends the /v1 group and the route path itself.
export CONTROL_BASE_URL="http://localhost:${CONTROL_PORT}"
# Consumed by internal/pkg/otelboot in each plane's entry.go Init: when set,
# it switches tracing/metrics from the default no-op providers to a real
# OTLP exporter pointed at the local collector started above.
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

echo "── Starting control plane ────────────────────────────────────────"
start control "$DEVBIN/sarvex" --service=control --configPath=./configs/control --env=local --port="$CONTROL_PORT"
wait_tcp 127.0.0.1 "$CONTROL_PORT" "control plane" 20

echo "── Starting sync plane ───────────────────────────────────────────"
start sync "$DEVBIN/sarvex" --service=sync --configPath=./configs/sync --env=local --port="$SYNC_PORT"
wait_tcp 127.0.0.1 "$SYNC_PORT" "sync plane" 20

echo "── Starting serving plane ────────────────────────────────────────"
start serving "$DEVBIN/sarvex" --service=serving --configPath=./configs/serving --env=local --port="$SERVING_PORT"
wait_tcp 127.0.0.1 "$SERVING_PORT" "serving plane" 20

cat <<EOF

✓ Stack is up:
    sim serve      http://localhost:${SIM_PORT}   (log: .devlogs/sim.log)
    control plane  http://localhost:${CONTROL_PORT}/v1  (log: .devlogs/control.log)
    sync plane     http://localhost:${SYNC_PORT}/v1  (log: .devlogs/sync.log)
    serving plane  http://localhost:${SERVING_PORT}/ucp  (log: .devlogs/serving.log)
    otel-collector OTLP gRPC :${OTEL_GRPC_HOST_PORT}, HTTP :${OTEL_HTTP_HOST_PORT}  (docker compose -f deployments/docker-compose.yaml logs otel-collector)

Next steps:
  1. Create a merchant (staff-only; it also starts the entry-level trial that
     console/API writes require), then mint that merchant's dev bearer token:
       OP=\$(scripts/dev/token.sh --role operator --sub op-1)
       curl -s -X POST http://localhost:${CONTROL_PORT}/v1/operator/merchants \\
         -H "Authorization: Bearer \$OP" -H "Content-Type: application/json" \\
         -H "Idempotency-Key: dev-1" \\
         -d '{"name":"Dev Merchant","owner_email":"dev@example.com"}'    # -> merchant_id
       TOKEN=\$(scripts/dev/token.sh --merchant-id <merchant_id>)
     (Or skip steps 1-5: \`go run ./cmd/sarvex onboard --name "Dev Merchant"\`)
  2. Create a store:
       curl -s -X POST http://localhost:${CONTROL_PORT}/v1/stores \\
         -H "Authorization: Bearer \$TOKEN" -H "Content-Type: application/json" \\
         -d '{"display_name":"Dev Store","platform":"woocommerce"}'
  3. Seed its fake catalog BEFORE pairing (products won't retroactively
     appear in an import that already ran). Use scripts/dev/sim.sh, not
     .devbin/sim directly — it carries the env this run resolved
     (VAULT_ADDR, NATS_URL etc.), which a fresh shell won't have:
       scripts/dev/sim.sh seed --store <store_id> --products 8 --messy
  4. Begin pairing — completes and imports automatically within a few seconds:
       curl -s -X POST http://localhost:${CONTROL_PORT}/v1/stores/<store_id>/connectors/pairing \\
         -H "Authorization: Bearer \$TOKEN" -H "Content-Type: application/json" \\
         -d '{"platform":"woocommerce"}'
  5. Check it worked:
       curl -s http://localhost:${CONTROL_PORT}/v1/stores/<store_id>/journey -H "Authorization: Bearer \$TOKEN"
       curl -s http://localhost:${CONTROL_PORT}/v1/tasks -H "Authorization: Bearer \$TOKEN"

Stop everything: scripts/dev/down.sh
Check status:    scripts/dev/status.sh
EOF
