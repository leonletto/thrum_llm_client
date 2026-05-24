# thrum_llm_client Makefile
#
# Standard quality gates.

.PHONY: help test test-race build vet quality clean-cache e2e

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

vet: ## go vet ./...
	go vet ./...

build: ## go build ./...
	go build ./...

test: ## go test ./endpoint/ -v
	go test ./endpoint/ -v

test-race: ## go test -race ./endpoint/
	go test -race ./endpoint/

quality: vet test test-race build ## Run all quality gates (vet + test + race + build)

clean-cache: ## go clean -testcache (forces test re-runs even when source unchanged)
	go clean -testcache

e2e: ## Run live API smoke suite (requires .env with ZAI_API_KEY + OPENROUTER_API_KEY)
	@test -f .env || (echo "Missing .env (need ZAI_API_KEY, OPENROUTER_API_KEY)"; exit 1)
	go test -tags=e2e -v -timeout 15m ./tests/e2e/...
