# SMS Mock Server - Makefile
# Usage: make <target>

.PHONY: install up stop restart test lint fix build-assets seed clean logs help

# Default target
.DEFAULT_GOAL := help

# Configuration
COMPOSE = docker compose
SERVICE = sms-mock-server

## install: Build Docker image and start the application
install:
	$(COMPOSE) build
	$(COMPOSE) up -d
	@echo "Waiting for container to be ready..."
	@sleep 2
	@echo "SMS Mock Server is running at http://localhost:8080"

## up: Start the application
up:
	$(COMPOSE) up -d
	@echo "SMS Mock Server is running at http://localhost:8080"

## stop: Stop the application
stop:
	$(COMPOSE) down

## restart: Restart the application
restart:
	$(COMPOSE) restart $(SERVICE)

## test: Run tests inside container
test:
	$(COMPOSE) exec $(SERVICE) sh -c "pip install -q --root-user-action=ignore -r requirements-dev.txt && pytest tests/"

## lint: Run Ruff linter and format check
lint:
	$(COMPOSE) exec $(SERVICE) sh -c "pip install -q --root-user-action=ignore ruff==0.8.6 && ruff check app/ tests/ && ruff format --check app/ tests/"

## fix: Auto-fix lint issues and format code
fix:
	$(COMPOSE) exec $(SERVICE) sh -c "pip install -q --root-user-action=ignore ruff==0.8.6 && ruff check --fix app/ tests/ && ruff format app/ tests/"

## build-assets: Minify CSS/JS and generate manifest with content hashes
build-assets:
	$(COMPOSE) exec $(SERVICE) python scripts/build_assets.py

## seed: Seed database with sample messages and calls
seed:
	@./scripts/seed_data.sh

## clean: Stop containers and remove volumes
clean:
	$(COMPOSE) down -v
	@echo "Containers stopped and volumes removed"

## logs: Show application logs
logs:
	$(COMPOSE) logs -f $(SERVICE)

## help: Show this help message
help:
	@echo "SMS Mock Server - Available Commands:"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
