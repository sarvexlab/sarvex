# Sarvex V1 Backend Makefile
# Targets for building, testing, and running services

BLACK        := $(shell tput -Txterm setaf 0)
RED          := $(shell tput -Txterm setaf 1)
GREEN        := $(shell tput -Txterm setaf 2)
YELLOW       := $(shell tput -Txterm setaf 3)
LIGHTPURPLE  := $(shell tput -Txterm setaf 4)
PURPLE       := $(shell tput -Txterm setaf 5)
BLUE         := $(shell tput -Txterm setaf 6)
WHITE        := $(shell tput -Txterm setaf 7)

RESET := $(shell tput -Txterm sgr0)

# set target color
TARGET_COLOR := $(BLUE)

.PHONY: help test new-spec-version new-platform test-unit test-scenarios test-integration test-integration-control test-integration-sync test-integration-serving test-e2e test-e2e-journey test-conformance test-conformance-official check-routes infra-up dev-up dev-up-monolith dev-down dev-status dev-token dev-db-reset console console-install console-real docs-install docs docs-build docs-preview docs-check loadtest-seed loadtest-control loadtest-serving selfhost-stage0 selfhost-stage1 selfhost-add-region

help: ## Show this help message
	@echo "${GREEN}Sarvex V1 Backend - Available Targets${RESET}"
	@echo ""
	@echo "${TARGET_COLOR}Local dev stack (Mongo/Redis/NATS/Vault + control + sync + serving + fake WooCommerce store):${RESET}"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  make %-30s %s\n", $$1, $$2}' | grep -E "^  make dev-"
	@echo ""
	@echo "${TARGET_COLOR}Testing:${RESET}"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  make %-30s %s\n", $$1, $$2}' | grep -E "test"
	@echo ""
	@echo "${TARGET_COLOR}Services:${RESET}"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  make %-30s %s\n", $$1, $$2}' | grep -vE "test|^  make dev-"
	@echo ""
	@echo "${YELLOW}Test Prerequisites:${RESET}"
	@echo "  - Mongo, Redis, NATS JetStream and Vault: the same services a self-hosted deployment runs."
	@echo "    Start them all: make infra-up   (docker compose, deployments/docker-compose.yaml)"
	@echo ""
	@echo "${YELLOW}Environment Variables (defaults match make infra-up):${RESET}"
	@echo "  - MONGO_URI: MongoDB connection string (default: mongodb://admin:password@localhost:27017/?directConnection=true)"
	@echo "  - REDIS_ADDR: Redis connection address (default: localhost:6379)"
	@echo "  - NATS_URL: NATS JetStream address (default: nats://localhost:4222)"
	@echo "  - VAULT_ADDR / VAULT_TOKEN: Vault address and token (default: http://localhost:8200 / sarvex-dev-token)"

install-tools:
	go install github.com/vektra/mockery/v2@v2.20.0

generate:
	go generate ./...
colors: ## - Show all the colors
	@echo "${BLACK}BLACK${RESET}"
	@echo "${RED}RED${RESET}"
	@echo "${GREEN}GREEN${RESET}"
	@echo "${YELLOW}YELLOW${RESET}"
	@echo "${LIGHTPURPLE}LIGHTPURPLE${RESET}"
	@echo "${PURPLE}PURPLE${RESET}"
	@echo "${BLUE}BLUE${RESET}"
	@echo "${WHITE}WHITE${RESET}"

infra-up: ## Start only the backing services (Mongo, Redis, NATS JetStream, Vault, otel-collector) via docker compose
	@bash scripts/dev/infra-up.sh

dev-up: ## Start Mongo+Redis+otel-collector, cmd/sim serve, control plane, sync plane, and serving plane (one process each) for local end-to-end testing
	@bash scripts/dev/up.sh

dev-up-monolith: ## Same as dev-up, but serving+control+sync run as one sarvex --service=all process instead of three
	@bash scripts/dev/up-monolith.sh

dev-down: ## Stop the local dev stack started by `make dev-up` or `make dev-up-monolith` (leaves Mongo/Redis running)
	@bash scripts/dev/down.sh

dev-status: ## Show what's running in the local dev stack
	@bash scripts/dev/status.sh

dev-db-reset: ## Drop all local dev DB collections except the merchant/account data (control_accounts)
	@bash scripts/dev/reset-db.sh

dev-token: ## Mint a dev-mode bearer token for curl testing (KINDE_ISSUER_URL unset only)
	@bash scripts/dev/token.sh $(ARGS)

