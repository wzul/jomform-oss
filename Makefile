SHELL := /bin/bash
export PATH := /usr/local/go/bin:$(PATH)

GO      ?= go
API_DIR := api

.PHONY: help build test lint dev up down migrate-new migrate-up

help: ## Senarai target
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build jomform-api binary
	cd $(API_DIR) && $(GO) build -o bin/jomform-api ./cmd/jomform-api

test: ## Run all Go tests
	cd $(API_DIR) && $(GO) test ./... -race

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

.PHONY: help build test lint tidy dev up down migrate-new