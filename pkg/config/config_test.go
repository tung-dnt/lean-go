package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configDir is where config.yaml is, from this package: two folders up from pkg/config.
const configDir = "../../config"

// env is a getenv over a fixed set of variables, so that a test decides what the configuration sees and the
// developer's shell never does.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoad(t *testing.T) {
	t.Run("reads the file", func(t *testing.T) {
		cfg, err := Load(env(nil), configDir)
		require.NoError(t, err)

		assert.Equal(t, uint16(8000), cfg.HTTP.Port)
		assert.Equal(t, EnvLocal, cfg.App.Environment)
	})

	t.Run("a variable overrides a key of the file", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{
			"GO_APP_ENVIRONMENT":  "abc",
			"GO_HTTP_PORT":        "9001",
			"GO_HTTP_TLS_ENABLED": "true",
		}), configDir)
		require.NoError(t, err)

		assert.Equal(t, environment("abc"), cfg.App.Environment)
		assert.Equal(t, uint16(9001), cfg.HTTP.Port, "a number is read from its text")
		assert.True(t, cfg.HTTP.TLS.Enabled, "and so is a key of a nested section")
	})

	t.Run("a variable that is empty keeps the value of the file", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{"GO_HTTP_PORT": ""}), configDir)
		require.NoError(t, err)

		assert.Equal(t, uint16(8000), cfg.HTTP.Port)
	})

	t.Run("a variable is read from the environment it is given, and from no other", func(t *testing.T) {
		t.Setenv("GO_HTTP_PORT", "9002")

		cfg, err := Load(env(nil), configDir)
		require.NoError(t, err)

		assert.Equal(t, uint16(8000), cfg.HTTP.Port)
	})

	t.Run("a missing file is an error", func(t *testing.T) {
		// The default directories are searched relative to the working directory too, so run from an empty
		// one: from pkg/config the repository's own config.yaml is within reach of those defaults.
		dir := t.TempDir()
		t.Chdir(dir)
		_, err := Load(env(nil), dir)
		require.Error(t, err)
	})
}

func TestSession(t *testing.T) {
	t.Run("the key and the age are in the committed config", func(t *testing.T) {
		cfg, err := Load(env(nil), configDir)
		require.NoError(t, err)

		assert.NotEmpty(t, cfg.App.EncryptionKey)
		assert.Empty(t, cfg.App.PreviousEncryptionKey, "there is no key being rotated out")
		assert.Equal(t, 30*24*time.Hour, cfg.App.SessionMaxAge)
	})

	t.Run("the previous key can be given by the environment", func(t *testing.T) {
		cfg, err := Load(env(map[string]string{
			"GO_APP_ENCRYPTIONKEY":         "new",
			"GO_APP_PREVIOUSENCRYPTIONKEY": "old",
			"GO_APP_SESSIONMAXAGE":         "1h",
		}), configDir)
		require.NoError(t, err)

		assert.Equal(t, "new", cfg.App.EncryptionKey)
		assert.Equal(t, "old", cfg.App.PreviousEncryptionKey)
		assert.Equal(t, time.Hour, cfg.App.SessionMaxAge)
	})
}

func TestContext(t *testing.T) {
	cfg := &Config{App: AppConfig{Name: "testing"}}

	assert.Nil(t, FromContext(t.Context()), "a context with no configuration has none")
	assert.Same(t, cfg, FromContext(NewContext(t.Context(), cfg)))
}
