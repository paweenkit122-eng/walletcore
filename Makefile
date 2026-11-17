.PHONY: help up down down-v logs psql migrate-up migrate-down run tidy test

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# ---- Docker (no local Go required) ----------------------------------------
up: ## Start the full stack (db + migrations + api) and rebuild
	docker compose up --build

down: ## Stop and remove containers
	docker compose down

down-v: ## Stop and remove containers AND volumes (wipes all data)
	docker compose down -v

logs: ## Tail the api logs
	docker compose logs -f api

psql: ## Open a psql shell inside the db container
	docker compose exec db psql -U wallet -d walletcore

migrate-up: ## Apply migrations against the running db
	docker compose run --rm migrate up

migrate-down: ## Roll back the last migration
	docker compose run --rm migrate down 1

# ---- Local Go (require Go installed: brew install go) ----------------------
run: ## Run the api locally against the compose db
	go run ./cmd/api

tidy: ## Sync go.mod / go.sum
	go mod tidy

test: ## Run all tests
	go test ./...
