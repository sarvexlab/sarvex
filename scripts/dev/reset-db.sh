#!/usr/bin/env bash
# Drops every collection in the local dev "sarvex" Mongo database except
# control_accounts (the merchant/account collection) — leaves merchant
# records intact while clearing stores, catalog, tasks, and everything
# else so you can re-onboard from a clean slate. Local dev container only;
# refuses to run against anything but the sarvex-dev-mongo container.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
source scripts/dev/lib.sh

KEEP_COLLECTION="control_accounts, control_billing_records"
DB_NAME="sarvex"

if ! docker ps --format '{{.Names}}' | grep -qx "$MONGO_CONTAINER"; then
	echo "  ✗ $MONGO_CONTAINER is not running — start it with \`make dev-up\` first" >&2
	exit 1
fi

echo "── Resetting $DB_NAME (keeping $KEEP_COLLECTION) ──────────────────"
docker exec "$MONGO_CONTAINER" mongosh --quiet \
	-u admin -p password --authenticationDatabase admin \
	"$DB_NAME" --eval '
		const keep = "'"$KEEP_COLLECTION"'".split(",").map(s => s.trim());
		db.getCollectionNames().forEach(name => {
			if (keep.includes(name)) {
				print("  • keeping " + name);
				return;
			}
			db.getCollection(name).drop();
			print("  • dropped " + name);
		});
	'
echo "  done"
