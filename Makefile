# Get the OS name in lowercase (linux, darwin)
OS_SYSNAME := $(shell uname -s | tr A-Z a-z)
# Get the machine architecture (x86_64, arm64)
OS_MACHINE := $(shell uname -m)

# If mac OS, use `macos-arm64` or `macos-x64`
ifeq ($(OS_SYSNAME),darwin)
	OS_SYSNAME = macos
	ifneq ($(OS_MACHINE),arm64)
		OS_MACHINE = x64
	endif
endif

# If Linux, normalise the arch (x86_64->x64, aarch64->arm64) and, on Alpine,
# select the musl build — this is what lets the Docker (alpine) image reuse the
# very same install target instead of re-implementing the download.
ifeq ($(OS_SYSNAME),linux)
	ifeq ($(OS_MACHINE),x86_64)
		OS_MACHINE = x64
	endif
	ifeq ($(OS_MACHINE),aarch64)
		OS_MACHINE = arm64
	endif
	TAILWIND_LIBC := $(shell [ -f /etc/alpine-release ] && echo -musl)
endif

# Pinned toolchain versions, shared with the Dockerfile — single source of truth.
include versions.env

# Local secrets live in an untracked .env, see .env.example. Every variable in it is exported to the
# commands make runs (run, watch, test...), so the app picks them up as GO_* overrides. An absent file
# is fine: config/config.yaml holds a working local configuration.
-include .env
export $(shell [ -f .env ] && sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' .env)

# The appropriate Tailwind package for your OS will attempt to be automatically determined.
# If this is not working, hard-code the package you want using these options:
# https://github.com/tailwindlabs/tailwindcss/releases/tag/v$(TAILWIND_VERSION)
TAILWIND_PACKAGE = tailwindcss-$(OS_SYSNAME)-$(OS_MACHINE)$(TAILWIND_LIBC)

# Where the golang-migrate migration files live. sqlc reads the same directory as its schema source.
MIGRATIONS_DIR = internal/external/db/migrations

# Where sqlc writes the generated Go code (sqlc.yaml `out`). Generated, committed, never hand-edited.
SQLC_OUT = internal/external/db

# Connection string used by the golang-migrate CLI targets. Override to point at another environment,
# ie: make migrate-up DATABASE_URL="postgres://..."
DATABASE_URL ?= postgres://leango:leango@localhost:5432/leango?sslmode=disable

# The golang-migrate CLI needs its database driver selected at compile time via a build tag, which
# `go tool` cannot pass. It is therefore run with `go run -tags pgx5` instead — still pinned to the
# github.com/golang-migrate/migrate/v4 version in go.mod, and using the same pgx/v5 driver the app
# itself registers in pkg/postgres/migrate.go. That driver is registered under the `pgx5://` scheme.
MIGRATE = go run -tags pgx5 github.com/golang-migrate/migrate/v4/cmd/migrate
MIGRATE_URL = $(subst postgres://,pgx5://,$(DATABASE_URL))
ADMIN_EMAIL = admin@home.local

.PHONY: help
help: ## Print make targets
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: install
install: deps-install tailwind-install bun-install ## Install all dependencies

.PHONY: deps-install
deps-install: ## Download the Go modules and pre-build the tools pinned in go.mod
	go mod download
	go build -o ./tmp/tools/ tool

.PHONY: bun-install
bun-install: ## Install JS packages with Bun: htmx/Alpine (bundled into app.js) + editor types
	bun install

.PHONY: tailwind-install
tailwind-install: ## Install the Tailwind CSS CLI
	# Download to ./tmp and move into place rather than overwriting the binary in place: on macOS,
	# replacing a running-or-previously-run binary's contents leaves a stale code-signature cache
	# for that inode and the next exec is SIGKILLed. `mv` swaps in a new inode, so it cannot happen.
	mkdir -p tmp
	curl -sLo tmp/tailwindcss https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)/$(TAILWIND_PACKAGE)
	chmod +x tmp/tailwindcss
	mv -f tmp/tailwindcss tailwindcss
	curl -sLO https://github.com/saadeghi/daisyui/releases/download/v$(DAISYUI_VERSION)/daisyui.js
	curl -sLO https://github.com/saadeghi/daisyui/releases/download/v$(DAISYUI_VERSION)/daisyui-theme.js

.PHONY: db-up
db-up: ## Start PostgreSQL via Docker
	docker compose up -d postgres

.PHONY: db-down
db-down: ## Stop PostgreSQL
	docker compose down

.PHONY: sqlc
sqlc: sqlc-gen ## Alias of sqlc-gen

.PHONY: sqlc-gen
sqlc-gen: ## Generate type-safe Go code from the SQL queries
	go tool sqlc generate

.PHONY: sqlc-vet
sqlc-vet: ## Lint the SQL queries
	go tool sqlc vet

.PHONY: migrate-new
migrate-new: ## Create a new migration (ie, make migrate-new name=add_posts)
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS_DIR) -seq $(name)

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(MIGRATE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(MIGRATE_URL)" down 1

.PHONY: migrate-force
migrate-force: ## Clear a dirty migration state (ie, make migrate-force version=1)
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(MIGRATE_URL)" force $(version)

.PHONY: admin
admin: ## Create a new admin user (ie, make admin ADMIN_EMAIL=me@web.com [DATABASE_URL=postgres://...])
	GO_DATABASE_CONNECTION="$(DATABASE_URL)" go run cmd/admin/main.go --email=$(ADMIN_EMAIL)

.PHONY: run
run: css js ## Run the application
	clear
	go run cmd/web/main.go

.PHONY: watch
watch: ## Run the application and watch for changes with air to automatically rebuild
	clear
	go tool air

.PHONY: test
test: css js ## Run all tests with the race detector (PostgreSQL comes from testcontainers, so Docker must be running; builds the embedded main.css and app.js first)
	go test -race -cover ./...

.PHONY: lint
lint: ## Lint the Go code with golangci-lint (see .golangci.yml)
	golangci-lint run

.PHONY: vuln
vuln: ## Scan the Go dependencies and the code paths that reach them for known vulnerabilities
	go tool govulncheck ./...

.PHONY: sqlc-verify
# internal/external/db also holds the migrations, the queries, embed.go and hand-written tests, so only the generated Go is diffed.
sqlc-verify: sqlc-gen ## Fail if the committed sqlc output is stale (sqlc verify needs sqlc Cloud, so regenerate and diff instead)
	git diff --exit-code -- '$(SQLC_OUT)/*.go' ':(exclude)$(SQLC_OUT)/*_test.go' ':(exclude)$(SQLC_OUT)/embed.go'

.PHONY: check
check: sqlc-verify lint vuln typecheck test build ## Everything CI and a finished task must pass

.PHONY: typecheck
typecheck: ## Type-check the TypeScript of web/js
	bun run typecheck

.PHONY: check-updates
check-updates: ## Check for direct dependency updates
	go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all | grep "\["

.PHONY: css
css: ## Build and minify Tailwind CSS
	./tailwindcss -i tailwind.css -o public/static/main.css -m

.PHONY: js
js: ## Bundle and minify TypeScript with esbuild (ESM, code-split)
	# --splitting: dynamic import() targets become content-hashed chunks under js/chunks/, fetched only
	# when a page needs them; htmx + Alpine stay in app.js because every page uses them. The output
	# dir is wiped first so stale chunks are never embedded. No --sourcemap: the bundle is embedded
	# into the scratch binary, and a ~460KB map would ride along as dead weight.
	rm -rf public/static/js
	go tool esbuild web/js/app.ts --bundle --minify --splitting --format=esm --outdir=public/static/js --chunk-names=chunks/[name]-[hash]

.PHONY: build
build: css js ## Build assets and compile the application binary
	go build -o ./tmp/main ./cmd/web
