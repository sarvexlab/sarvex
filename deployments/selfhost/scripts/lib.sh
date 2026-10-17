#!/usr/bin/env bash
# Shared helpers for deployments/selfhost/scripts/*.sh. Not meant to be run
# directly. Mirrors the style of scripts/dev/lib.sh (idempotent steps,
# ✓/✗/• status lines) but for remote-host/kubectl operations instead of
# local dev processes.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SELFHOST_DIR="$ROOT_DIR/deployments/selfhost"

log()  { echo "  • $*"; }
ok()   { echo "  ✓ $*"; }
fail() { echo "  ✗ $*" >&2; exit 1; }

# require_cmd NAME... — fails loudly, once, listing every missing tool,
# rather than dying on the first `command not found` a script happens to
# hit — every bootstrap script calls this before doing anything else.
require_cmd() {
	local missing=()
	for c in "$@"; do
		command -v "$c" >/dev/null 2>&1 || missing+=("$c")
	done
	if [ "${#missing[@]}" -gt 0 ]; then
		fail "missing required command(s): ${missing[*]} — install them before running this script"
	fi
}

# helm_upsert RELEASE CHART NAMESPACE VALUES_FILES... — `helm upgrade
# --install`, i.e. safe to re-run. VALUES_FILES are applied in order (later
# overrides earlier), matching values.yaml -> values-<env>.yaml layering.
helm_upsert() {
	local release="$1" chart="$2" namespace="$3"
	shift 3
	local -a value_args=()
	for f in "$@"; do
		[ -f "$f" ] && value_args+=(-f "$f")
	done
	helm upgrade --install "$release" "$chart" \
		--namespace "$namespace" --create-namespace \
		"${value_args[@]}"
}
