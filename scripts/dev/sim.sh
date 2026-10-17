#!/usr/bin/env bash
# Wrapper for `.devbin/sim` (seed/mutate/fix/status) that loads the env
# up.sh resolved and persisted (.devbin/dev.env) — a plain new terminal
# doesn't have VAULT_ADDR, NATS_URL etc. exported, since those only lived
# in up.sh's own process and its background children.
#
# Usage: scripts/dev/sim.sh seed --store <id> --products 8 --messy
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

if [ ! -f "$DEV_ENV_FILE" ]; then
	echo "no persisted dev env found — run scripts/dev/up.sh first" >&2
	exit 1
fi
set -a
source "$DEV_ENV_FILE"
set +a

exec "$DEVBIN/sim" "$@"
