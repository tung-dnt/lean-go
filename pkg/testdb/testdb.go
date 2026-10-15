// Package testdb gives the tests a PostgreSQL: one throwaway postgres:17-alpine container per test process,
// started the first time a test binary asks for it and terminated when that binary ends (testcontainers' Ryuk
// reaper removes it if the binary crashes before it can). Nothing has to be started by hand, so `go test ./...`
// needs Docker and nothing else.
//
// Only tests import this package, so testcontainers is never linked into the application binary.
package testdb

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	// Image is the PostgreSQL the tests run against: the one docker-compose.yml and CI run.
	Image = "postgres:17-alpine"

	// EnvConnection is the environment variable that sets the test connection string (config key
	// database.testConnection, as GO_DATABASE_TESTCONNECTION). CI sets it to a postgres service; when it is
	// set, no container is started.
	EnvConnection = "GO_DATABASE_TESTCONNECTION"

	// schemaPlaceholder is replaced by the app with a random value so that every App of a test process isolates
	// itself within a schema of its own.
	schemaPlaceholder = "test_$RAND"

	startTimeout = 2 * time.Minute
)

// the one container of this process.
var (
	once      sync.Once
	container *postgres.PostgresContainer
	url       string
	startErr  error

	stopOnce sync.Once
)

// URL returns the connection string of this process's PostgreSQL container, starting it on the first call, and a
// function that terminates it. Every call returns the same container. The stop function is safe to call more than
// once, from any call's result, and the container must not be used after it.
func URL(ctx context.Context) (string, func(), error) {
	once.Do(func() {
		ctx, cancel := context.WithTimeout(ctx, startTimeout)
		defer cancel()

		container, startErr = postgres.Run(ctx, Image,
			postgres.WithDatabase("leango_test"),
			postgres.WithUsername("leango"),
			postgres.WithPassword("leango"),
			postgres.BasicWaitStrategies(),
			// testcontainers narrates every start, stop and terminate on the default logger, which would
			// print into every package's `go test` output.
			testcontainers.WithLogger(log.New(io.Discard, "", 0)),
		)
		if startErr != nil {
			startErr = fmt.Errorf("failed to start the test postgres container (is Docker running?): %w", startErr)
			return
		}
		if url, startErr = container.ConnectionString(ctx, "sslmode=disable"); startErr != nil {
			startErr = fmt.Errorf("failed to read the test postgres connection string: %w", startErr)
		}
	})
	if startErr != nil {
		return "", stop, startErr
	}

	return url, stop, nil
}

// stop terminates the container, if one was started. Once only. A failure is reported on stderr and not
// returned: the tests are over, and Ryuk removes what is left.
func stop() {
	stopOnce.Do(func() {
		if container == nil {
			return
		}
		// A fresh context: the one that started it may be long cancelled by the time the tests end.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := testcontainers.TerminateContainer(container, testcontainers.StopContext(ctx)); err != nil {
			fmt.Fprintf(os.Stderr, "testdb: failed to terminate the test postgres container: %v\n", err)
		}
	})
}

// Setup makes the test connection string available to the application's configuration: if
// GO_DATABASE_TESTCONNECTION is already set (CI) it changes nothing, otherwise it starts the container and sets
// the variable to its connection string, with a per-App schema left to the app (see schemaPlaceholder). Call it
// before the config is loaded, from a TestMain, and call the returned function when the tests are done.
func Setup(ctx context.Context) (func(), error) {
	if os.Getenv(EnvConnection) != "" {
		return func() {}, nil
	}

	base, stop, err := URL(ctx)
	if err != nil {
		return stop, err
	}
	if err := os.Setenv(EnvConnection, base+"&search_path="+schemaPlaceholder); err != nil {
		return stop, fmt.Errorf("failed to set %s: %w", EnvConnection, err)
	}

	return stop, nil
}
