// Package postgres runs the schema work that the application does itself: applying the embedded migrations, and
// creating and dropping the disposable schema of a test run. The queries are sqlc's (db/sqlc).
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5" database driver with golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"

	"github.com/tung-dnt/lean-go/internal/external/db"
)

// ReplaceRand substitutes $RAND in a connection string with a random value, which is how a test process gives
// itself a database or schema of its own: the connection says search_path=test_$RAND, and every process that
// replaces it works in a schema that no other has. The value is lower case letters and digits, so that it is
// the same name whether the schema is quoted or not.
func ReplaceRand(connection string) string {
	return strings.Replace(connection, "$RAND", strings.ToLower(rand.Text()), 1)
}

// Migrate applies all pending up migrations to the database at the given connection string.
// The connection string is expected to use the standard postgres:// scheme; it is rewritten to the
// pgx5:// scheme that golang-migrate registers its pgx/v5 driver under.
func Migrate(connection string) (err error) {
	src, err := iofs.New(db.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to load embedded migrations: %w", err)
	}

	dsn, err := migrateDSN(connection)
	if err != nil {
		return err
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("failed to initialize migrations: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		err = errors.Join(err, sourceErr, databaseErr)
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

// Schema returns the schema named in the connection's search_path, or an empty string if the
// connection does not specify one and therefore uses the default.
func Schema(connection string) (string, error) {
	u, err := url.Parse(connection)
	if err != nil {
		return "", fmt.Errorf("failed to parse database connection string: %w", err)
	}

	return u.Query().Get("search_path"), nil
}

// CreateSchema creates the schema named in the connection's search_path, if one is named and it
// does not already exist. Migrations are applied into the current schema, so this must run before
// Migrate whenever the connection targets a schema other than the default.
func CreateSchema(connection string) error {
	return execOnSchema(connection, func(schema string) string {
		return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{schema}.Sanitize())
	})
}

// DropSchema removes the schema named in the connection's search_path, along with everything in it.
// This is used to clean up the disposable schema each test run isolates itself within.
func DropSchema(connection string) error {
	return execOnSchema(connection, func(schema string) string {
		return fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgx.Identifier{schema}.Sanitize())
	})
}

// execOnSchema runs a statement against the schema named in the connection's search_path.
// It is a no-op if the connection does not name a schema.
func execOnSchema(connection string, statement func(schema string) string) (err error) {
	schema, err := Schema(connection)
	if err != nil {
		return err
	}
	if schema == "" {
		return nil
	}

	ctx := context.Background()

	conn, err := pgx.Connect(ctx, connection)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		err = errors.Join(err, conn.Close(ctx))
	}()

	if _, err := conn.Exec(ctx, statement(schema)); err != nil {
		return fmt.Errorf("failed to execute schema statement: %w", err)
	}

	return nil
}

// migrateDSN rewrites a postgres connection string to the scheme golang-migrate's pgx/v5 driver is
// registered under.
func migrateDSN(connection string) (string, error) {
	u, err := url.Parse(connection)
	if err != nil {
		return "", fmt.Errorf("failed to parse database connection string: %w", err)
	}

	u.Scheme = "pgx5"

	return u.String(), nil
}
