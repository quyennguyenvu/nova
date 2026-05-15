.PHONY: build run clean generate test-gen verify-gen rebuild help

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

generate: build ## Generate a sample project in interactive mode
	./$(BINARY) new
	@echo ""
	@echo "✅ new project generated"


generate-all: build ## Generate a sample project to test templates (Fiber/Postgres/Redis)
	rm -rf $(TEST_DIR)
	./$(BINARY) new testproject \
		--module=nova/testproject \
		--transport=http \
		--http-framework=fiber \
		--database=postgres \
		--db-driver=pgx \
		--query=sqlc \
		--cache=redis \
		--queue=none \
		--config=yaml \
		--di=wire \
		--docker \
		--makefile \
		--ci=github
	mv testproject $(TEST_DIR)
	@echo ""
	@echo "✅ Output at $(TEST_DIR)"

generate-minimal: build ## Generate a minimal project (no DB, no cache, no Docker)
	rm -rf $(TEST_DIR)
	./$(BINARY) new testproject \
		--module=nova/testproject \
		--transport=http \
		--http-framework=fiber \
		--database=none \
		--cache=none \
		--queue=none \
		--config=yaml \
		--di=manual
	mv testproject $(TEST_DIR)

verify-gen: generate ## Generate + list all output files for review
	@echo ""
	@echo "📂 Generated files:"
	@find $(TEST_DIR) -type f | sort
	@echo ""
	@echo "📊 Total: $$(find $(TEST_DIR) -type f | wc -l | tr -d ' ') files"

diff-gen: generate ## Generate + show key template outputs for quick review
	@echo "=== go.mod ==="
	@cat $(TEST_DIR)/go.mod
	@echo "\n=== cmd/api.go ==="
	@cat $(TEST_DIR)/cmd/api.go
	@echo "\n=== internal/infrastructure/di/container.go ==="
	@cat $(TEST_DIR)/internal/infrastructure/di/container.go

## —— Quality ————————————————————————————————————

test: ## Run Go tests
	go test -v ./...

lint: ## Run linter
	golangci-lint run

fmt: ## Format all Go source files
	golangci-lint fmt

vet: ## Run go vet
	go vet ./...

## —— Help ———————————————————————————————————————

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
