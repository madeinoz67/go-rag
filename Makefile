BINARY  := go-rag
BIN_DIR := bin
PKG     := ./...
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build run test test-eval vet fmt lint vuln tidy install docker clean help

build: ## Build the static go-rag binary into ./bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/go-rag

run: build ## Build and run go-rag (pass ARGS="..." for flags)
	./$(BIN_DIR)/$(BINARY) $(ARGS)

test: ## Run tests with race detector and coverage
	go test -race -cover $(PKG)

test-eval: build ## Retrieval-quality regression gate (offline, reproducible). Fails on recall@10 regression.
	./$(BIN_DIR)/$(BINARY) eval --embedder offline --baseline testdata/golden/baseline.json --tolerance 2.0

vet: ## Run go vet
	go vet $(PKG)

fmt: ## Format all Go sources
	gofmt -s -w .

lint: ## Run golangci-lint
	golangci-lint run

vuln: ## Run govulncheck
	govulncheck $(PKG)

tidy: ## Tidy module dependencies
	go mod tidy

install: build ## Install go-rag into /usr/local/bin
	install -m 0755 $(BIN_DIR)/$(BINARY) /usr/local/bin/$(BINARY)

docker: ## Build the container image
	docker build -t $(BINARY) .

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'
