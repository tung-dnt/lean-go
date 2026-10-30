# Local secrets live in an untracked .env, see .env.example. Every variable in it is exported to the
# commands just runs (run, watch, test...), so the app picks them up as GO_* overrides. An absent file
# is fine: config/config.yaml holds a working local configuration.
set dotenv-load

# Comment lines inside a recipe are notes for the reader, not commands to echo.
set ignore-comments

# The Tailwind release for this machine: macos-arm64, macos-x64, linux-x64, linux-arm64, and on Alpine
# the musl build. This is what lets the Docker (alpine) image reuse the very same install recipe
# instead of re-implementing the download. If it picks the wrong one, hard-code the package from
# https://github.com/tailwindlabs/tailwindcss/releases
tailwind_os := if os() == "macos" { "macos" } else { "linux" }
tailwind_arch := if arch() == "aarch64" { "arm64" } else { "x64" }
tailwind_libc := if path_exists("/etc/alpine-release") == "true" { "-musl" } else { "" }
tailwind_package := "tailwindcss-" + tailwind_os + "-" + tailwind_arch + tailwind_libc

# Where the golang-migrate migration files live. sqlc reads the same directory as its schema source.
migrations_dir := "internal/external/db/migrations"

# Where sqlc writes the generated Go code (sqlc.yaml `out`). Generated, committed, never hand-edited.
sqlc_out := "internal/external/db"

# Connection string used by the golang-migrate CLI recipes, and the admin. Override to point at
# another environment, ie: DATABASE_URL="postgres://..." just migrate-up
database_url := env("DATABASE_URL", "postgres://leango:leango@localhost:5432/leango?sslmode=disable")

# The golang-migrate CLI needs its database driver selected at compile time via a build tag, which
# `go tool` cannot pass. It is therefore run with `go run -tags pgx5` instead — still pinned to the
# github.com/golang-migrate/migrate/v4 version in go.mod, and using the same pgx/v5 driver the app
# itself registers in pkg/postgres/migrate.go. That driver is registered under the `pgx5://` scheme.
migrate := "go run -tags pgx5 github.com/golang-migrate/migrate/v4/cmd/migrate"
migrate_url := replace(database_url, "postgres://", "pgx5://")

# Scanners are pinned and run with go run, so nothing has to be installed first.
gitleaks := "go run github.com/zricethezav/gitleaks/v8@v8.30.1"
osv_scanner := "go run github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.7.0"

# Print the recipes
help:
    @just --list --unsorted

# Install all dependencies
install: deps-install tailwind-install bun-install

# Download the Go modules and pre-build the tools pinned in go.mod
deps-install:
    go mod download
    go build -o ./tmp/tools/ tool

# Install JS packages with Bun: htmx/Alpine (bundled into app.js) + editor types
bun-install:
    bun install

# Install the Tailwind CSS CLI and the daisyUI plugins, at the versions pinned in versions.env
tailwind-install:
    #!/bin/sh
    set -eu
    # versions.env is the single source of truth for the pinned versions, shared with the Dockerfile.
    . ./versions.env
    # Download to ./tmp and move into place rather than overwriting the binary in place: on macOS,
    # replacing a running-or-previously-run binary's contents leaves a stale code-signature cache
    # for that inode and the next exec is SIGKILLed. `mv` swaps in a new inode, so it cannot happen.
    mkdir -p tmp
    curl -sLo tmp/tailwindcss "https://github.com/tailwindlabs/tailwindcss/releases/download/v${TAILWIND_VERSION}/{{tailwind_package}}"
    chmod +x tmp/tailwindcss
    mv -f tmp/tailwindcss tailwindcss
    curl -sLO "https://github.com/saadeghi/daisyui/releases/download/v${DAISYUI_VERSION}/daisyui.js"
    curl -sLO "https://github.com/saadeghi/daisyui/releases/download/v${DAISYUI_VERSION}/daisyui-theme.js"

# Start PostgreSQL via Docker
db-up:
    docker compose up -d postgres

