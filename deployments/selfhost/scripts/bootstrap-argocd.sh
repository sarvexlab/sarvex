#!/usr/bin/env bash
# bootstrap-argocd.sh — installs Argo CD into the core cluster (one
# control-plane instance manages every regional cluster from here on) and
# applies gitops/root-app.yaml. This is the LAST manual step in the whole
# rollout: every deploy after this is `git push` to this repo, reconciled
# by Argo CD — never a console click, never a re-run of this script for an
# ordinary change (only re-run if Argo CD itself needs reinstalling).
#
# Idempotent: helm upgrade --install + kubectl apply are both safe to
# re-run against an already-bootstrapped cluster.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
source lib.sh

require_cmd kubectl helm argocd
kubectl cluster-info >/dev/null || fail "kubectl can't reach a cluster — set KUBECONFIG first"

echo "── Argo CD ──────────────────────────────────────────────────────────"
helm repo add argo https://argoproj.github.io/argo-helm >/dev/null 2>&1 || true
helm repo update argo >/dev/null
helm_upsert argocd argo/argo-cd argocd
kubectl -n argocd rollout status deployment/argocd-server --timeout=180s

log "logging into the Argo CD API (initial admin password from its bootstrap secret)..."
ARGOCD_PASSWORD="$(kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)"
kubectl -n argocd port-forward svc/argocd-server 8080:443 >/tmp/argocd-port-forward.log 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" 2>/dev/null || true' EXIT
sleep 3
argocd login localhost:8080 --username admin --password "$ARGOCD_PASSWORD" --insecure >/dev/null
ok "logged in — change the admin password after this (argocd account update-password), it's only meant for this bootstrap"

echo "── Applying the app-of-apps root Application ───────────────────────"
kubectl apply -f "$SELFHOST_DIR/gitops/root-app.yaml"
ok "sarvex-root Application applied — Argo CD now reconciles everything under deployments/selfhost/gitops/apps/*.yaml on every push"

kill "$PF_PID" 2>/dev/null || true
trap - EXIT
ok "Argo CD bootstrap complete"
