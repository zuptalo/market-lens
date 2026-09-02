SHELL := /bin/bash
SERVER_DIR := server
GOBIN := $(shell go env GOPATH)/bin
AIR := $(GOBIN)/air

.PHONY: start db-up db-down backend frontend cli tools test build verify release-policy spec

# One environment file, at the repository root, because that is the only place Docker Compose
# will read one from. Every make target loads the same file, so `docker compose up`,
# `make start`, `make backend` and `make cli` see identical configuration. Three targets
# previously disagreed about where it lived — and `make backend` read none at all, which is how
# a CLI command could refuse to start for a missing key that was sitting on disk.
ENV_FILE := .env
LOAD_ENV := set -a; [ -f $(CURDIR)/$(ENV_FILE) ] && . $(CURDIR)/$(ENV_FILE); set +a

start: db-up tools
	@$(LOAD_ENV); scripts/dev-ports.sh "$${PORT:-8080}" 5173
	@echo "Starting Market Lens backend and frontend; Ctrl+C stops both"
	@trap 'kill 0' INT TERM EXIT; \
		( $(LOAD_ENV); cd $(SERVER_DIR) && $(AIR) ) & \
		( npm run dev ) & \
		wait

db-up:
	docker compose up -d db

db-down:
	docker compose down

backend:
	@$(LOAD_ENV); cd $(SERVER_DIR) && go run ./cmd/market-lens

# One owner command against the local database, with the same environment the server gets:
#   make cli ARGS="features compute --universe nordic-liquid-v1"
#   make cli ARGS="signals compute --universe nordic-liquid-v1"
cli:
	@$(LOAD_ENV); cd $(SERVER_DIR) && go run ./cmd/market-lens $(ARGS)

frontend:
	npm run dev

tools: $(AIR)

$(AIR):
	go install github.com/air-verse/air@latest

test:
	$(MAKE) release-policy
	scripts/dev-ports.test.sh
	cd $(SERVER_DIR) && go test ./...
	npm run test:unit

release-policy:
	scripts/release-version.test.sh
	scripts/workflow-contract.test.sh

build:
	cd $(SERVER_DIR) && go build ./...
	npm run build

verify:
	$(MAKE) release-policy
	scripts/dev-ports.test.sh
	cd $(SERVER_DIR) && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...
	npm run build
	npm run test:unit

spec:
	@scripts/spec-new.sh "$(DESC)"
