SHELL := /bin/bash
export PATH := /usr/local/go/bin:$(PATH)

GO      ?= go
API_DIR := api

.PHONY: help build test test-integration lint dev up down migrate-new preflight preflight-selftest

help: ## Senarai target
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build jomform-api binary
	cd $(API_DIR) && $(GO) build -o bin/jomform-api ./cmd/jomform-api

test: ## Run all Go tests (unit + integration; integration skips without env)
	cd $(API_DIR) && $(GO) test ./... -race

test-integration: ## Run the full suite incl. integration tests against real PG+Valkey
	cd $(API_DIR) && JOMFORM_TEST_DATABASE_URL="$${JOMFORM_TEST_DATABASE_URL:-postgres://jomform:jomform_dev@localhost:5432/jomform_test?sslmode=disable}" \
		JOMFORM_TEST_VALKEY_URL="$${JOMFORM_TEST_VALKEY_URL:-valkey://localhost:6379/1}" \
		$(GO) test ./... -race

lint: ## go vet
	cd $(API_DIR) && $(GO) vet ./...

tidy: ## go mod tidy
	cd $(API_DIR) && $(GO) mod tidy

dev: ## Run API locally (needs compose deps)
	cd $(API_DIR) && $(GO) run ./cmd/jomform-api

up: ## Start dev services (PG, Valkey, MinIO)
	docker compose -f deploy/docker-compose.yml up -d

down: ## Stop dev services
	docker compose -f deploy/docker-compose.yml down

migrate-new: ## Usage: make migrate-new NAME=create_users
	cd $(API_DIR) && migrate create -ext sql -dir internal/db/migrations -seq "$(NAME)"

preflight: ## Run the local gate — the ONLY gate (no CI for this repo)
	./scripts/preflight.sh

preflight-selftest: ## Run the gate runner's own safety self-test (also gate `selftest`)
	./scripts/preflight_test.sh

.PHONY: help build test test-integration lint tidy dev up down migrate-new migrate-up preflight preflight-selftest