#!/usr/bin/env bash
# Runs the official UCP conformance suite
# (github.com/Universal-Commerce-Protocol/conformance) against a real,
# freshly onboarded Sarvex store, once per supported UCP release.
#
# Why wrap an external suite instead of only running cmd/sarvex/conformance
# (Sarvex's own): the official suite is now the authority on protocol
# conformance — Sarvex re-deriving the same checks is duplicated,
# lower-fidelity effort. Sarvex's own suite keeps its job (fast checks of
# Sarvex-specific failure paths the official suite knows nothing about);
# this script adds the official one as a second, independent gate.
#
# What "once per supported UCP release" means in practice: the suite reads
# a single ucp_version from conformance_input.json, which it uses only to
# pick the version its own decoy shopping agent declares during
# negotiation (protocol_test.py, integration_test_utils.AgentProfileServer)
# — Sarvex's serving plane then negotiates each capability down to whatever
# it can mutually serve (internal/planes/serving/api/dependencies.go's
# ServableVersions/CheckoutServableVersions). Running the suite once per
# internal/spec generation we ship (2026-04-08, 2026-08-25) is
# therefore the correct way to exercise both, not a workaround.
#
# The suite's own ucp-sdk dependency (a single, unversioned model package —
# see python-sdk's src/ucp_sdk) is not itself split per spec release as of
# this writing, so a mismatch here is upstream's to fix, not something to
# route around locally: a genuine failure at one version should surface as
# one, not be silently absorbed.
#
# UCP_CONFORMANCE_DESELECT below is the one deliberate exception to that: a
# named, individually-justified list of tests excluded because they fail
# inside the pinned suite/SDK's own code before a request ever reaches
# Sarvex, not because of anything Sarvex does. Nothing traces back to
# Sarvex's behavior belongs there — a genuine regression must still fail
# the run.
#
# Requires: a Go toolchain (to build/run cmd/sarvex), Python 3.10+ and uv
# (https://docs.astral.sh/uv/), and the same infrastructure
# `make test-conformance` needs — Mongo, Redis, NATS and Vault (`make infra-up`) — since
# this script starts Sarvex's planes in-process exactly the way that target
# does.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
REPO_ROOT="$(pwd)"

# Shared simulation secret: the official suite sends it (Simulation-Secret
# header) and Sarvex's serving-plane testing affordance validates against the
# same value. Export it so both the in-process Sarvex started below and the
# pytest run inherit one agreed value rather than the suite defaulting to a
# random per-run uuid that the server could never match.
export SIMULATION_SECRET="${SIMULATION_SECRET:-ucp-conformance-ci}"

