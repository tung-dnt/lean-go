// Package config is the application's configuration: config/config.yaml with every key overridable from the
// environment as GO_<SECTION>_<KEY> (GO_HTTP_PORT, GO_DATABASE_CONNECTION, GO_APP_ENCRYPTIONKEY, ...).
package config

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type environment string

const (
	// EnvLocal represents the local environment.
	EnvLocal environment = "local"

	// EnvTest represents the test environment.
	EnvTest environment = "test"

	// EnvDevelopment represents the development environment.
	EnvDevelopment environment = "dev"

	// EnvStaging represents the staging environment.
	EnvStaging environment = "staging"

	// EnvQA represents the qa environment.
	EnvQA environment = "qa"

	// EnvProduction represents the production environment.
	EnvProduction environment = "prod"
)

type (
	// Config stores complete configuration.
	Config struct {
		HTTP     HTTPConfig
		App      AppConfig
		Cache    CacheConfig
		Database DatabaseConfig
	}

	// HTTPConfig stores HTTP configuration.
	HTTPConfig struct {
		Hostname        string
		Port            uint16
		ReadTimeout     time.Duration
		WriteTimeout    time.Duration
		IdleTimeout     time.Duration
		ShutdownTimeout time.Duration
		TLS             struct {
			Enabled     bool
			Certificate string
			Key         string
		}
	}

	// AppConfig stores application configuration.
	AppConfig struct {
		Name string
		Host string

		Environment environment

		// EncryptionKey seals the session cookie. A key of exactly 32 bytes is used as it is; any other length
		// is stretched to 32 bytes with SHA-256 (see pkg/session). Change it on every live
		// environment: it is in config.yaml, so the committed value is public.
		EncryptionKey string

		// PreviousEncryptionKey is the key that EncryptionKey replaced. A cookie sealed with it is still
		// accepted, and is sealed again with the current key the next time the session is saved, so rotating the
		// key does not log everybody out. Leave it empty when there is none.
		PreviousEncryptionKey string

		// SessionMaxAge is how long a session cookie is good for, counted from the response that last set it.
		SessionMaxAge time.Duration

		Timeout time.Duration
	}

	// CacheConfig stores the cache configuration.
	CacheConfig struct {
		Expiration struct {
			PublicFile time.Duration
			// Page is how long a boosted/preloaded full-page GET may be reused
			// from the user's own browser cache, so htmx-ext-preload's warmed
			// navigations can be served on click instead of re-fetched.
			Page time.Duration
		}
	}

	// DatabaseConfig stores the database configuration.
	DatabaseConfig struct {
		// Connection is the PostgreSQL connection string used for application data.
		Connection string

		// TestConnection is the PostgreSQL connection string used when running tests.
		// $RAND will be automatically replaced with a random value, which is used to give each test
		// process its own throwaway schema via search_path.
		TestConnection string
	}
)

// defaultDirs are where config.yaml is looked for after the directories Load is given: the working directory,
// its config folder, and the config folder one and two levels up, which is where the file is when the process
// runs from the repository root, from the folder of a command, or from the folder of a package under test.
var defaultDirs = []string{".", "config", "../config", "../../config"}

// Load reads config.yaml and applies the overrides getenv knows of. It is given the environment as a function,
// and not read from the process, so that a caller (the server, a command, a test) decides what the
// configuration sees.
//
// The file is searched for in dirs first and then in defaultDirs. Every key of the file can be overridden with
// the variable GO_<SECTION>_<KEY>, upper case, with a dot in the key written as an underscore:
// http.port is GO_HTTP_PORT and database.testConnection is GO_DATABASE_TESTCONNECTION. A variable that is empty
// is the same as one that is not set, so the value in the file is kept; and a key has to be in the file for its
// variable to be read, which is why config.yaml lists every key, with an empty value for a secret.
func Load(getenv func(string) string, dirs ...string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	for _, dir := range slices.Concat(dirs, defaultDirs) {
		v.AddConfigPath(dir)
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	for _, key := range v.AllKeys() {
		name := "GO_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if value := getenv(name); value != "" {
			v.Set(key, value)
		}
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &c, nil
}

// ctxKey is the key of the configuration in a context.
type ctxKey struct{}

// NewContext returns a copy of ctx that carries cfg, for FromContext. The server puts the configuration in the
// context of every request so that the page shell can read it without being handed it.
func NewContext(ctx context.Context, cfg *Config) context.Context {
	return context.WithValue(ctx, ctxKey{}, cfg)
}

// FromContext is the configuration NewContext put in ctx, or nil when there is none.
func FromContext(ctx context.Context) *Config {
	cfg, _ := ctx.Value(ctxKey{}).(*Config)

	return cfg
}
