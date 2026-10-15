package server

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/postgres"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// Run is the program: it builds what the server is made of, serves until ctx is done or the process is told to
// stop (an interrupt, or the SIGTERM that a container is stopped with), and shuts down in order.
//
// Everything it takes from outside is an argument, so that a test can run the real server and nothing else
// can: the command line (args, the first of which is the name of the program), the environment (getenv, which
// answers what config.Load asks of it, GO_* overrides included), and where it writes (stdout says where the
// server listens, stderr is the log). The only flag is -config, the directory that holds config.yaml.
//
// What it builds: the configuration, the logger, the database (in the test environment a schema of its own,
// created, migrated, and dropped at the end; anywhere else the schema is the migrate-* make targets' business,
// and is not touched), the session store, and the handler (NewServer).
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	flags := flag.NewFlagSet("web", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configDir := flags.String("config", "", "the directory that holds config.yaml (the default places are searched when it is not given)")
	if len(args) > 1 {
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
	}

	var dirs []string
	if *configDir != "" {
		dirs = append(dirs, *configDir)
	}
	cfg, err := config.Load(getenv, dirs...)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := newLogger(cfg, stderr)

	pool, closeDB, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB(logger)

	sessions, err := session.NewStore(session.Config{
		Key:         cfg.App.EncryptionKey,
		PreviousKey: cfg.App.PreviousEncryptionKey,
		MaxAge:      cfg.App.SessionMaxAge,
		Secure:      cfg.HTTP.TLS.Enabled,
	})
	if err != nil {
		return fmt.Errorf("failed to create the session store: %w", err)
	}

	handler, err := NewServer(cfg, logger, pool, db.New(pool), sessions)
	if err != nil {
		return fmt.Errorf("failed to build the server: %w", err)
	}

	return serve(ctx, cfg, logger, handler, stdout)
}

// newLogger is the logger of the server: text on w, at info level in production and at debug level elsewhere.
func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	level := slog.LevelDebug
	if cfg.App.Environment == config.EnvProduction {
		level = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// serve listens, and serves handler until ctx is done, and then shuts the server down, giving the requests that
// are being served the configured time to finish.
func serve(ctx context.Context, cfg *config.Config, logger *slog.Logger, handler http.Handler, stdout io.Writer) error {
	addr := net.JoinHostPort(cfg.HTTP.Hostname, strconv.Itoa(int(cfg.HTTP.Port)))
	srv := &http.Server{
		Handler:           handler,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		ReadHeaderTimeout: cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	var listener net.Listener
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if cfg.HTTP.TLS.Enabled {
		certs, err := tls.LoadX509KeyPair(cfg.HTTP.TLS.Certificate, cfg.HTTP.TLS.Key)
		if err != nil {
			return errors.Join(fmt.Errorf("cannot load TLS certificate: %w", err), listener.Close())
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certs}, MinVersion: tls.VersionTLS12}
		listener = tls.NewListener(listener, srv.TLSConfig)
	}

	if _, err := fmt.Fprintf(stdout, "listening on %s\n", listener.Addr()); err != nil {
		logger.Warn("cannot write to stdout", "error", err)
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()

	select {
	case err := <-served:
		// Serve only returns on its own when it has failed: a clean shutdown starts below.
		return fmt.Errorf("the server stopped: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shut the server down: %w", err)
	}
	if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("the server stopped: %w", err)
	}

	return nil
}

// openDatabase opens the PostgreSQL database of the application data, and returns the function that closes it.
//
// Migrations are deliberately NOT applied here for real environments: schema changes are rolled out separately
// via the migrate-* make targets. The test environment is the exception: its schema is disposable and created
// from scratch on each run, so it has to be migrated before any queries can run against it. Each run isolates
// itself within a schema of its own (the $RAND of the test connection becomes a random number) rather than
// sharing one and clobbering the others, and drops it when it ends.
func openDatabase(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, func(*slog.Logger), error) {
	connection := cfg.Database.Connection
	testing := cfg.App.Environment == config.EnvTest

	if testing {
		if cfg.Database.TestConnection == "" {
			return nil, nil, errors.New("no test database: a TestMain must call servertest or testdb.Setup (pkg/testdb), or set GO_DATABASE_TESTCONNECTION")
		}
		connection = postgres.ReplaceRand(cfg.Database.TestConnection)

		if err := postgres.CreateSchema(connection); err != nil {
			return nil, nil, fmt.Errorf("failed to create test schema: %w", err)
		}

		// The schema was just created and is therefore empty. Build it out with the same embedded migrations the
		// migrate-* make targets apply, so tests run against the real schema.
		if err := postgres.Migrate(connection); err != nil {
			return nil, nil, fmt.Errorf("failed to migrate test schema: %w", err)
		}
	}

	pool, err := pgxpool.New(ctx, connection)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()

		return nil, nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, func(logger *slog.Logger) {
		pool.Close()

		// Discard the disposable schema this test run created.
		if testing {
			if err := postgres.DropSchema(connection); err != nil {
				logger.Error("failed to drop the test schema", "error", err)
			}
		}
	}, nil
}
