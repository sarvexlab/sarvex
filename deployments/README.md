# Deployments

Sarvex runs on Kubernetes. There is one production path and one local path,
and they use the same backing services.

| What | Where | Backing services |
|---|---|---|
| Production (self-hosted Kubernetes) | [`selfhost/`](./selfhost/README.md): Helm charts, Argo CD GitOps, bootstrap scripts, Cloudflare Terraform | MongoDB replica set, Redis, NATS JetStream, Vault (Transit + KV v2) |
| Local development, tests, CI | [`docker-compose.yaml`](./docker-compose.yaml) | The same four, plus an OTel Collector |

## Local backing services

```bash
make infra-up        # or `make dev-up`, which also starts the planes
```

| Service | Address | Notes |
|---|---|---|
| MongoDB 7 | `localhost:27017` | single-node replica set `rs0`, user `admin` / `password`; connect with `directConnection=true` |
| Redis | `localhost:6379` | serving cache, sessions, JWK store |
| NATS JetStream | `nats://localhost:4222` (monitoring `:8222`) | event bus; data persisted in a volume |
| Vault | `http://localhost:8200` | persistent file storage; initialises, unseals and provisions itself on every start (`vault/local-entrypoint.sh`): Transit key `sarvex-jwk-signing`, KV v2 at `secret/`, fixed token `sarvex-dev-token` |
| OTel Collector | `:4317` (gRPC), `:4318` (HTTP) | prints traces and metrics |

The Vault token is a root-policy token with a fixed id for **local use only**.
Production uses the Vault Agent Injector with the Kubernetes auth method
(see `selfhost/helm`).

Stop everything with `docker compose -f deployments/docker-compose.yaml down`
(add `-v` to delete the data volumes too).

## Configuration

Each plane reads `configs/<plane>/configs.<env>.json`; any field with an
`env:"NAME"` tag in `internal/pkg/utils/configurations.go` is overridden by the
environment variable of that name. The local defaults (`NATS_URL`,
`VAULT_ADDR`, `VAULT_TOKEN`, `VAULT_TRANSIT_KEY_NAME`) match the compose file,
so tests and the dev stack need no setup. In the cluster the charts inject the
same variables.

## Container image

`build/package/Dockerfile` builds the single binary (`--service` selects the
plane). `.github/workflows/image.yml` publishes it to
`ghcr.io/sarvexlabs/sarvex.cp`.

`otel-collector-config.yaml` configures the local collector.
