# DYD API — common developer + ops tasks. Run `make help` for the list.

.DEFAULT_GOAL := help
BINARY := api
PKG := ./cmd/api

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod/go.sum
	go mod tidy

.PHONY: build
build: ## Build a static binary into ./out
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o out/$(BINARY) $(PKG)

.PHONY: run
run: ## Run locally (reads .env via your shell / direnv)
	go run $(PKG)

.PHONY: test
test: ## Run tests with race detector
	go test -race ./...

.PHONY: vet
vet: ## Static checks
	go vet ./...

.PHONY: fmt
fmt: ## Format all Go source
	gofmt -w .

.PHONY: check
check: fmt vet test ## Format, vet, and test

.PHONY: keys
keys: ## Generate the three base64 32-byte secrets for .env
	@echo "PII_ENCRYPTION_KEY=$$(openssl rand -base64 32)"
	@echo "BLIND_INDEX_KEY=$$(openssl rand -base64 32)"
	@echo "VERIFICATION_HMAC_KEY=$$(openssl rand -base64 32)"

.PHONY: up
up: ## Bring up the full stack (db + redis + api + caddy)
	docker compose up --build

.PHONY: down
down: ## Tear down the stack (keeps volumes)
	docker compose down

.PHONY: docker
docker: ## Build the production image
	docker build -t dyd-api:latest .