selfhost-stage0: ## Stand up the self-hosted stack locally in k3d (Stage 0 — see deployments/selfhost/README.md); requires k3d/helm/cilium CLIs
	@k3d cluster create sarvex-local --api-port 6550 -p "80:80@loadbalancer" || true
	@bash deployments/selfhost/scripts/bootstrap-cluster.sh local core
	@helm upgrade --install sarvex-gateway deployments/selfhost/helm/sarvex-gateway -f deployments/selfhost/helm/sarvex-gateway/values-local.yaml -n sarvex-core --create-namespace
	@helm upgrade --install sarvex-serving deployments/selfhost/helm/sarvex-serving -f deployments/selfhost/helm/sarvex-serving/values-local.yaml -n sarvex-core
	@helm upgrade --install sarvex-control deployments/selfhost/helm/sarvex-control -f deployments/selfhost/helm/sarvex-control/values-local.yaml -n sarvex-core
	@helm upgrade --install sarvex-sync deployments/selfhost/helm/sarvex-sync -f deployments/selfhost/helm/sarvex-sync/values-local.yaml -n sarvex-core

selfhost-stage1: ## Harden the CORE region to production posture (Vault init/unseal, Argo CD, GitOps) — run against a real multi-node k3s cluster, not k3d
	@bash deployments/selfhost/scripts/bootstrap-cluster.sh core core
	@bash deployments/selfhost/scripts/bootstrap-argocd.sh core
	@echo "${TARGET_COLOR}Next: run \`terraform apply\` in deployments/selfhost/terraform/cloudflare (fill in regions.auto.tfvars.json's core origin IP first).${RESET}"

selfhost-add-region: ## Add a region on day N+1: make selfhost-add-region PROVIDER=... HOST=root@1.2.3.4 REGION=... ORIGIN_IP=... CORE_LEAF_URL=nats-leaf://...
	@bash deployments/selfhost/scripts/add-region.sh "$(PROVIDER)" "$(HOST)" "$(REGION)" "$(ORIGIN_IP)" "$(CORE_LEAF_URL)"

console-install: ## Install web console dependencies (npm)
	@cd web/console && npm install

console: console-install ## Start the web console dev server (Vite on :5173, mock-mode by default — no backend needed)
	@echo "${TARGET_COLOR} Console dev server starting on http://localhost:5173 (mock mode; see web/console/README.md to point it at a real backend)...${RESET}"
	@cd web/console && npm run dev

console-real: console-install ## Start the web console against the local real backend (run `make dev-up` first; proxies /api -> control plane on :8081)
	@echo "${TARGET_COLOR} Console dev server starting on http://localhost:5173, proxying /api -> http://localhost:8081/v1 (real backend — make sure \`make dev-up\` is running)...${RESET}"
	@cd web/console && VITE_USE_MOCKS=0 VITE_API_PROXY_TARGET=http://localhost:8081 npm run dev

docs-install: ## Install the documentation site's dependencies (npm)
	@cd web/docs && npm ci

docs: docs-install ## Start the docs site dev server with live reload (Blume on :4321) — guides, API reference, developer & contributor docs
	@echo "${TARGET_COLOR} Docs dev server starting (see the URL below; web/docs/README.md for how the site is assembled)...${RESET}"
	@cd web/docs && npm run dev

docs-build: docs-install ## Build the docs site as static HTML into web/docs/dist (nothing is deployed)
	@cd web/docs && npm run build

docs-preview: docs-build ## Serve the built docs site locally
	@cd web/docs && npm run preview

docs-check: docs-install ## Validate the docs site (Blume doctor) and lint the OpenAPI + AsyncAPI contracts it renders
	@cd web/docs && npm run doctor
	@npx --yes @redocly/cli@1 lint sarvex-v1-control-plane.yaml --format=summary
	@npx --yes @asyncapi/cli@2 validate sarvex-v1-syncing-plane-async.yaml

new-spec-version: ## Scaffold a UCP spec generation from the latest one: make new-spec-version V=YYYY-MM-DD (docs/HOWTO-add-spec-version.md)
	@test -n "$(V)" || (echo "usage: make new-spec-version V=YYYY-MM-DD" && exit 1)
	@go run ./cmd/scaffold spec-version "$(V)"

new-platform: ## Scaffold a backend platform from the default one: make new-platform P=name [D=DisplayName] (docs/HOWTO-add-platform.md)
	@test -n "$(P)" || (echo "usage: make new-platform P=name [D=DisplayName]" && exit 1)
	@go run ./cmd/scaffold platform -display "$(D)" "$(P)"

test-unit: ## Run unit tests only
	@echo "${TARGET_COLOR} Running unit tests...${RESET}"
	@go test -race ./internal/pkg/... ./internal/planes/control/... ./internal/planes/serving/... ./internal/planes/sync/... ./internal/testtenancy/... ./internal/archtest/... ./internal/platform/... ./internal/spec/... ./cmd/... -v -timeout 60s

test-scenarios: ## Run end-to-end scenario tests (all 7 flows)
	@echo "${TARGET_COLOR} Running scenario tests (E2E flows)...${RESET}"
	@go test -tags=scenarios ./internal/planes/scenarios -v -timeout 60s

