# Self-hosted infrastructure (k3s + Cilium + NATS JetStream + Vault + Cloudflare)

This tree is Sarvex's production deployment: Kubernetes (k3s + Cilium)
with NATS JetStream for the event bus, Vault (Transit + KV v2) for signing
keys and connector credentials, MongoDB and Redis, fronted by Cloudflare Load
Balancing for global proximity routing. It runs on infrastructure you own — k3s
clusters (one "core" region plus any number of "edge" regions) on any compute
provider.

Local development runs the same backing services (Mongo, Redis, NATS
JetStream, Vault) from `deployments/docker-compose.yaml` — see the root
`README.md` and `make infra-up`. Nothing is emulated.

## Layout

```
deployments/selfhost/
├── helm/
│   ├── sarvex-serving/   # Deployment+Service for the serving plane — runs in EVERY region (core and edge)
│   ├── sarvex-control/   # Deployment+Service for the control plane — CORE region only
│   ├── sarvex-sync/      # Deployment+Service for the sync plane — CORE region only
│   └── sarvex-gateway/   # Gateway API Gateway + HTTPRoutes reproducing cmd/root.go's proxyMux path rules
├── gitops/
│   └── root-app.yaml     # Argo CD "app of apps" — the one thing bootstrap-argocd.sh applies by hand;
│                          # everything else is reconciled from here on by pushing to this repo/directory
├── scripts/              # One idempotent script per bootstrap step (see below) — no dashboard/console step
│                          # anywhere in this tree; every action here has a CLI or Terraform equivalent.
└── terraform/cloudflare/ # DNS + Load Balancer + pools + health-check monitors, as code
```

## Region topology

- **Core region** (≥3 k3s nodes for HA): `control` + `sync` + `serving`, a
  3-node MongoDB replica set (MongoDB Community Kubernetes Operator), the
  NATS JetStream core cluster, and Vault (Transit + KV engines).
- **Edge region** (1+ k3s nodes): `serving` only, plus a local Redis replica.
  No Mongo, no Vault, no control/sync — matches the architecture invariant
  that the serving plane's discovery/catalog reads never leave the cache
  (`CLAUDE.md` §2.2).

## Script order (day 0 → day N+1)

| Step | Script | Runs on |
|---|---|---|
| 1 | `scripts/provision-node.sh <host> server --cluster-init` | first core node |
| 1b | `scripts/provision-node.sh <host> server` / `agent` | additional core nodes |
| 2 | `scripts/bootstrap-cluster.sh core core` | any core node (via kubeconfig) |
| 3 | `scripts/bootstrap-argocd.sh core` | core cluster |
| 4 | `helm install` the four charts above via the GitOps repo (Argo CD does this from step 3 onward — you don't run `helm install` by hand again) | — |
| N+1 | `scripts/add-region.sh <provider> <host> <region>` | new edge region, one command |

`make selfhost-stage0` / `make selfhost-stage1` / `make selfhost-add-region`
wrap these — see the root `Makefile`.

## Images

`.github/workflows/image.yml` builds `build/package/Dockerfile` and pushes
`ghcr.io/sarvexlabs/sarvex.cp` on every merge to `main` (tags: `latest`,
`sha-<commit>`) and on `v*` tags. The charts' `image.tag` selects what the
cluster runs.
