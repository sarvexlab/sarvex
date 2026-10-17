#!/usr/bin/env bash
# bootstrap-nats-leaf.sh <region> <core-leafnode-url>
#
#   bootstrap-nats-leaf.sh eu-west nats-leaf://core.sarvex.internal:7422
#
# Joins a new edge region's NATS to the core supercluster (plan §6's
# concrete day-N+1 step 3) — this is what lets the core region's control/
# sync planes fan cache-rebuild events out to every region without each
# edge's serving plane reaching back to core for every message. Idempotent:
# `helm upgrade --install` against an already-joined region just reconciles.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source lib.sh

REGION="${1:?usage: bootstrap-nats-leaf.sh <region> <core-leafnode-url>}"
CORE_URL="${2:?usage: bootstrap-nats-leaf.sh <region> <core-leafnode-url>}"
NAMESPACE="sarvex-${REGION}"

require_cmd kubectl helm sed
kubectl cluster-info >/dev/null || fail "kubectl can't reach the $REGION cluster — set KUBECONFIG first"

helm repo add nats https://nats-io.github.io/k8s/helm/charts/ >/dev/null 2>&1 || true
helm repo update nats >/dev/null

TMP_VALUES="$(mktemp)"
trap 'rm -f "$TMP_VALUES"' EXIT
sed "s#__CORE_LEAFNODE_URL__#${CORE_URL}#" "$SELFHOST_DIR/manifests/nats-leaf-values.yaml.tmpl" > "$TMP_VALUES"

helm_upsert nats-leaf nats/nats "$NAMESPACE" "$TMP_VALUES"
kubectl -n "$NAMESPACE" rollout status statefulset/nats-leaf --timeout=120s || true
ok "NATS leaf node in '$REGION' joined to core supercluster at $CORE_URL"