test-integration-control: ## Run control plane integration tests (requires `make infra-up`)
	@echo "${TARGET_COLOR} Running control plane integration tests...${RESET}"
	@echo "${YELLOW}Requires: MongoDB running at $$MONGO_URI (default: mongodb://admin:password@localhost:27017/?directConnection=true)${RESET}"
	@REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} go test -race -tags=integration ./internal/planes/control/integration -v -timeout 60s

test-integration-sync: ## Run sync plane integration tests (requires `make infra-up`)
	@echo "${TARGET_COLOR} Running sync plane integration tests...${RESET}"
	@echo "${YELLOW}Requires: MongoDB at $$MONGO_URI (default: mongodb://admin:password@localhost:27017/?directConnection=true) and Redis at $$REDIS_ADDR (default: localhost:6379)${RESET}"
	@REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} go test -race -tags=integration ./internal/planes/sync/integration -v -timeout 60s

test-integration-serving: ## Run serving plane integration tests (requires `make infra-up`)
	@echo "${TARGET_COLOR} Running serving plane integration tests...${RESET}"
	@echo "${YELLOW}Requires: Redis at $$REDIS_ADDR (default: localhost:6379) and MongoDB at $$MONGO_URI${RESET}"
	@REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} go test -race -tags=integration ./internal/planes/serving/integration -v -timeout 60s

test-integration: test-integration-control test-integration-sync test-integration-serving ## Run all integration tests (control + sync + serving planes)

test-e2e-journey: ## Run full WooCommerce-to-agent E2E journey tests (requires `make infra-up`)
	@echo "${TARGET_COLOR} Running E2E journey tests (WooCommerce → agent serving)...${RESET}"
	@echo "${YELLOW}Requires: `make infra-up` (Mongo, Redis, NATS, Vault)${RESET}"
	@MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} go test -tags=e2e ./test/e2e/journey -v -timeout 120s

test-e2e: test-scenarios test-integration test-e2e-journey ## Run all end-to-end tests (scenarios + integration + journey)

test: test-unit test-e2e ## Run all tests (unit + E2E + integration)

test-conformance: ## Run the UCP conformance suite (onboards a merchant in-process, then drives the agent surface)
	@echo "${TARGET_COLOR} Running UCP conformance suite...${RESET}"
	@echo "${YELLOW}Requires: `make infra-up` (Mongo, Redis, NATS, Vault). Planes start in-process on OS-assigned ports.${RESET}"
	@MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} \
		REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} \
		go test -tags=conformance ./cmd/sarvex/conformance -v -timeout 15m

test-conformance-official: ## Run the OFFICIAL UCP conformance suite against an in-process Sarvex, for every UCP release we support
	@echo "${TARGET_COLOR} Running the official UCP conformance suite...${RESET}"
	@echo "${YELLOW}Requires: `make infra-up` (see test-conformance), plus Python 3.10+ and uv (https://docs.astral.sh/uv/).${RESET}"
	@MONGO_URI=$${MONGO_URI:-mongodb://admin:password@localhost:27017/?directConnection=true} \
		REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} \
		./scripts/ucp-conformance/run.sh

check-routes: ## Verify control-plane routes against OpenAPI spec (drift detection)
	@echo "${TARGET_COLOR} Verifying control-plane routes...${RESET}"
	@go run ./cmd/verify-routes/main.go

loadtest-seed: ## Provision k6 load-test fixtures (stores+catalog+pairing) against the running dev stack
	@echo "${TARGET_COLOR} Seeding load-test stores...${RESET}"
	@echo "${YELLOW}Requires: \`make dev-up\` already running. Env: LOADTEST_STORES (default 5), LOADTEST_PRODUCTS_PER_STORE (default 50)${RESET}"
	@bash scripts/loadtest/seed.sh

loadtest-control: ## Run control-plane k6 load tests (requires \`make loadtest-seed\` first). ARGS=scenario to run one (smoke|console_read|catalog_import|websocket_journey)
	@echo "${TARGET_COLOR} Running control-plane load tests...${RESET}"
	@echo "${YELLOW}Requires: \`make dev-up\` and \`make loadtest-seed\` already run; k6 installed${RESET}"
	@bash scripts/loadtest/run-control.sh $(ARGS)

loadtest-serving: ## Run serving-plane k6 load tests (requires \`make loadtest-seed\` first). ARGS=scenario to run one (smoke|catalog_read|checkout_flow|checkout_negative)
	@echo "${TARGET_COLOR} Running serving-plane load tests...${RESET}"
	@echo "${YELLOW}Requires: \`make dev-up\` and \`make loadtest-seed\` already run; k6 installed${RESET}"
	@bash scripts/loadtest/run-serving.sh $(ARGS)
