#!/bin/sh
# Local Vault bootstrap: runs a persistent (file-backed) Vault server and, on
# every start, makes it ready for Sarvex — initialises it once, unseals
# it, enables the Transit engine (store signing keys) and the KV v2 engine
# (connector credentials), and creates the fixed local-development token.
#
# This mirrors what deployments/selfhost/scripts/bootstrap-cluster.sh does on
# a Kubernetes cluster, minus the Kubernetes auth method and Agent Injector.
# The unseal key and initial root token are kept on the data volume
# (/vault/file/local-init.txt), so a restart unseals itself. Development only.
set -eu

export VAULT_ADDR="http://127.0.0.1:8200"
INIT_FILE="/vault/file/local-init.txt"
TRANSIT_KEY="${VAULT_TRANSIT_KEY_NAME:-sarvex-jwk-signing}"
DEV_TOKEN="${VAULT_DEV_TOKEN:-sarvex-dev-token}"

vault server -config=/vault/config/vault.hcl &
SERVER_PID=$!

# `vault status` exits 0 (unsealed), 2 (sealed/uninitialised) or 1 (cannot connect).
until { vault status >/dev/null 2>&1; rc=$?; [ "$rc" -ne 1 ]; }; do
	sleep 1
done

if ! vault status -format=json 2>/dev/null | grep -q '"initialized": true'; then
	vault operator init -key-shares=1 -key-threshold=1 >"$INIT_FILE"
	chmod 600 "$INIT_FILE"
fi
UNSEAL_KEY="$(awk '/Unseal Key 1:/ {print $NF}' "$INIT_FILE")"
ROOT_TOKEN="$(awk '/Initial Root Token:/ {print $NF}' "$INIT_FILE")"

vault status >/dev/null 2>&1 || vault operator unseal "$UNSEAL_KEY" >/dev/null
export VAULT_TOKEN="$ROOT_TOKEN"

vault secrets list -format=json | grep -q '"transit/"' || vault secrets enable transit
vault read "transit/keys/$TRANSIT_KEY" >/dev/null 2>&1 || vault write -f "transit/keys/$TRANSIT_KEY"
vault secrets list -format=json | grep -q '"secret/"' || vault secrets enable -path=secret -version=2 kv
# A root-policy token with a fixed id, so every local config, test and script
# can use the same VAULT_TOKEN. Never do this outside local development.
vault token lookup "$DEV_TOKEN" >/dev/null 2>&1 || vault token create -id="$DEV_TOKEN" -policy=root >/dev/null

echo "vault: ready (transit key '$TRANSIT_KEY', kv v2 at secret/, token '$DEV_TOKEN')"
wait "$SERVER_PID"
