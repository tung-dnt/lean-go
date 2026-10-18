// Package servertest runs the real server for end-to-end tests: server.Run in a goroutine, against a real PostgreSQL,
// with a free port and the same configuration a deployment has, and the
// helpers to drive it the way a browser does. A test makes real HTTP requests, with a cookie jar and without
// following redirects, and reads what comes back; it never reaches into the server's handlers, so nothing that a
// test asserts depends on how the server is wired.
//
// A package of tests starts one server for all of them, in its TestMain:
//
//	var srv *servertest.Server
//
//	func TestMain(m *testing.M) {
//		os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s }))
//	}
//
// and a test that needs a server of its own, with a database of its own to break, starts one:
//
//	srv := servertest.Start(t)
//
// The database is a throwaway postgres container (pkg/testdb) that is started the first time it is needed and
// removed when the tests end, or the one that GO_DATABASE_TESTCONNECTION names, which is what CI does. Each server
// isolates itself within a schema of its own, created, migrated and dropped by server.Run in the test
// environment, so the servers of the packages that go test runs in parallel cannot clobber each other.
//
// The tests of a package share a server, and so a database: a row that a test makes is there for the others, so a test
// makes what it looks for with a name that nothing else has.
//
// An end-to-end test is in the external test package of its domain (package note_test), never in the package
// itself: the server imports every domain, so a test of a domain that imports the server would be an import cycle.
package servertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/server"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/postgres"
	"github.com/tung-dnt/lean-go/pkg/testdb"
)

// startTimeout is how long a server has to answer its readiness probe: a container may have to start first.
const startTimeout = 2 * time.Minute

// Server is a running server, and what a test needs to look at it from the outside and from beside.
type Server struct {
	// URL is the address of the server, "http://127.0.0.1:port", with no trailing slash.
	URL string

	// Config is the configuration that the server runs with: the one that server.Run loads from the same
	// environment.
	Config *config.Config

	// DB is a pool of connections to the database of the server, in its own schema, for the rows a test makes
	// and the rows it looks for. Queries is sqlc over it.
	DB      *pgxpool.Pool
	Queries *db.Queries

	// connection is the database connection of the server, its own schema included, for a server that shares it.
	connection string
	cancel     context.CancelFunc
	done       chan error
	log        *logBuffer
	stopped    sync.Once
	stopErr    error
}

// Log is everything that the server has written to its log so far (what server.Run is given as stderr): the line
// of every request, and the error a failed one ends in, which a test reads to see what the server said of it. It
// grows with the life of the server, so a test that looks for a line of its own request takes the length of the log
// before the request and reads what comes after it.
func (s *Server) Log() string {
	return s.log.String()
}

// logBuffer is the log of a server: it is written by the goroutines of the server and read by the test, and it
// writes to stderr as well, so that the log of a failing test is on the screen as it always was.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// The buffer first: it is what a test reads, and it cannot fail. A stderr that does is the caller's to hear of.
	n, err := l.buf.Write(p)
	if err != nil {
		return n, err
	}

	return os.Stderr.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}

// Option changes how a server is started.
type Option func(env map[string]string)

// WithEnv sets an environment variable of the server, over the ones the test server has: GO_APP_TIMEOUT=2s makes
// a page time out in two seconds, say. The server sees no environment but its own: what is set in the shell the
// tests run from does not reach it.
func WithEnv(key, value string) Option {
	return func(env map[string]string) { env[key] = value }
}

// KeepSchema leaves the schema of the server in place when it stops, for a test that starts another server on it
// (Sharing), as a restart does. The server that shares it drops it.
func KeepSchema() Option {
	return WithEnv("GO_DATABASE_KEEPTESTSCHEMA", "true")
}

// Sharing starts the server on the schema of other, as a second container of the same deployment, or the next start
// of the same one, does. It has a port of its own.
func Sharing(other *Server) Option {
	return WithEnv("GO_DATABASE_TESTCONNECTION", other.connection)
}

// Main starts the server for the tests of a package, hands it to use (which keeps it where the tests can find it),
// runs the tests, and stops the server and the database after them. It is what a TestMain returns to os.Exit.
func Main(m *testing.M, use func(*Server), opts ...Option) int {
	ctx := context.Background()

	stopDB, err := testdb.Setup(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "servertest:", err)

		return 1
	}
	defer stopDB()

	srv, err := start(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "servertest:", err)

		return 1
	}
	use(srv)

	code := m.Run()

	if err := srv.stop(); err != nil {
		fmt.Fprintln(os.Stderr, "servertest:", err)
		if code == 0 {
			code = 1
		}
	}

	return code
}

