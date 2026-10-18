// Command admin creates the admin account: the one user of the site who can log in. It prints the generated
// password once, and it is not kept anywhere else.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tung-dnt/lean-go/internal/auth"
	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/config"
)

func main() {
	if err := run(context.Background(), os.Args, os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %s\n", err)
		os.Exit(1)
	}
}

// run creates a new admin user with the email passed in via the flag, in the database that the configuration
// names (config database.connection, which GO_DATABASE_CONNECTION overrides). The schema has to be there already:
// it is made by the migrate-* just recipes.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "email address for the admin user")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("email is required")
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.Database.Connection)
	if err != nil {
		return fmt.Errorf("failed to connect to the database: %w", err)
	}
	defer pool.Close()

	// Generate a password, and hash it before storing it.
	password, err := auth.RandomToken(10)
	if err != nil {
		return fmt.Errorf("failed to generate a random password: %w", err)
	}
	hashed, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash the password: %w", err)
	}

	// Create the admin user.
	if _, err := db.New(pool).CreateUser(ctx, db.CreateUserParams{
		Name:     "Admin",
		Email:    *email,
		Password: hashed,
		Verified: true,
		Admin:    true,
	}); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "\n-- ADMIN USER CREATED --\nEmail: %s\nPassword: %s\n----\n\n", *email, password); err != nil {
		return err
	}

	return nil
}
