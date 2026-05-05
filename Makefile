.PHONY: dev dev-logs dev-down dev-build \
       stage stage-logs stage-down stage-restart stage-build \
       prod prod-logs prod-down prod-restart prod-build \
       test lint fmt \
       native-setup native-build native-run native-dev native-clean native-test native-test-cover native-lint \
       native-setup-prod native-run-prod native-install-prod \
       native-setup-stage native-run-stage native-install-stage \
       clean help

# ---------------------------------------------------------------------------
# Compose file arguments
# ---------------------------------------------------------------------------
DEV_COMPOSE  := -f docker-compose.yml
STAGE_COMPOSE := -f docker-compose.yml -f docker-compose.staging.yml
PROD_COMPOSE  := -f docker-compose.yml -f docker-compose.prod.yml

# ---------------------------------------------------------------------------
# Dev (base docker-compose.yml)
# ---------------------------------------------------------------------------
dev:
	docker compose $(DEV_COMPOSE) up --build

dev-build-d:
	docker compose $(DEV_COMPOSE) up --build -d

dev-d:
	docker compose $(DEV_COMPOSE) up -d

dev-logs:
	docker compose $(DEV_COMPOSE) logs -f nano-review

dev-down:
	docker compose $(DEV_COMPOSE) down

dev-restart:
	docker compose $(DEV_COMPOSE) restart

dev-build:
	docker compose $(DEV_COMPOSE) build --no-cache

# ---------------------------------------------------------------------------
# Staging (base + staging overlay)
# ---------------------------------------------------------------------------
stage:
	docker compose $(STAGE_COMPOSE) up --build -d

stage-logs:
	docker compose $(STAGE_COMPOSE) logs -f nano-review

stage-down:
	docker compose $(STAGE_COMPOSE) down

stage-restart:
	docker compose $(STAGE_COMPOSE) restart

stage-build:
	docker compose $(STAGE_COMPOSE) build --no-cache

# ---------------------------------------------------------------------------
# Prod (base + prod overlay)
# ---------------------------------------------------------------------------
prod:
	docker compose $(PROD_COMPOSE) up --build -d

prod-logs:
	docker compose $(PROD_COMPOSE) logs -f nano-review

prod-down:
	docker compose $(PROD_COMPOSE) down

prod-restart:
	docker compose $(PROD_COMPOSE) restart

prod-build:
	docker compose $(PROD_COMPOSE) build --no-cache

# ---------------------------------------------------------------------------
# Local development (no Docker)
# ---------------------------------------------------------------------------
test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...
	go fmt ./...

fmt:
	go fmt ./...

# ---------------------------------------------------------------------------
# Native (no Docker)
# ---------------------------------------------------------------------------
NATIVE_BIN := ./bin/nano-review

native-setup: ## First-time native dev setup
	@bash scripts/setup-native.sh

native-build: ## Build binary locally
	@mkdir -p bin
	CGO_ENABLED=0 go build -o $(NATIVE_BIN) ./cmd/server

native-run: native-build ## Build and run natively
	@bash scripts/run-native.sh

native-dev: ## Run natively with auto-rebuild (requires air)
	@which air > /dev/null 2>&1 || (echo "Install air: go install github.com/air-verse/air@latest" && exit 1)
	air -c .air.toml

native-clean: ## Remove native build artifacts
	rm -rf bin/ data/ logs/

native-test: ## Run tests natively
	go test -race ./...

native-test-cover: ## Run tests with coverage natively
	go test -race -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html

native-lint: ## Lint natively
	golangci-lint run ./...
	go fmt ./...

# ---------------------------------------------------------------------------
# Native production (no Docker)
# ---------------------------------------------------------------------------
native-setup-prod: ## First-time native prod setup (generates .env.prod)
	@bash scripts/setup-native-prod.sh

native-run-prod: native-build ## Build and run natively with .env.prod
	@bash scripts/run-native-prod.sh

native-install-prod: native-build ## Install systemd service for production
	@bash scripts/install-systemd-prod.sh

# ---------------------------------------------------------------------------
# Native staging (no Docker)
# ---------------------------------------------------------------------------
native-setup-stage: ## First-time native staging setup (generates .env.stage)
	@bash scripts/setup-native-stage.sh

native-run-stage: native-build ## Build and run natively with .env.stage
	@bash scripts/run-native-stage.sh

native-install-stage: native-build ## Install systemd service for staging
	@bash scripts/install-systemd-stage.sh

# ---------------------------------------------------------------------------
# Utilities
# ---------------------------------------------------------------------------
clean:
	docker compose $(DEV_COMPOSE) down -v --rmi local

ps:
	docker compose ps

help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "  Dev commands:"
	@echo "    dev             Build and run (foreground)"
	@echo "    dev-d           Build and run (detached)"
	@echo "    dev-logs        Tail container logs"
	@echo "    dev-down        Stop and remove containers"
	@echo "    dev-restart     Restart containers"
	@echo "    dev-build       Rebuild image (no cache)"
	@echo ""
	@echo "  Staging commands:"
	@echo "    stage           Build, run, detach"
	@echo "    stage-logs      Tail container logs"
	@echo "    stage-down      Stop and remove containers"
	@echo "    stage-restart   Restart containers"
	@echo "    stage-build     Rebuild image (no cache)"
	@echo ""
	@echo "  Prod commands:"
	@echo "    prod            Build, run, detach"
	@echo "    prod-logs       Tail container logs"
	@echo "    prod-down       Stop and remove containers"
	@echo "    prod-restart    Restart containers"
	@echo "    prod-build      Rebuild image (no cache)"
	@echo ""
	@echo "  Local development:"
	@echo "    test            Run tests with race detector"
	@echo "    test-cover      Run tests with HTML coverage report"
	@echo "    lint            golangci-lint and format code"
	@echo "    fmt             Format code"
	@echo ""
	@echo "  Native commands:"
	@echo "    native-setup    First-time native dev setup"
	@echo "    native-build    Build binary locally"
	@echo "    native-run      Build and run natively"
	@echo "    native-dev      Run with auto-rebuild (requires air)"
	@echo "    native-clean    Remove native build artifacts"
	@echo "    native-test     Run tests natively"
	@echo "    native-test-cover  Run tests with coverage natively"
	@echo "    native-lint     Lint natively"
	@echo ""
	@echo "  Native production:"
	@echo "    native-setup-prod    First-time native prod setup (.env.prod)"
	@echo "    native-run-prod      Build and run with .env.prod"
	@echo "    native-install-prod  Install systemd service for production"
	@echo ""
	@echo "  Native staging:"
	@echo "    native-setup-stage   First-time native staging setup (.env.stage)"
	@echo "    native-run-stage     Build and run with .env.stage"
	@echo "    native-install-stage Install systemd service for staging"
	@echo ""
	@echo "  Utilities:"
	@echo "    clean           Remove containers, volumes, and local images"
	@echo "    ps              Show running containers"
	@echo "    help            Show this message"
