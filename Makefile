.PHONY: build run clean gen gen-api gen-grpc gen-worker verify-gen diff-gen rebuild test test-all lint fmt vet help

# Binary
BINARY    := bin/nova
TEST_DIR  := /tmp/nova-test-output

## —— Build ——————————————————————————————————————

build: ## Build the nova CLI binary
	go build -o $(BINARY) .

run: build ## Build and show help
	./$(BINARY) --help

clean: ## Remove build artifacts and test output
	rm -rf bin/ $(TEST_DIR)

rebuild: clean build ## Clean + rebuild from scratch

## —— Template Development ————————————————————————

gen: build ## Generate a sample project in interactive mode
	./$(BINARY) new
	@echo ""
	@echo "✅ new project generated"

gen-api: build ## Generate a sample project to test templates (Fiber/Postgres/Redis)
	rm -rf $(TEST_DIR)
	./$(BINARY) new testapi \
		--module=nova/testapi \
		--transport=http \
		--http-framework=fiber \
		--database=postgres \
		--db-driver=pgx \
		--query=sqlc \
		--cache=redis \
		--queue=kafka \
		--config=yaml \
		--di=wire \
		--docker \
		--ci=github
	mv testapi $(TEST_DIR)
	@echo ""
	@echo "✅ Output at $(TEST_DIR)"

gen-worker: build ## Generate a worker (consumer) service: Kafka + Postgres + sqlc + Wire
	rm -rf $(TEST_DIR)
	./$(BINARY) new testworker \
		--module=nova/testworker \
		--transport=worker \
		--database=postgres \
		--db-driver=pgx \
		--query=sqlc \
		--cache=redis \
		--queue=kafka \
		--config=yaml \
		--di=wire \
		--docker \
		--ci=github
	mv testworker $(TEST_DIR)
	@echo ""
	@echo "✅ Output at $(TEST_DIR)"

gen-grpc: build ## Generate a gRPC service: Postgres + sqlc + Wire
	rm -rf $(TEST_DIR)
	./$(BINARY) new testgrpc \
		--module=nova/testgrpc \
		--transport=grpc \
		--database=postgres \
		--db-driver=pgx \
		--query=sqlc \
		--cache=redis \
		--queue=kafka \
		--config=yaml \
		--di=wire \
		--docker \
		--ci=github
	mv testgrpc $(TEST_DIR)
	@echo ""
	@echo "✅ Output at $(TEST_DIR)"

verify-gen: gen-api ## Generate + list all output files for review
	@echo ""
	@echo "📂 Generated files:"
	@find $(TEST_DIR) -type f | sort
	@echo ""
	@echo "📊 Total: $$(find $(TEST_DIR) -type f | wc -l | tr -d ' ') files"

diff-gen: gen-api ## Generate + show key template outputs for quick review
	@echo "=== go.mod ==="
	@cat $(TEST_DIR)/go.mod
	@echo "\n=== cmd/api.go ==="
	@cat $(TEST_DIR)/cmd/api.go
	@echo "\n=== internal/infrastructure/di/wire.go ==="
	@cat $(TEST_DIR)/internal/infrastructure/di/wire.go

## —— Quality ————————————————————————————————————

test: ## Fast suite (-short skips the render-then-vet matrix)
	go test -short ./...

test-all: ## Full suite: 32-case vet matrix (2 at a time) + 4 real-toolchain bootstraps (~2 min warm cache; <2.5 GB)
	go test -parallel 2 -timeout 30m ./...

lint: ## Run linter (GOGC=50 trades a few seconds for a lower memory peak)
	GOGC=50 golangci-lint run

fmt: ## Format all Go source files
	golangci-lint fmt

vet: ## Run go vet
	go vet ./...

## —— Help ———————————————————————————————————————

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
