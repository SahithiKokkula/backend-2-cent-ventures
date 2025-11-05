.PHONY: help build run test clean docker-build docker-up docker-down deps lint

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Download dependencies
	go mod download
	go mod tidy

build: ## Build the application
	go build -o bin/trading-engine ./cmd/server

run: ## Run the application locally
	go run ./cmd/server

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Run tests with coverage report
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	golangci-lint run

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html

docker-build: ## Build docker image
	docker-compose build

docker-up: ## Start all services
	docker-compose up -d

docker-down: ## Stop all services
	docker-compose down

docker-logs: ## Show logs
	docker-compose logs -f

docker-clean: ## Clean docker volumes
	docker-compose down -v

db-migrate: ## Run database migrations
	docker-compose exec postgres psql -U trader -d trading -f /docker-entrypoint-initdb.d/init.sql

load-test: ## Run load tests
	cd load-test && k6 run load-test.js

fixtures: ## Generate test fixtures
	cd fixtures && go run gen_orders.go

.DEFAULT_GOAL := help
