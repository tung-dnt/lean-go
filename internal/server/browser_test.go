//go:build browser

package server_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/server/servertest"
)

func TestBrowser(t *testing.T) {
	account := srv.NewAccount(t, true)
	cmd := exec.CommandContext(t.Context(), "bunx", "--bun", "playwright", "test")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(),
		"LEANGO_BROWSER_URL="+srv.URL,
		"LEANGO_BROWSER_EMAIL="+account.Email,
		"LEANGO_BROWSER_PASSWORD="+servertest.AccountPassword,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "browser regression suite failed; see test-results and playwright-report")
}