# Pinned commits, not branches or "main": the official suite has no
# per-UCP-release tags (its own versioning is the ucp_version field, not
# git refs), so a floating ref would make CI's pass/fail depend on whatever
# upstream merged since the last run. Bump these deliberately, the same way
# internal/spec/*/DISCREPANCIES.md pins the spec source commit.
UCP_CONFORMANCE_REPO=${UCP_CONFORMANCE_REPO:-https://github.com/Universal-Commerce-Protocol/conformance.git}
UCP_CONFORMANCE_REF=${UCP_CONFORMANCE_REF:-016ecbc22240affdec4429a60d539bedd04ab51b}
UCP_PYTHON_SDK_REPO=${UCP_PYTHON_SDK_REPO:-https://github.com/Universal-Commerce-Protocol/python-sdk.git}
UCP_PYTHON_SDK_REF=${UCP_PYTHON_SDK_REF:-836b0228d19a4a64af8930c6b70c0e581c09b20b}

# The UCP releases internal/spec vendors today. Add a version here
# the same day a third types/vYYYYMMDD package lands, not before — the
# fixtures this script generates come from the store's live catalog, so
# there is nothing else to keep in sync.
UCP_VERSIONS=${UCP_VERSIONS:-"2026-04-08 2026-08-25"}

# Space-separated pytest node ids to --deselect, confirmed upstream bugs in
# the pinned suite/SDK — not Sarvex behavior. Re-verify each entry (still
# reproduces, still traces to the same upstream cause) whenever
# UCP_CONFORMANCE_REF/UCP_PYTHON_SDK_REF above are bumped, and drop it the
# day upstream fixes it.
#
# pytest's own --deselect matches by node id PREFIX, not exact id (see
# _pytest/main.py: `colitem.nodeid.startswith(deselect_prefixes)`) — before
# adding an entry here, check the suite for any other test whose name
# extends it (e.g. test_discovery/test_discovery_urls below, which is
# exactly why that one isn't listed here — see
# UCP_CONFORMANCE_DISCOVERY_VERSION_DESELECT_ON instead).
#
# - business_logic_test.py::BusinessLogicTest::test_buyer_consent: fails
#   with `TypeError: TypeAliasType.__call__() got an unexpected keyword
#   argument 'marketing'` while the test itself builds its request payload —
#   before anything is sent to Sarvex. A bug in the pinned python-sdk
#   commit's buyer-consent model (a PEP 695 `type X = ...` alias being
#   called with a keyword argument, which TypeAliasType does not support).
UCP_CONFORMANCE_DESELECT=${UCP_CONFORMANCE_DESELECT:-"business_logic_test.py::BusinessLogicTest::test_buyer_consent"}

# protocol_test.py::ProtocolTest::test_discovery, excluded for the
# 2026-04-08 pass only — unlike UCP_CONFORMANCE_DESELECT above, this is NOT
# an upstream bug: Sarvex's discovery document is a single, unauthenticated,
# cached profile whose top-level ucp.version is always the newest UCP
# generation it knows (types.Latest(), currently 2026-08-25 —
# RenderProfile's own doc comment), so the pass that expects the *older*
# release at the top level can never see it there. A real, current,
# deliberately-scoped Sarvex limitation, excluded here at explicit
# direction rather than a bug fix. Scoped to the one pass it actually fails
# on, not added to UCP_CONFORMANCE_DESELECT, so the 2026-08-25 pass — where
# this exact assertion already passes — keeps exercising it.
#
# Uses -k, not --deselect: test_discovery is a node id PREFIX of the
# sibling test test_discovery_urls (see the prefix-match note above), so
# --deselect "...::test_discovery" silently takes test_discovery_urls with
# it too. The -k expression below excludes exactly the one test: false for
# items whose (substring, pytest -k semantics) keywords include
# "test_discovery" unless they also include "test_discovery_urls".
# Verified against a real collect-only run before this was added.
UCP_CONFORMANCE_DISCOVERY_VERSION_DESELECT_ON=${UCP_CONFORMANCE_DISCOVERY_VERSION_DESELECT_ON:-"2026-04-08"}
UCP_CONFORMANCE_DISCOVERY_KEXPR="not test_discovery or test_discovery_urls"

WORKDIR=${WORKDIR:-$(mktemp -d -t ucp-conformance.XXXXXX)}
FIXTURES_DIR="$WORKDIR/fixtures"
READY_FILE="$WORKDIR/ready"
SERVE_LOG="$WORKDIR/sarvex-conform-serve.log"

echo "── Working directory: $WORKDIR ─────────────────────────────────────"
mkdir -p "$FIXTURES_DIR"

clone_pinned() {
	local repo=$1 ref=$2 dest=$3
	if [[ -d "$dest/.git" ]]; then
		echo "reusing existing clone at $dest"
		return
	fi
	git init -q "$dest"
	git -C "$dest" fetch -q --depth 1 "$repo" "$ref"
	git -C "$dest" checkout -q FETCH_HEAD
}

echo "── Fetching the official suite (pinned) ────────────────────────────"
clone_pinned "$UCP_CONFORMANCE_REPO" "$UCP_CONFORMANCE_REF" "$WORKDIR/conformance"
# The conformance suite's pyproject.toml resolves ucp-sdk through a local
# editable path override (tool.uv.sources), not PyPI, so its own README
# has the suite and the SDK checked out as siblings — mirror that layout
# here rather than fighting uv's resolution.
clone_pinned "$UCP_PYTHON_SDK_REPO" "$UCP_PYTHON_SDK_REF" "$WORKDIR/python-sdk"

echo "── Installing the suite's dependencies (uv sync) ───────────────────"
(cd "$WORKDIR/conformance" && uv sync)

# Known upstream bug, not this repo's: the pinned conformance commit still
# imports several types (payment, payment_create_request,
# payment_update_request, types.reverse_domain_name, ...) from
# ucp_sdk.models.schemas.shopping, which is where they lived as of
# python-sdk's 2026-01-23 release; a later python-sdk reorganization moved
# them under ucp_sdk.models.schemas.common.types and conformance's main
# was never updated to follow. This reproduces the official README's own
# walkthrough verbatim (main next to main), not just these pinned commits.
# fix_upstream_imports.py relocates the stale imports and verifies every
# file in the suite imports cleanly afterward, failing loudly if it can't —
# delete this step once upstream ships a fix.
echo "── Working around a known upstream import-path bug ─────────────────"
(cd "$WORKDIR/conformance" && uv run python "$REPO_ROOT/scripts/ucp-conformance/fix_upstream_imports.py" . "$WORKDIR/python-sdk/src")

SARVEX_PID=""
cleanup() {
	if [[ -n "$SARVEX_PID" ]] && kill -0 "$SARVEX_PID" 2>/dev/null; then
		kill "$SARVEX_PID" 2>/dev/null || true
		wait "$SARVEX_PID" 2>/dev/null || true
	fi
}
trap cleanup EXIT

echo "── Building sarvex ──────────────────────────────────────────────────"
# Built once and exec'd directly, rather than `go run`'d, so SARVEX_PID
# below names the actual server process — `go run` spawns a child for the
# compiled binary, and killing the wrapper does not reliably reach it.
go build -o "$WORKDIR/sarvex" ./cmd/sarvex

echo "── Onboarding a store and starting Sarvex in-process ───────────────"
"$WORKDIR/sarvex" conform serve \
	--in-process \
	--merchant-name "UCP Official Conformance" \
	--output-dir "$FIXTURES_DIR" \
	--ready-file "$READY_FILE" \
	--ucp-versions "${UCP_VERSIONS// /,}" \
	>"$SERVE_LOG" 2>&1 &
SARVEX_PID=$!

echo "waiting for the store to come up (pid $SARVEX_PID, log: $SERVE_LOG)"
for _ in $(seq 1 150); do
	if [[ -s "$READY_FILE" ]]; then
		break
	fi
	if ! kill -0 "$SARVEX_PID" 2>/dev/null; then
		echo "sarvex conform serve exited before becoming ready:" >&2
		cat "$SERVE_LOG" >&2
		exit 1
	fi
	sleep 2
done
if [[ ! -s "$READY_FILE" ]]; then
	echo "timed out waiting for $READY_FILE" >&2
	cat "$SERVE_LOG" >&2
	exit 1
fi
SERVER_URL=$(cat "$READY_FILE")
echo "store ready at $SERVER_URL"

# The suite's own --conformance_input/--fixture_config flags (its README's
# documented usage) go nowhere on this pinned commit: conftest.py calls
# FLAGS(["pytest"]) — a hardcoded argv, not sys.argv — so pytest's own
# parser sees those flags as unrecognized and refuses to start. Another
# upstream/README mismatch, verified by hand the same way as the import
# path one above. The flags' actual defaults point at
# test_data/flower_shop/{conformance_input,test_fixtures}.json relative to
# the suite's own cwd, which *is* honored (it's a plain default, not argv
# parsing) — so each version's run overwrites those two files in place and
# invokes bare `uv run pytest`, exactly like the README's own "Example
# Walkthrough" section does against the sample server.
DEFAULT_INPUT="$WORKDIR/conformance/test_data/flower_shop/conformance_input.json"
DEFAULT_FIXTURES="$WORKDIR/conformance/test_data/flower_shop/test_fixtures.json"
# The suite reads its checkout-completion payment instrument from this CSV
# (integration_test_utils.get_valid_payment_payload) and the server rejects a
# handler it does not advertise. `sarvex conform serve` writes one whose
# handler_id is the store's own advertised handler; install it over the
# flower_shop default so completion targets a handler this store accepts.
DEFAULT_INSTRUMENTS="$WORKDIR/conformance/test_data/flower_shop/payment_instruments.csv"

deselect_args=()
for test_id in $UCP_CONFORMANCE_DESELECT; do
	deselect_args+=(--deselect "$test_id")
done

status=0
for version in $UCP_VERSIONS; do
	echo "── Running the official suite against UCP $version ─────────────"
	input="$FIXTURES_DIR/conformance_input-$version.json"
	if [[ ! -f "$input" ]]; then
		echo "no fixture written for $version ($input); skipping" >&2
		status=1
		continue
	fi
	cp "$input" "$DEFAULT_INPUT"
	cp "$FIXTURES_DIR/test_fixtures.json" "$DEFAULT_FIXTURES"
	if [[ -f "$FIXTURES_DIR/payment_instruments.csv" ]]; then
		cp "$FIXTURES_DIR/payment_instruments.csv" "$DEFAULT_INSTRUMENTS"
	fi
	version_deselect_args=("${deselect_args[@]}")
	if [[ "$version" == "$UCP_CONFORMANCE_DISCOVERY_VERSION_DESELECT_ON" ]]; then
		version_deselect_args+=(-k "$UCP_CONFORMANCE_DISCOVERY_KEXPR")
	fi
	if ! (
		cd "$WORKDIR/conformance" &&
			SERVER_URL="$SERVER_URL" \
				SIMULATION_SECRET="${SIMULATION_SECRET:-ucp-conformance-ci}" \
				uv run pytest "${version_deselect_args[@]}"
	); then
		echo "official UCP conformance suite FAILED for $version" >&2
		status=1
	fi
done

exit $status
