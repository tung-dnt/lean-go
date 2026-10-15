package server_test

import (
	"os"
	"testing"

	"github.com/tung-dnt/lean-go/internal/server/servertest"
)

// srv is the real server that these tests drive: server.Run, over a real PostgreSQL, started once for all of them.
var srv *servertest.Server

func TestMain(m *testing.M) {
	os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s }))
}
