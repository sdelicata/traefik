# Traefik ext-proc Plugin POC Makefile

.PHONY: help build up down logs logs-traefik logs-extproc status test test-stress clean dev rebuild
.PHONY: traefik-build traefik-restart extproc-build extproc-restart shell-traefik shell-extproc urls
.PHONY: test-integration test-unit lint fmt validate

# Load environment variables
include .env
export

# Variables
COMPOSE_FILE := docker-compose.yml
OVERRIDE_FILE := docker-compose.override.yml
PROJECT_NAME := $(COMPOSE_PROJECT_NAME)

# Default target
.DEFAULT_GOAL := help

help: ## Show available commands
	@echo "🚀 Traefik ext-proc Plugin POC"
	@echo ""
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Quick start: make up && make test"

# Build and Environment Management
build: ## Build all Docker services
	@echo "🔨 Building Docker services..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) build
	@echo "✅ Build completed"

up: build ## Start the complete POC environment
	@echo "🚀 Starting POC environment..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) up -d
	@echo "⏳ Waiting for services to be ready..."
	@sleep 15
	@$(MAKE) status
	@echo ""
	@echo "🎉 POC environment is ready!"
	@$(MAKE) urls

down: ## Stop the POC environment  
	@echo "🛑 Stopping POC environment..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) down -v
	@echo "✅ Environment stopped"

restart: down up ## Restart the complete environment

# Logging and Monitoring
logs: ## Show logs from all services
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) logs -f

logs-traefik: ## Show Traefik logs only
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) logs -f traefik

logs-extproc: ## Show ext-proc server logs only  
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) logs -f extproc-plugin

status: ## Check service health and status
	@echo "📊 Service Status:"
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) ps
	@echo ""
	@echo "🔍 Health Checks:"
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec -T extproc-plugin grpc_health_probe -addr=:$(EXTPROC_GRPC_PORT) >/dev/null 2>&1 && \
		echo "✅ ext-proc gRPC server: OK" || echo "❌ ext-proc gRPC server: FAILED"
	@curl -s http://localhost:$(TRAEFIK_API_PORT)/ping >/dev/null 2>&1 && \
		echo "✅ Traefik API: OK" || echo "❌ Traefik API: FAILED"
	@curl -s http://localhost:$(TRAEFIK_API_PORT)/api/http/middlewares 2>/dev/null | jq -r '.[].name' 2>/dev/null | grep -q extproc && \
		echo "✅ ext-proc middleware: LOADED" || echo "❌ ext-proc middleware: NOT LOADED"

# Testing
test: ## Run functional tests for the POC
	@echo "🧪 Running POC functional tests..."
	@echo ""
	@echo "Test 1: Request with X-Request-Header"
	@curl -H "X-Request-Header: test-value" -H "Host: $(TEST_HOST)" -s http://localhost:$(TRAEFIK_HTTP_PORT)/ | head -5
	@echo ""
	@echo "Test 2: Checking for X-Response-Header in response"
	@curl -H "X-Request-Header: hello-world" -H "Host: $(TEST_HOST)" -I -s http://localhost:$(TRAEFIK_HTTP_PORT)/ | \
		grep -i "x-response-header:" && echo "✅ X-Response-Header found" || echo "❌ X-Response-Header not found"
	@echo ""
	@echo "Test 3: Request without X-Request-Header (should work normally)"
	@curl -H "Host: $(TEST_HOST)" -s http://localhost:$(TRAEFIK_HTTP_PORT)/ | head -3
	@echo ""
	@echo "Test 4: Traefik API accessibility"
	@curl -s http://localhost:$(TRAEFIK_API_PORT)/api/overview >/dev/null && \
		echo "✅ Traefik API accessible" || echo "❌ Traefik API not accessible"

test-stress: ## Run stress test with wrk
	@echo "🚀 Running stress test..."
	@echo "Duration: $(STRESS_TEST_DURATION), Connections: $(STRESS_TEST_CONNECTIONS)"
	@docker run --rm --network $(PROJECT_NAME)_default williamyeh/wrk \
		-t4 -c$(STRESS_TEST_CONNECTIONS) -d$(STRESS_TEST_DURATION) \
		-H "X-Request-Header: stress-test" -H "Host: $(TEST_HOST)" \
		http://traefik:$(TRAEFIK_HTTP_PORT)/

test-integration: ## Run integration tests
	@echo "🔬 Running integration tests..."
	@cd tests && go test -v ./...

test-unit: ## Run unit tests for all components
	@echo "🧪 Running unit tests..."
	@cd extproc-server && go test -v ./...
	@echo "Note: Traefik middleware unit tests require Traefik build"

# Development
dev: ## Start development environment with hot reload
	@echo "🔧 Starting development environment..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) up --build

rebuild: down build up ## Complete rebuild and restart
# Service Management
traefik-build: ## Build Traefik service only
	@echo "🔨 Building Traefik with ext-proc middleware..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) build traefik

traefik-restart: traefik-build ## Restart Traefik service only
	@echo "🔄 Restarting Traefik..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) restart traefik
	@sleep 5
	@echo "✅ Traefik restarted"

extproc-build: ## Build ext-proc server only
	@echo "🔨 Building ext-proc server..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) build extproc-plugin

extproc-restart: extproc-build ## Restart ext-proc server only
	@echo "🔄 Restarting ext-proc server..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) restart extproc-plugin
	@sleep 5
	@echo "✅ ext-proc server restarted"

# Debugging and Development
shell-traefik: ## Open shell in Traefik container
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec traefik sh

shell-extproc: ## Open shell in ext-proc container
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec extproc-plugin sh

