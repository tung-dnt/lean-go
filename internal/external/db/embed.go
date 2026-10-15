// Package db is the database layer: the golang-migrate migrations (migrations/), the SQL that sqlc
// reads (queries/), and the Go code sqlc generates from them (every *.sql.go, models.go, db.go: never
// edit those by hand, run `make sqlc`). This file is the only hand-written source: it embeds the
// migrations so that they ship inside the binary. The queries are build-time input only.
package db

import "embed"

// Migrations holds migrations/*.sql, the same files the migrate-* make targets apply.
//
//go:embed migrations/*.sql
var Migrations embed.FS
