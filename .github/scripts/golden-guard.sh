#!/usr/bin/env bash
# Enforces the label rules documented in .github/workflows/golden-guard.yml.
# Inputs: BASE_SHA, HEAD_SHA, PR_LABELS (space-separated).
set -euo pipefail

has_label() { [[ " ${PR_LABELS:-} " == *" $1 "* ]]; }
has_label_prefix() { [[ " ${PR_LABELS:-} " == *" $1"* ]]; }

base=$(git merge-base "$BASE_SHA" "$HEAD_SHA")
fail=0
version_re='^[0-9]{4}-[0-9]{2}-[0-9]{2}$'

# Modified or deleted goldens (A = new files need no label).
while IFS=$'\t' read -r status path; do
  [[ "$status" == A ]] && continue
  rel=${path#*testdata/golden/}
  IFS=/ read -r first second _ <<<"$rel"
  first=${first%.json}; second=${second%.json}
  ok=0
  if [[ "$first" == profile ]]; then
    { has_label "platform-change/$second" || has_label_prefix "spec-change/"; } && ok=1
    need="platform-change/$second or a spec-change/* label"
  elif [[ "$first" =~ $version_re || "$first" == not-negotiated ]]; then
    has_label "spec-change/$first" && ok=1
    need="spec-change/$first"
    [[ "$first" == not-negotiated ]] && { has_label_prefix "spec-change/" && ok=1; need="a spec-change/* label"; }
  elif [[ "$second" == canonical ]]; then
    has_label "platform-change/$first" && ok=1
    need="platform-change/$first"
  else
    { has_label "spec-change/$second" || has_label "platform-change/$first"; } && ok=1
    need="spec-change/$second or platform-change/$first"
  fi
  if [[ $ok == 0 ]]; then
    echo "::error file=$path::golden changed ($status) without label $need"
    fail=1
  fi
done < <(git diff --name-status --no-renames "$base" "$HEAD_SHA" -- '*/testdata/golden/*')

if [[ $fail == 0 ]]; then
  echo "golden guard: ok"
fi
exit $fail
