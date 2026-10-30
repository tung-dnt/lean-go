# Data access and schema

Use PostgreSQL through sqlc and golang-migrate; there is no ORM. SQL is the source of query behavior and generated Go is committed output.

## Do / Don't

**Do:** add versioned schema changes under `internal/external/db/migrations` and SQL queries under `internal/external/db/queries`; regenerate with `just sqlc` and commit the generated `internal/external/db` code. Test queries whose behavior depends on ordering, locking, or limits.

```bash
just migrate-new add_notes_index
# Edit the up/down migration and add any required query under internal/external/db/queries.
just sqlc
just migrate-up
```

**Don't:** hand-edit generated files, change schema outside a migration, or assume SQL defaults encode application-level validation. Migrations are embedded for test schemas; production schema changes are applied by the `migrate-*` recipes or the `migrate/migrate` image, never automatically on application startup.

sqlc reads migrations as its schema source, keeping generated types aligned with the migrated schema. Keep constraints in the database as well as relevant input checks: for example, a form limit also needs a database `CHECK` so non-HTTP writes cannot bypass it. Make transformations explicit in SQL or domain code; emails are lowercased in SQL (`LOWER()`), and passwords are hashed with `auth.HashPassword` before storage.