# Stop PostgreSQL
db-down:
    docker compose down

# Alias of sqlc-gen
sqlc: sqlc-gen

# Generate type-safe Go code from the SQL queries
sqlc-gen:
    go tool sqlc generate

# Lint the SQL queries
sqlc-vet:
    go tool sqlc vet

# Create a new migration (ie, just migrate-new add_posts)
migrate-new name:
    {{migrate}} create -ext sql -dir {{migrations_dir}} -seq {{name}}

# Apply all pending migrations
migrate-up:
    {{migrate}} -path {{migrations_dir}} -database "{{migrate_url}}" up

# Roll back the most recent migration
migrate-down:
    {{migrate}} -path {{migrations_dir}} -database "{{migrate_url}}" down 1

# Clear a dirty migration state (ie, just migrate-force 1)
migrate-force version:
    {{migrate}} -path {{migrations_dir}} -database "{{migrate_url}}" force {{version}}

# Create a new admin user (ie, just admin me@web.com)
admin email="admin@home.local":
    GO_DATABASE_CONNECTION="{{database_url}}" go run cmd/admin/main.go --email={{email}}

# Run the application
run: css js
    clear
    go run cmd/web/main.go

# Run the application and watch for changes with air to automatically rebuild
watch:
    clear
    go tool air

# Run all tests with the race detector (PostgreSQL comes from testcontainers, so Docker must be running; builds the embedded main.css and app.js first)
test: css js
    go test -race -coverprofile=cover.out ./...

# Lint the Go code with golangci-lint (see .golangci.yml)
lint:
    golangci-lint run

# Scan the Go dependencies and the code paths that reach them for known vulnerabilities
vuln:
    go tool govulncheck ./...

# Fail if the committed sqlc output is stale (sqlc verify needs sqlc Cloud, so regenerate and diff instead)
sqlc-verify: sqlc-gen
    # internal/external/db also holds the migrations, the queries, embed.go and hand-written tests, so only the generated Go is diffed.
    git diff --exit-code -- '{{sqlc_out}}/*.go' ':(exclude){{sqlc_out}}/*_test.go' ':(exclude){{sqlc_out}}/embed.go'

# Type-check the TypeScript of web/js
types:
    bun run typecheck

# Scan the repository and its history for secrets
secrets:
    {{gitleaks}} git --redact --no-banner

# Scan the Go and npm dependencies for known vulnerabilities
deps:
    {{osv_scanner}} scan source -r .

# Fail when duplicated code (Go, TS, CSS; tests and generated code excluded) passes the threshold in .jscpd.json
dupes:
    # Pinned and run with `bun x --bun`, so jscpd and its dependency tree use Bun and stay out of bun.lock:
    # braces@3.0.3 (GHSA-vfj7-8cjw-p6xm, no fixed version), which just deps would otherwise report.
    bun x --bun jscpd@4.3.0 --config .jscpd.json .

# Everything CI and a finished task must pass
check: sqlc-verify sqlc-vet lint vuln types secrets deps dupes test build

# Check for direct dependency updates
check-updates:
    go list -u -m -f '{{{{if not .Indirect}}{{{{.}}{{{{end}}' all | grep "\["

# Build and minify Tailwind CSS
css:
    ./tailwindcss -i tailwind.css -o public/static/main.css -m

# Bundle and minify TypeScript with esbuild (ESM, code-split)
js:
    # --splitting: dynamic import() targets become content-hashed chunks under js/chunks/, fetched only
    # when a page needs them; htmx + Alpine stay in app.js because every page uses them. The output
    # dir is wiped first so stale chunks are never embedded. No --sourcemap: the bundle is embedded
    # into the scratch binary, and a ~460KB map would ride along as dead weight.
    rm -rf public/static/js
    go tool esbuild web/js/app.ts --bundle --minify --splitting --format=esm --outdir=public/static/js --chunk-names=chunks/[name]-[hash]

# Build assets and compile the application binary
build: css js
    go build -o ./tmp/main ./cmd/web
