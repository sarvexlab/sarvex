#!/usr/bin/env bash
# Starts the backing services every local run, test run and CI job shares:
# MongoDB (single-node replica set), Redis, NATS JetStream, Vault (Transit +
# KV v2) and an OTel Collector, all from deployments/docker-compose.yaml —
# the same services a self-hosted Kubernetes deployment runs. Idempotent.
# Used by `make dev-up`, `make dev-up-monolith`, `make infra-up` and CI.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

echo "── Infra (Mongo, Redis, NATS, Vault, otel-collector) ──────────────"
compose up -d mongodb mongodb-rs-init redis nats vault otel-collector
wait_healthy mongodb 60
wait_mongo_primary 60
wait_tcp 127.0.0.1 "$REDIS_HOST_PORT" redis 30
wait_healthy nats 60
wait_healthy vault 90
wait_tcp 127.0.0.1 "$OTEL_GRPC_HOST_PORT" otel-collector 30
echo "  ✓ mongo + redis + nats + vault + otel-collector ready"
