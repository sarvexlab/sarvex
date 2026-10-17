#!/usr/bin/env bash
# Provisions load-test fixtures against an already-running `make dev-up`
# stack: one merchant+store per LOADTEST_STORES, a seeded fake-WooCommerce
# catalog per store (via scripts/dev/sim.sh, which must run BEFORE pairing —
# products don't retroactively appear in an import that already ran), then
# pairing (which auto-completes and imports in dev mode via the
# ConnectorService devPairingHook — see internal/planes/control/connector/
# connector_service.go). Polls until both the control-plane journey and the
# serving-plane cache report the store ready, since catalog reads 404 on an
# unpopulated Redis snapshot and there is no lazy/on-demand population path.
#
# Each merchant is created via POST /operator/merchants (operator-scoped)
# rather than self-minted into an owner token: AccountService.Create
# synchronously provisions an active trial subscription
# (billing_service.go's ProvisionTrial), which every mutating merchant-scoped
# route requires (requireActiveBilling — see middleware.go). A merchant_id
# that was never created this way has no billing record, so
# POST /stores/etc. 403s with "billing_read_only".
#
# Idempotent-ish: re-running adds LOADTEST_STORES *more* stores rather than
# reusing existing ones (there's no lookup-by-name), so pass a smaller count
# or inspect .manifest.json before re-seeding a long-running dev box.
#
# Usage: scripts/loadtest/seed.sh
# Env:   LOADTEST_STORES=5 LOADTEST_PRODUCTS_PER_STORE=50 \
#        LOADTEST_LARGE_STORE_PRODUCTS=5000 (0 to disable) \
#        CONTROL_BASE_URL=http://localhost:8081/v1
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

CONTROL_BASE_URL="${CONTROL_BASE_URL:-http://localhost:8081/v1}"
SERVING_BASE_URL="${SERVING_BASE_URL:-http://localhost:8083/ucp}"
STORES="${LOADTEST_STORES:-5}"
PRODUCTS_PER_STORE="${LOADTEST_PRODUCTS_PER_STORE:-50}"
LARGE_STORE_PRODUCTS="${LOADTEST_LARGE_STORE_PRODUCTS:-5000}"
RUN_ID="${LOADTEST_RUN_ID:-$(date +%s)}"
MANIFEST="loadtest/.manifest.json"

if ! command -v jq >/dev/null 2>&1; then
	echo "jq is required (used to parse control-plane API responses)" >&2
	exit 1
fi

echo "── Seeding $STORES load-test store(s) ($PRODUCTS_PER_STORE products each, control=$CONTROL_BASE_URL) ──"

OPERATOR_TOKEN=$(scripts/dev/token.sh --role operator)

entries=()

seed_one_store() {
	local idx="$1" product_count="$2" messy_flag="$3"
	local token store_id connector_id merchant_id

	merchant_id=$(curl -sf -X POST "$CONTROL_BASE_URL/operator/merchants" \
		-H "Authorization: Bearer $OPERATOR_TOKEN" -H "Content-Type: application/json" \
		-d '{"name":"Load Merchant '"$idx"' ('"$RUN_ID"')","owner_email":"loadtest-'"$RUN_ID"'-'"$idx"'@example.test"}' | jq -r '.merchant_id')
	if [ -z "$merchant_id" ] || [ "$merchant_id" = "null" ]; then
		echo "  ✗ store $idx: failed to create merchant account" >&2
		return 1
	fi

	token=$(scripts/dev/token.sh --merchant-id "$merchant_id" --role owner)

	store_id=$(curl -sf -X POST "$CONTROL_BASE_URL/stores" \
		-H "Authorization: Bearer $token" -H "Content-Type: application/json" \
		-d '{"display_name":"Load Store '"$idx"'","platform":"woocommerce"}' | jq -r '.store_id')
	if [ -z "$store_id" ] || [ "$store_id" = "null" ]; then
		echo "  ✗ store $idx: failed to create store" >&2
		return 1
	fi

	echo "  • store $idx: $store_id (merchant $merchant_id) — seeding $product_count products"
	if [ "$messy_flag" = "1" ]; then
		scripts/dev/sim.sh seed --store "$store_id" --products "$product_count" --messy >/dev/null
	else
		scripts/dev/sim.sh seed --store "$store_id" --products "$product_count" >/dev/null
	fi

	connector_id=$(curl -sf -X POST "$CONTROL_BASE_URL/stores/$store_id/connectors/pairing" \
		-H "Authorization: Bearer $token" -H "Content-Type: application/json" \
		-d '{"platform":"woocommerce"}' | jq -r '.connector_id')
	echo "  • store $idx: pairing started (connector $connector_id), auto-completes in dev mode — waiting for import"

	# Poll the control-plane journey until it moves past "connected" (pairing
	# complete but import not yet finished) — see
	# internal/planes/control/domain/journey.go for the state sequence.
	local waited=0 state=""
	while [ "$waited" -lt 60 ]; do
		state=$(curl -sf "$CONTROL_BASE_URL/stores/$store_id/journey" -H "Authorization: Bearer $token" | jq -r '.state // ""')
		if [ -n "$state" ] && [ "$state" != "connected" ] && [ "$state" != "prospect" ]; then
			break
		fi
		sleep 1
		waited=$((waited + 1))
	done
	if [ "$state" = "connected" ] || [ "$state" = "prospect" ] || [ -z "$state" ]; then
		echo "  ! store $idx: journey still \"$state\" after ${waited}s — import may still be running" >&2
	else
		echo "  ✓ store $idx: journey state = $state (import complete) after ${waited}s"
	fi

	# Poll the serving plane's cache directly — it's populated by a separate
	# event chain (control.catalog.import -> serving.rebuild.requested ->
	# CacheRebuilder.RebuildSnapshot) and can lag the journey state.
	waited=0
	while [ "$waited" -lt 30 ]; do
		if curl -sf -o /dev/null "$SERVING_BASE_URL/woocommerce/$store_id/.well-known/ucp"; then
			echo "  ✓ store $idx: serving-plane cache populated after ${waited}s"
			break
		fi
		sleep 1
		waited=$((waited + 1))
	done

	entries+=("$(jq -nc --arg idx "$idx" --arg merchant_id "$merchant_id" --arg store_id "$store_id" \
		--arg connector_id "$connector_id" --arg token "$token" --arg platform woocommerce \
		--argjson products "$product_count" \
		'{index:($idx|tonumber),merchant_id:$merchant_id,store_id:$store_id,connector_id:$connector_id,token:$token,platform:$platform,products:$products}')")
}

for i in $(seq 1 "$STORES"); do
	seed_one_store "$i" "$PRODUCTS_PER_STORE" "1"
done

if [ "$LARGE_STORE_PRODUCTS" -gt 0 ]; then
	large_idx=$((STORES + 1))
	echo "── Seeding 1 large-catalog store ($LARGE_STORE_PRODUCTS products, for catalog/search scaling) ──"
	seed_one_store "$large_idx" "$LARGE_STORE_PRODUCTS" "0"
fi

printf '%s\n' "${entries[@]}" | jq -s '.' >"$MANIFEST"
echo ""
echo "✓ Wrote $(jq 'length' "$MANIFEST") store(s) to $MANIFEST"
echo "  Run load tests with: make loadtest-serving / make loadtest-control"
