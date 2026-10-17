#!/usr/bin/env bash
# provision-node.sh <ssh-host> <role:server|agent> [k3s-url] [k3s-token]
#
# Installs k3s on any host on any provider — the k3s installer is
# provider-agnostic by construction, so this one script is reused unchanged
# across Vultr/Hetzner/bare metal/whatever is cheapest that day (plan §6,
# "add compute on any compute provider").
#
#   provision-node.sh root@1.2.3.4 server                # first core node (cluster-init)
#   provision-node.sh root@1.2.3.5 server https://1.2.3.4:6443 "$K3S_TOKEN"   # HA join
#   provision-node.sh root@5.6.7.8 agent  https://1.2.3.4:6443 "$K3S_TOKEN"   # edge node
#
# Idempotent: if k3s is already installed and its service is active, this
# is a no-op (matches scripts/dev/lib.sh's ensure_container convention).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source lib.sh

require_cmd ssh

HOST="${1:?usage: provision-node.sh <ssh-host> <role:server|agent> [k3s-url] [k3s-token]}"
ROLE="${2:?role must be 'server' or 'agent'}"
K3S_URL="${3:-}"
K3S_TOKEN="${4:-}"

[ "$ROLE" = "server" ] || [ "$ROLE" = "agent" ] || fail "role must be 'server' or 'agent', got '$ROLE'"

log "checking whether k3s is already running on $HOST..."
if ssh -o StrictHostKeyChecking=accept-new "$HOST" 'systemctl is-active --quiet k3s || systemctl is-active --quiet k3s-agent' 2>/dev/null; then
	ok "$HOST already runs k3s — nothing to do"
	exit 0
fi

INSTALL_EXEC="$ROLE"
if [ "$ROLE" = "server" ] && [ -z "$K3S_URL" ]; then
	# First server in a new HA control plane (embedded etcd via --cluster-init).
	# Additional servers join with --server <url>, same as agents do.
	INSTALL_EXEC="server --cluster-init"
fi

log "installing k3s ($ROLE) on $HOST..."
# shellcheck disable=SC2087 # intentional: K3S_URL/K3S_TOKEN/INSTALL_K3S_EXEC are meant to expand on the remote side via the heredoc below, not locally
ssh -o StrictHostKeyChecking=accept-new "$HOST" bash -s -- "$INSTALL_EXEC" "$K3S_URL" "$K3S_TOKEN" <<'REMOTE'
set -euo pipefail
exec_role="$1"; url="$2"; token="$3"
export INSTALL_K3S_EXEC="$exec_role"
[ -n "$url" ] && export K3S_URL="$url"
[ -n "$token" ] && export K3S_TOKEN="$token"
curl -sfL https://get.k3s.io | sh -
REMOTE

ok "k3s ($ROLE) installed on $HOST"

if [ "$ROLE" = "server" ] && [ -z "$K3S_URL" ]; then
	log "fetching kubeconfig from $HOST — merge it into your local KUBECONFIG:"
	echo "    ssh $HOST cat /etc/rancher/k3s/k3s.yaml | sed 's/127.0.0.1/${HOST#*@}/' > ./kubeconfig-\$(date +%s).yaml"
	log "and the join token other nodes will need:"
	echo "    ssh $HOST cat /var/lib/rancher/k3s/server/node-token"
fi
