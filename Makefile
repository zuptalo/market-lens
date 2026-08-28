SHELL := /bin/bash
SERVER_DIR := server
GOBIN := $(shell go env GOPATH)/bin
AIR := $(GOBIN)/air

.PHONY: start db-up db-down backend frontend tools test build verify release-policy spec

start: db-up tools
	@echo "Starting Market Lens backend and frontend; Ctrl+C stops both"
	@trap 'kill 0' INT TERM EXIT; \
		( cd $(SERVER_DIR) && set -a && { [ -f .env ] && . ./.env; }; set +a; $(AIR) ) & \
		( npm run dev ) & \
		wait

db-up:
	docker compose up -d db

db-down:
	docker compose down

backend:
	cd $(SERVER_DIR) && go run ./cmd/market-lens

frontend:
	npm run dev

tools: $(AIR)

$(AIR):
	go install github.com/air-verse/air@latest

test:
	$(MAKE) release-policy
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
	cd $(SERVER_DIR) && test -z "$$(gofmt -l .)" && go vet ./... && go test ./...
	npm run build
	npm run test:unit

spec:
	@scripts/spec-new.sh "$(DESC)"
