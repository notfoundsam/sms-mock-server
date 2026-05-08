# SMS Mock Server - Makefile
# Usage: make <target>

.PHONY: build test test-race lint run tidy version docker-build \
        install up stop restart seed clean logs help

# Default target
.DEFAULT_GOAL := help

# Configuration
COMPOSE = docker compose
SERVICE = sms-mock-server

# Go build configuration
BIN_DIR ?= bin
BIN ?= $(BIN_DIR)/sms-mock-server

# Build revision: short git hash + UTC timestamp, e.g. "abc1234-20260508T132045"
# Falls back to "dev" outside a git checkout. Injected into httpapi.version
# so it's visible in /health responses and in `--version`-style logs.
GIT_REV ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
GIT_DIRTY ?= $(shell git diff --quiet 2>/dev/null || echo "-dirty")
BUILD_TS ?= $(shell date -u +%Y%m%dT%H%M%S)
VERSION ?= $(GIT_REV)$(GIT_DIRTY)-$(BUILD_TS)
LDFLAGS ?= -s -w -X github.com/notfoundsam/sms-mock-server/app/httpapi.version=$(VERSION)

# --- Go targets ---

## build: Build the Go binary into ./bin/sms-mock-server (static, CGO_ENABLED=0)
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./app
	@echo "Built $(BIN)"
	@du -h $(BIN)

## test: Run all Go unit tests
test:
	go test ./...

## test-race: Run all Go unit tests with the race detector
test-race:
	go test -race ./...

## lint: Run golangci-lint on all packages (requires golangci-lint installed)
lint:
	golangci-lint run ./...

## run: Run the server directly (uses local config.yaml)
run:
	go run ./app -config config.yaml

## tidy: Tidy go.mod (remove unused deps, fetch missing)
tidy:
	go mod tidy

## version: Print the build version that `make build` would stamp into the binary
version:
	@echo "$(VERSION)"

# --- Docker / orchestration ---

## docker-build: Build the Docker image
docker-build:
	docker build -t sms-mock-server:latest --build-arg VERSION=$(VERSION) .
	@docker images sms-mock-server:latest

## install: Build Docker image and start the application
install:
	$(COMPOSE) build
	$(COMPOSE) up -d
	@echo "Waiting for container to be ready..."
	@sleep 2
	@echo "SMS Mock Server is running at http://localhost:8080"

## up: Start the application via docker compose
up:
	$(COMPOSE) up -d
	@echo "SMS Mock Server is running at http://localhost:8080"

## stop: Stop the application
stop:
	$(COMPOSE) stop

## restart: Restart the application
restart:
	$(COMPOSE) restart $(SERVICE)

## clean: Stop containers and remove volumes
clean:
	$(COMPOSE) down -v
	@echo "Containers stopped and volumes removed"

## logs: Tail application logs
logs:
	$(COMPOSE) logs -f $(SERVICE)

# --- Data helpers ---

## seed: Seed database with sample messages and calls (against running server on :8080)
seed:
	@./scripts/seed_data.sh

## help: Show this help message
help:
	@echo "SMS Mock Server - Available Commands:"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
