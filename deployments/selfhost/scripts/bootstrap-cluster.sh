#!/usr/bin/env bash
# bootstrap-cluster.sh <region> <role:core|edge>
#
# Idempotent. Assumes KUBECONFIG already points at the target cluster (see
# provision-node.sh's printed instructions for pulling one off a fresh k3s
# server). Installs:
#   - every role: Cilium (eBPF CNI, kube-proxy-replacement) + Gateway API
#     CRDs + a regional Redis (bitnami/redis).
#   - role=core only: the MongoDB Community Operator + a 3-node replica
#     set, NATS (JetStream, becomes the supercluster hub), and Vault
#     (Transit+KV), with a SCRIPTED init/unseal — no Vault UI step ever.
#
# Re-running this script against an already-bootstrapped cluster converges
# it (helm upgrade --install, kubectl apply, and the Vault init/unseal
# logic all detect "already done" and skip) — see lib.sh's helm_upsert and
# this file's vault_init_and_unseal.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source lib.sh

REGION="${1:?usage: bootstrap-cluster.sh <region> <role:core|edge>}"
ROLE="${2:?role must be 'core' or 'edge'}"
[ "$ROLE" = "core" ] || [ "$ROLE" = "edge" ] || fail "role must be 'core' or 'edge', got '$ROLE'"

NAMESPACE="sarvex-${ROLE}"
[ "$ROLE" = "edge" ] && NAMESPACE="sarvex-${REGION}"

require_cmd kubectl helm cilium
kubectl cluster-info >/dev/null || fail "kubectl can't reach a cluster — set KUBECONFIG first"

echo "── Cilium (eBPF CNI, kube-proxy-replacement) ──────────────────────"
if cilium status --wait=false >/dev/null 2>&1; then
	ok "cilium already installed"
else
	cilium install --set kubeProxyReplacement=true --set gatewayAPI.enabled=true
	cilium status --wait
	ok "cilium installed"
fi

echo "── Gateway API CRDs ────────────────────────────────────────────────"
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
ok "Gateway API CRDs applied"

echo "── Regional Redis (namespace: $NAMESPACE) ──────────────────────────"
helm repo add bitnami https://charts.bitnami.com/bitnami >/dev/null 2>&1 || true
helm repo update bitnami >/dev/null
helm_upsert redis bitnami/redis "$NAMESPACE" "$SELFHOST_DIR/manifests/redis-values.yaml"
ok "redis ready in $NAMESPACE"

if [ "$ROLE" != "core" ]; then
	ok "edge bootstrap complete for region '$REGION' — serving-plane Helm release is deployed by Argo CD, not this script"
	exit 0
fi

echo "── MongoDB Community Operator + 3-node replica set (core only) ─────"
kubectl apply -f https://raw.githubusercontent.com/mongodb/mongodb-kubernetes-operator/master/config/crd/bases/mongodbcommunity.mongodb.com_mongodbcommunity.yaml
kubectl apply -f https://raw.githubusercontent.com/mongodb/mongodb-kubernetes-operator/master/config/rbac/role.yaml -n "$NAMESPACE" || true
helm repo add mongodb https://mongodb.github.io/helm-charts >/dev/null 2>&1 || true
helm repo update mongodb >/dev/null
helm_upsert mongodb-operator mongodb/community-operator "$NAMESPACE"
kubectl apply -n "$NAMESPACE" -f "$SELFHOST_DIR/manifests/mongodb-community-cr.yaml"
ok "MongoDB Community Operator + replica-set CR applied"

echo "── NATS JetStream (core supercluster hub) ──────────────────────────"
helm repo add nats https://nats-io.github.io/k8s/helm/charts/ >/dev/null 2>&1 || true
helm repo update nats >/dev/null
helm_upsert nats nats/nats "$NAMESPACE" "$SELFHOST_DIR/manifests/nats-values.yaml"
ok "NATS JetStream ready"

echo "── Vault (Transit + KV) ─────────────────────────────────────────────"
helm repo add hashicorp https://helm.releases.hashicorp.com >/dev/null 2>&1 || true
helm repo update hashicorp >/dev/null
helm_upsert vault hashicorp/vault "$NAMESPACE" "$SELFHOST_DIR/manifests/vault-values.yaml"
kubectl -n "$NAMESPACE" rollout status statefulset/vault --timeout=180s || true

vault_init_and_unseal() {
	# age recipient/identity are how the unseal keys leave this script
	# without ever touching a dashboard or an unencrypted file. This is a
	# one-time bootstrap step, run locally.
	local age_recipient="${VAULT_AGE_RECIPIENT:-}"
	local keyfile="$SELFHOST_DIR/.secrets/vault-unseal-${REGION}.json.age"
	mkdir -p "$SELFHOST_DIR/.secrets"

	local status
	status="$(kubectl -n "$NAMESPACE" exec vault-0 -- vault status -format=json 2>/dev/null || echo '{}')"
	local initialized sealed
	initialized="$(echo "$status" | grep -o '"initialized":[a-z]*' | cut -d: -f2)"
	sealed="$(echo "$status" | grep -o '"sealed":[a-z]*' | cut -d: -f2)"

	if [ "$initialized" != "true" ]; then
		[ -n "$age_recipient" ] || fail "Vault isn't initialized yet and VAULT_AGE_RECIPIENT isn't set — export it (an 'age' public key: age-keygen -o key.txt) so the unseal keys can be encrypted at rest instead of printed to a terminal"
		require_cmd age
		log "initializing Vault (first run for this cluster)..."
		kubectl -n "$NAMESPACE" exec vault-0 -- vault operator init -key-shares=5 -key-threshold=3 -format=json \
			| age -r "$age_recipient" -o "$keyfile"
		ok "unseal keys encrypted to $keyfile — back this up somewhere durable NOW (it is the only copy)"
		sealed="true"
	fi

	if [ "$sealed" = "true" ]; then
		[ -f "$keyfile" ] || fail "Vault is sealed and $keyfile doesn't exist locally — restore it from backup, decrypt with your age identity, and re-run"
		require_cmd age jq
		log "unsealing Vault from $keyfile..."
		local identity="${VAULT_AGE_IDENTITY:?set VAULT_AGE_IDENTITY to the path of the age private key that decrypts $keyfile}"
		local keys
		keys="$(age -d -i "$identity" "$keyfile" | jq -r '.unseal_keys_b64[0:3][]')"
		for pod in vault-0 vault-1 vault-2; do
			while IFS= read -r k; do
				kubectl -n "$NAMESPACE" exec "$pod" -- vault operator unseal "$k" >/dev/null
			done <<< "$keys"
		done
		ok "Vault unsealed"
	else
		ok "Vault already initialized and unsealed"
	fi
}
vault_init_and_unseal

ok "core bootstrap complete for region '$REGION'"
