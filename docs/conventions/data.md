# Data access and the schema

PostgreSQL through sqlc and golang-migrate; no ORM.

- Data access is sqlc over raw SQL in `internal/external/db/queries`, against the schema in `internal/external/db/migrations` (golang-migrate, versioned; production migrates with the `migrate-*` just recipes or the `migrate/migrate` image). The generated code is in `internal/external/db`: never hand-edit it.
- Changing data access: `just migrate-new <name>`, edit the SQL in `internal/external/db/migrations` / `internal/external/db/queries`, then `just sqlc`. Commit the regenerated `internal/external/db`. A query whose SQL has a rule of its own (an order, a lock, a limit) gets a test in `internal/external/db`.

```bash
just migrate-new add_posts         # create the migration files
# ...write the up/down SQL, then add queries under internal/external/db/queries/
just sqlc                         # regenerate the type-safe Go code
just migrate-up                   # apply it to the local database
```

sqlc reads the migrations directory as its schema source, so the generated code always matches the migrated schema.
The migrations are also embedded in the binary (`internal/external/db/embed.go`): the test environment migrates its
throwaway schema with them, while a real environment is migrated with the `migrate-*` targets, never on startup.

Things an ORM would do for you are explicit: emails are lowercased in SQL (`LOWER()`), and a password is hashed with
`auth.HashPassword` before it is stored.