// Start starts a server for one test, with a schema of its own, and stops it when the test
// is over. Use it for a test that has to break the database, or run with a configuration that the others do not
// (WithEnv); the tests that need nothing of the kind share the server of Main, which costs a start only once.
func Start(tb testing.TB, opts ...Option) *Server {
	tb.Helper()

	if _, err := testdb.Setup(tb.Context()); err != nil {
		tb.Fatalf("servertest: %v", err)
	}

	srv, err := start(tb.Context(), opts)
	if err != nil {
		tb.Fatalf("servertest: %v", err)
	}
	tb.Cleanup(func() {
		if err := srv.stop(); err != nil {
			tb.Errorf("servertest: %v", err)
		}
	})

	return srv
}

// start runs server.Run with the environment of a test server, waits for it to be ready, and opens what a test
// looks at it with.
func start(ctx context.Context, opts []Option) (*Server, error) {
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("no free port: %w", err)
	}

	// The schema is named here, and not by Run, so that the connection is the same for the server and for the
	// pool that the tests use. $RAND is how the connection says "a schema of your own".
	database := os.Getenv(testdb.EnvConnection)
	if database == "" {
		return nil, errors.New("no test database: " + testdb.EnvConnection + " is not set")
	}
	database = postgres.ReplaceRand(database)

	env := map[string]string{
		"GO_APP_ENVIRONMENT":         string(config.EnvTest),
		"GO_APP_HOST":                "http://127.0.0.1:" + strconv.Itoa(port),
		"GO_HTTP_HOSTNAME":           "127.0.0.1",
		"GO_HTTP_PORT":               strconv.Itoa(port),
		"GO_DATABASE_TESTCONNECTION": database,
	}
	for _, opt := range opts {
		opt(env)
	}
	getenv := func(key string) string { return env[key] }

	cfg, err := config.Load(getenv, configDir())
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	log := &logBuffer{}
	go func() {
		done <- server.Run(runCtx, []string{"web", "-config", configDir()}, getenv, io.Discard, log)
	}()

	srv := &Server{
		URL: "http://127.0.0.1:" + strconv.Itoa(port), Config: cfg,
		connection: cfg.Database.TestConnection, cancel: cancel, done: done, log: log,
	}

	if err := srv.waitReady(ctx); err != nil {
		cancel()

		return nil, errors.Join(err, srv.waitDone())
	}

	if srv.DB, err = pgxpool.New(ctx, cfg.Database.TestConnection); err != nil {
		cancel()

		return nil, errors.Join(fmt.Errorf("cannot connect to the test database: %w", err), srv.waitDone())
	}
	srv.Queries = db.New(srv.DB)

	return srv, nil
}

// waitReady polls the readiness probe until the server answers it, or has stopped, or the time is up.
func (s *Server) waitReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	for {
		select {
		case err := <-s.done:
			s.done <- err // kept for waitDone

			return fmt.Errorf("the server stopped before it was ready: %w", err)
		case <-ctx.Done():
			return fmt.Errorf("the server was not ready in %s: %w", startTimeout, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/healthz", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue // not listening yet
		}
		if err := resp.Body.Close(); err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}
}

// waitDone waits for server.Run to return, and returns what it returned.
func (s *Server) waitDone() error {
	select {
	case err := <-s.done:
		return err
	case <-time.After(startTimeout):
		return errors.New("the server did not stop")
	}
}

// Stop stops the server before the test ends, as a deployment stops a container: it stops serving, and what it runs
// in the background stops in the middle of what it was doing. Its schema is dropped unless it was started with
// KeepSchema.
func (s *Server) Stop(t *testing.T) {
	t.Helper()

	if err := s.stop(); err != nil {
		t.Fatalf("servertest: %v", err)
	}
}

// stop shuts the server down, once: the pool of the tests first, then server.Run, which stops serving and drops
// the schema (unless KeepSchema). It returns what went wrong, and a server that stopped for any reason other than
// being told to is one of them.
func (s *Server) stop() error {
	s.stopped.Do(func() {
		if s.DB != nil {
			s.DB.Close()
		}
		s.cancel()
		s.stopErr = s.waitDone()
	})

	return s.stopErr
}

// freePort is a port that nothing listens on. It is free when it is found, and a server that cannot listen on
// it later, which takes someone else to have found it first, fails to start with the error that says so.
func freePort() (int, error) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.Join(fmt.Errorf("%v is not a TCP address", l.Addr()), l.Close())
	}

	return addr.Port, l.Close()
}

// configDir is the config directory of the repository, found from this file so that it does not depend on how deep
// the package under test is.
func configDir() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Join(filepath.Dir(file), "..", "..", "..", "config")
}