urls: ## Display useful URLs for the POC
	@echo "🌐 POC URLs and endpoints:"
	@echo "  • Traefik Dashboard: http://localhost:$(TRAEFIK_API_PORT)"
	@echo "  • API Overview: http://localhost:$(TRAEFIK_API_PORT)/api/overview"
	@echo "  • Test App: http://$(TEST_HOST):$(TRAEFIK_HTTP_PORT)"
	@echo ""
	@echo "🧪 Test commands:"
	@echo "  • With header: curl -H 'X-Request-Header: test' -H 'Host: $(TEST_HOST)' http://localhost:$(TRAEFIK_HTTP_PORT)/"
	@echo "  • Check headers: curl -H 'X-Request-Header: test' -H 'Host: $(TEST_HOST)' -I http://localhost:$(TRAEFIK_HTTP_PORT)/"

# Validation and Quality
lint: ## Run linting for Go code
	@echo "🔍 Running Go linting..."
	@cd extproc-server && go vet ./...
	@cd extproc-server && go fmt ./...

fmt: ## Format Go code
	@echo "🎨 Formatting Go code..."
	@cd extproc-server && go fmt ./...

validate: ## Validate configuration and setup
	@echo "🔧 Validating POC setup..."
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) config >/dev/null && \
		echo "✅ Docker Compose configuration valid" || echo "❌ Docker Compose configuration invalid"
	@[ -f .env ] && echo "✅ Environment file present" || echo "❌ Environment file missing"
	@[ -f config/traefik.yml ] && echo "✅ Traefik config present" || echo "❌ Traefik config missing"
	@[ -f config/dynamic.yml ] && echo "✅ Dynamic config present" || echo "❌ Dynamic config missing"

# Cleanup
clean: down ## Complete cleanup (containers, volumes, images)
	@echo "🧹 Performing complete cleanup..."
	docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) down -v --remove-orphans
	docker system prune -f --volumes
	@echo "🗑️  Removing POC images..."
	@docker rmi $(PROJECT_NAME)_traefik $(PROJECT_NAME)_extproc-plugin 2>/dev/null || true
	@docker volume rm $(PROJECT_NAME)_logs 2>/dev/null || true
	@echo "✅ Cleanup completed"

clean-logs: ## Clean log files only
	@echo "🧹 Cleaning log files..."
	@rm -rf logs/*.log
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec traefik sh -c "rm -f /var/log/traefik/*" 2>/dev/null || true
	@echo "✅ Logs cleaned"

# Advanced Operations
debug: ## Start with debug configuration
	@echo "🐛 Starting debug environment..."
	@export TRAEFIK_LOG_LEVEL=DEBUG EXTPROC_LOG_LEVEL=DEBUG && \
		docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) up --build

monitor: ## Monitor resource usage
	@echo "📊 Monitoring resource usage (Ctrl+C to stop)..."
	@docker stats $(PROJECT_NAME)_traefik_1 $(PROJECT_NAME)_extproc-plugin_1 $(PROJECT_NAME)_whoami_1 2>/dev/null || \
		docker stats --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}"

health: ## Detailed health check
	@echo "🏥 Performing detailed health checks..."
	@echo ""
	@echo "1. Docker Compose Services:"
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) ps
	@echo ""
	@echo "2. Network Connectivity:"
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec -T traefik ping -c 1 extproc-plugin >/dev/null 2>&1 && \
		echo "✅ Traefik → ext-proc: OK" || echo "❌ Traefik → ext-proc: FAILED"
	@echo ""
	@echo "3. gRPC Health:"
	@docker-compose -f $(COMPOSE_FILE) -f $(OVERRIDE_FILE) exec -T extproc-plugin grpc_health_probe -addr=:$(EXTPROC_GRPC_PORT) >/dev/null 2>&1 && \
		echo "✅ gRPC Health Check: OK" || echo "❌ gRPC Health Check: FAILED"
	@echo ""
	@echo "4. HTTP Endpoints:"
	@curl -s http://localhost:$(TRAEFIK_API_PORT)/ping >/dev/null 2>&1 && \
		echo "✅ Traefik Ping: OK" || echo "❌ Traefik Ping: FAILED"
	@curl -s -H "Host: $(TEST_HOST)" http://localhost:$(TRAEFIK_HTTP_PORT)/ >/dev/null 2>&1 && \
		echo "✅ Test Application: OK" || echo "❌ Test Application: FAILED"

# Documentation
docs: ## Open documentation in browser (if available)
	@echo "📚 Opening documentation..."
	@open README.md 2>/dev/null || echo "📖 See README.md for documentation"

# Performance Testing
perf-test: ## Run comprehensive performance tests
	@echo "⚡ Running performance tests..."
	@echo "This may take several minutes..."
	@$(MAKE) test-stress
	@echo ""
	@echo "Performance test completed. Check logs for detailed metrics."

# Quick validation workflow
quick-test: status test ## Quick validation of POC functionality
	@echo ""
	@echo "🎯 Quick test completed!"

# Complete validation workflow  
full-test: build up health test test-integration ## Complete testing workflow
	@echo ""
	@echo "🏆 Full test suite completed!"

# Development workflow helpers
dev-traefik: ## Development workflow for Traefik changes
	@echo "🔧 Traefik development workflow..."
	@$(MAKE) traefik-restart
	@sleep 3
	@$(MAKE) test
	@echo "✅ Traefik development cycle completed"

dev-extproc: ## Development workflow for ext-proc server changes
	@echo "🔧 ext-proc development workflow..." 
	@$(MAKE) extproc-restart
	@sleep 3
	@$(MAKE) test
	@echo "✅ ext-proc development cycle completed"

