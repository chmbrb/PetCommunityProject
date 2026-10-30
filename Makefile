# SPDX-License-Identifier: AGPL-3.0-or-later
# Common development tasks. See docs/DEVELOPMENT.md.

COMPOSE ?= docker compose
DEV_COMPOSE = $(COMPOSE) -f compose.dev.yaml
LOCAL_COMPOSE = $(COMPOSE) -p petcommunity-local --env-file deploy/local/local.env -f compose.yaml -f compose.local.yaml
GO_TOOL = go tool -modfile=tools/go.mod
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

export PC_DATABASE_URL ?= postgres://petcommunity:dev-only-password@localhost:5432/petcommunity?sslmode=disable
TEST_DATABASE_URL ?= postgres://petcommunity:dev-only-password@localhost:5432/petcommunity_test?sslmode=disable

.PHONY: help dev-up dev-down dev-reset migrate run worker web local-certs local-up local-down local-reset local-logs generate test test-go test-integration test-web lint vuln images

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

dev-up: ## Start Postgres and MinIO for development
	$(DEV_COMPOSE) up -d

dev-down: ## Stop development services (keeps data)
	$(DEV_COMPOSE) down

dev-reset: ## Stop development services and delete their data
	$(DEV_COMPOSE) down -v

migrate: ## Apply database migrations to the development database
	go run ./cmd/petcommunity migrate

run: ## Run the API on :8080
	go run ./cmd/petcommunity serve

worker: ## Run background jobs (sends email to Mailpit in development)
	go run ./cmd/petcommunity worker

web: ## Run the web app with hot reload on :5173
	cd web && pnpm dev

generate: ## Regenerate Go code from SQL (sqlc)
	$(GO_TOOL) sqlc generate

test: test-go test-web ## Run unit tests

test-go:
	go test -race ./...

test-integration: ## Run Go tests including database integration tests
	PC_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race ./...

test-web:
	cd web && pnpm test

lint: ## Check formatting, vet, generated code, and web lint
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt: files need formatting" && exit 1)
	go vet ./...
	$(GO_TOOL) sqlc diff
	cd web && pnpm lint && pnpm typecheck

vuln: ## Scan Go dependencies for known vulnerabilities
	$(GO_TOOL) govulncheck ./...

local-certs: ## Create the throwaway certificates for the local production stack
	deploy/local/make-certs.sh

local-up: local-certs ## Build and run the production stack locally (https://localhost:8443, mail at :8026)
	$(LOCAL_COMPOSE) up -d --build

local-down: ## Stop the local production stack (keeps data)
	$(LOCAL_COMPOSE) down

local-reset: ## Stop the local production stack and delete its data
	$(LOCAL_COMPOSE) down -v

local-logs: ## Follow the local production stack's logs
	$(LOCAL_COMPOSE) logs -f

images: ## Build the app and web container images
	docker build --target app --build-arg VERSION=$(VERSION) -t petcommunity-app:local .
	docker build --target web -t petcommunity-web:local .
