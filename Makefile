# zhim-back: Go API + Telegram bot, OpenAPI contract and infra.
# Expects a zhim-front checkout next to this repo (override with FRONT_DIR=...).

FRONT_DIR ?= ../zhim-front
export FRONT_DIR := $(abspath $(FRONT_DIR))

COMPOSE := docker compose --env-file .env -f deploy/docker-compose.dev.yml
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
SQLC_VERSION := 1.31.1
SQLC := docker run --rm -v "$(CURDIR)/backend:/src" -w /src -u "$$(id -u):$$(id -g)" sqlc/sqlc:$(SQLC_VERSION)
# Host-side commands (migrate, seed) read .env; DATABASE_URL there points at localhost.
WITH_ENV := set -a && . ./.env && set +a &&

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.env:
	@echo "Missing .env — run: cp .env.example .env (then fill in secrets)"; exit 1

# ---------- Local stack ----------

.PHONY: dev
dev: .env ## Start everything locally with hot reload (foreground)
	@test -d "$(FRONT_DIR)/web" || { echo "zhim-front not found at $(FRONT_DIR) (set FRONT_DIR=...)"; exit 1; }
	$(COMPOSE) up --build --renew-anon-volumes

.PHONY: up
up: .env ## Same as dev, but detached
	$(COMPOSE) up --build --renew-anon-volumes --detach

.PHONY: down
down: ## Stop the local stack (data volumes are kept)
	$(COMPOSE) down

.PHONY: logs
logs: ## Follow logs (s=service to filter)
	$(COMPOSE) logs -f $(s)

.PHONY: ps
ps: ## List running services
	$(COMPOSE) ps

# ---------- Database ----------

# Postgres must be running (`make up`).

.PHONY: migrate
migrate: .env ## Apply all pending DB migrations
	$(WITH_ENV) cd backend && go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: .env ## Roll back the last migration
	$(WITH_ENV) cd backend && go run ./cmd/migrate down

.PHONY: migrate-status
migrate-status: .env ## Show applied and pending migrations
	$(WITH_ENV) cd backend && go run ./cmd/migrate status

.PHONY: migrate-new
migrate-new: ## Create a new migration: make migrate-new name=add_products
	@test -n "$(name)" || { echo "usage: make migrate-new name=<migration_name>"; exit 1; }
	cd backend && go run ./cmd/migrate create $(name)

.PHONY: seed
seed: .env ## Load test data (idempotent; resets seeded rows to seed values)
	$(WITH_ENV) cd backend && go run ./cmd/seed

# ---------- Codegen ----------

.PHONY: gen
gen: ## Generate sqlc + oapi-codegen + TS clients
	@echo "==> sqlc"
	$(SQLC) generate
	@echo "==> oapi-codegen"
	cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
	@if [ -d "$(FRONT_DIR)" ]; then \
		echo "==> TS clients ($(FRONT_DIR))"; \
		$(MAKE) -C "$(FRONT_DIR)" gen OPENAPI_SPEC="$(CURDIR)/api/openapi.yaml"; \
	else \
		echo "==> skip TS clients: $(FRONT_DIR) not found"; \
	fi

# ---------- Quality ----------

.PHONY: test
test: ## Run Go tests, including repository tests on testcontainers (needs Docker)
	cd backend && go test -race ./...

.PHONY: test-unit
test-unit: ## Run Go tests without Docker (skips integration tests)
	cd backend && go test -race -short ./...

.PHONY: lint
lint: ## golangci-lint + frontend lint (if zhim-front is present)
	cd backend && $(GOLANGCI_LINT) run ./...
	@if [ -d "$(FRONT_DIR)" ]; then $(MAKE) -C "$(FRONT_DIR)" lint; else echo "==> skip frontend lint: $(FRONT_DIR) not found"; fi

.PHONY: fmt
fmt: ## Format Go code
	cd backend && $(GOLANGCI_LINT) fmt ./...
