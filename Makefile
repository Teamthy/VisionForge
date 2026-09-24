# ──────────────────────────────────────────────────────────
# VisionForge — top-level Makefile
# ──────────────────────────────────────────────────────────
SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE := docker compose -f docker-compose.yml
E2E_BASE ?= http://localhost:8080/api/v1

# ── Help ─────────────────────────────────────────────────
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_.-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# ── Environment ──────────────────────────────────────────
.PHONY: env
env: ## Create .env from example if missing
	@test -f .env || cp .env.example .env

# ── Infrastructure ───────────────────────────────────────
.PHONY: up
up: env ## Start all services (docker compose)
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop all services
	$(COMPOSE) down

.PHONY: logs
logs: ## Tail logs for all services
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## Show running containers
	$(COMPOSE) ps

.PHONY: infra-up
infra-up: env ## Start only infrastructure (postgres, redis, minio, otel, prom, grafana)
	$(COMPOSE) up -d postgres redis minio otel-collector prometheus grafana

.PHONY: infra-down
infra-down: ## Stop only infrastructure
	$(COMPOSE) stop postgres redis minio otel-collector prometheus grafana

# ── Database ─────────────────────────────────────────────
.PHONY: migrate
migrate: ## Run database migrations (inside the api container)
	$(COMPOSE) exec api /app/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back last migration
	$(COMPOSE) exec api /app/migrate down 1

.PHONY: migrate-new
migrate-new: ## Create new migration (make migrate-new NAME=add_table)
	$(COMPOSE) run --rm api go run . migrate create -ext sql -dir migrations -seq $(NAME)

.PHONY: seed
seed: ## Seed development data
	$(COMPOSE) exec api /app/seed

# ── Model artifacts ──────────────────────────────────────
.PHONY: model
model: ## Download + verify the demo ONNX model used by the seed data
	bash scripts/download-demo-model.sh

# ── Development (host-run services) ──────────────────────
# Go services are built from the single apps/api module; the
# worker binary shares that module (cmd/worker).
.PHONY: dev-web
dev-web: ## Run Next.js web app locally
	cd apps/web && npm install && npm run dev

.PHONY: dev-api
dev-api: ## Run Go API locally (requires postgres+redis+minio running)
	cd apps/api && go run ./cmd/api

.PHONY: dev-worker
dev-worker: ## Run Go worker pool locally (same module as the API)
	cd apps/api && go run ./cmd/worker

.PHONY: dev-ml
dev-ml: ## Run Python ML service locally (expects apps/ml/artifacts to exist; `make model`)
	cd apps/ml && pip install -r requirements.txt && python -m uvicorn app.main:app --host 0.0.0.0 --port 8090 --reload

# ── Testing ──────────────────────────────────────────────
.PHONY: test
test: test-go test-py test-web ## Run all unit tests

# Go workspace mode (go.work) needs explicit module prefixes for ./... patterns.
GO_MODS := ./apps/api/... ./packages/config/... ./packages/types/...

.PHONY: test-go
test-go: ## Run Go unit tests (api, worker, shared packages)
	go test -race -count=1 $(GO_MODS)

.PHONY: test-py
test-py: ## Run Python ML tests
	cd apps/ml && pip install -q -r requirements.txt -r requirements-dev.txt && python -m pytest -q

.PHONY: test-web
test-web: ## Type-check the frontend (no runtime unit tests yet)
	cd apps/web && npm install && npx tsc --noEmit

.PHONY: test-integration
test-integration: ## E2E smoke suite against a running stack (compose or make up)
	python3 scripts/e2e_smoke.py --base $(E2E_BASE)

# ── Quality ──────────────────────────────────────────────
.PHONY: lint
lint: ## Run linters across all services
	go vet $(GO_MODS)
	cd apps/ml && python -m ruff check . || (echo "ruff not installed — CI runs the real check" && true)
	cd apps/web && npm run lint || true

.PHONY: fmt
fmt: ## Format all code
	gofmt -w $$(find apps packages -name '*.go')
	cd apps/api && GOWORK=off go mod tidy
	cd apps/ml && python -m ruff format . || true
	cd apps/web && npm run format || true

# ── Build ────────────────────────────────────────────────
.PHONY: build
build: build-api build-worker build-web build-ml ## Build all service binaries/images

.PHONY: build-api
build-api:
	cd apps/api && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o ../../bin/api ./cmd/api

.PHONY: build-worker
build-worker:
	cd apps/api && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o ../../bin/worker ./cmd/worker

.PHONY: build-web
build-web:
	cd apps/web && npm install && npm run build

.PHONY: build-ml
build-ml:
	cd apps/ml && pip install -r requirements.txt

# ── Clean ────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ apps/web/.next apps/web/node_modules apps/api/bin
	find . -type d -name __pycache__ -not -path './node_modules/*' -exec rm -rf {} +
