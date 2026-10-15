package middleware_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/htmx"
	"github.com/tung-dnt/lean-go/pkg/middleware"
)

func TestSecure(t *testing.T) {
	rec := get(middleware.Secure(okHandler), "/")

	// The three headers Echo's Secure sent, with its values.
	assert.Equal(t, "1; mode=block", rec.Header().Get("X-XSS-Protection"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"), "Echo's did not send HSTS by default, and neither does this")
	assert.Equal(t, "ok", rec.Body.String())
}

func TestWithConfig(t *testing.T) {
	cfg := &config.Config{}
	var got *config.Config

	do(middleware.WithConfig(cfg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = config.FromContext(r.Context())
	})), httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Same(t, cfg, got)
}

func TestCacheControl(t *testing.T) {
	assert.Equal(t, "public, max-age=5", get(middleware.CacheControl(5*time.Second)(okHandler), "/").Header().Get("Cache-Control"))
	assert.Equal(t, "no-cache, no-store", get(middleware.CacheControl(0)(okHandler), "/").Header().Get("Cache-Control"))
}

func TestCacheBoostedPages(t *testing.T) {
	cached := middleware.CacheBoostedPages(time.Minute)(okHandler)

	boosted := func(method string) *http.Request {
		r := httptest.NewRequest(method, "/", nil)
		r.Header.Set(htmx.HeaderBoosted, "true")

		return r
	}

	// A boosted GET (what a preload / boosted click sends) is privately cacheable
	// so the warmed response can be reused on click.
	r := boosted(http.MethodGet)
	r.Header.Set(htmx.HeaderRequest, "true")
	assert.Equal(t, "private, max-age=60", do(cached, r).Header().Get("Cache-Control"))

	// A non-boosted request (direct load or a partial htmx fragment) is left
	// uncached, so a fragment can never be served in place of a full page.
	assert.Empty(t, get(cached, "/").Header().Get("Cache-Control"))
	fragment := httptest.NewRequest(http.MethodGet, "/", nil)
	fragment.Header.Set(htmx.HeaderRequest, "true")
	assert.Empty(t, do(cached, fragment).Header().Get("Cache-Control"))

	// A non-GET boosted request is never cached.
	assert.Empty(t, do(cached, boosted(http.MethodPost)).Header().Get("Cache-Control"))

	// A zero maxAge disables caching.
	assert.Empty(t, do(middleware.CacheBoostedPages(0)(okHandler), boosted(http.MethodGet)).Header().Get("Cache-Control"))
}

func TestRemoveTrailingSlash(t *testing.T) {
	h := middleware.RemoveTrailingSlash(okHandler)

	t.Run("redirects an address with a slash to the one without, for good", func(t *testing.T) {
		for target, want := range map[string]string{
			"/packs/cat/":           "/packs/cat",
			"/login/":               "/login",
			"/packs/cat/?page=2":    "/packs/cat?page=2",
			"/a/b/c/":               "/a/b/c",
			"/search/?q=a&tag=b%2F": "/search?q=a&tag=b%2F",
		} {
			rec := get(h, target)

			assert.Equal(t, http.StatusMovedPermanently, rec.Code, target)
			assert.Equal(t, want, rec.Header().Get("Location"), target)
		}
	})

	t.Run("leaves the others alone", func(t *testing.T) {
		for _, target := range []string{"/", "/packs/cat", "/login?x=/", "/static/app.js"} {
			rec := get(h, target)

			assert.Equal(t, http.StatusOK, rec.Code, target)
		}
	})

	t.Run("never redirects to another site", func(t *testing.T) {
		// "//evil.example/" with its slash removed is "//evil.example", which a browser takes for another site.
		for _, target := range []string{"//evil.example/", "///evil.example/", `/\evil.example/`, `\\evil.example/`} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.URL.Path = target
			r.RequestURI = target

			rec := do(h, r)

			require.Equal(t, http.StatusMovedPermanently, rec.Code, target)
			assert.Equal(t, "/evil.example", rec.Header().Get("Location"), "%s stays on this site", target)
		}
	})
}

func TestHTTPSRedirect(t *testing.T) {
	h := middleware.HTTPSRedirect(okHandler)

	t.Run("sends a plain request to the same address over https", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "http://example.com/packs/cat?page=2", nil)

		rec := do(h, r)

		assert.Equal(t, http.StatusMovedPermanently, rec.Code)
		assert.Equal(t, "https://example.com/packs/cat?page=2", rec.Header().Get("Location"))
	})

	t.Run("lets a request that came in over TLS through", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
		r.TLS = &tls.ConnectionState{}

		assert.Equal(t, http.StatusOK, do(h, r).Code)
	})

	t.Run("believes a proxy that says it terminated TLS", func(t *testing.T) {
		for header, value := range map[string]string{
			"X-Forwarded-Proto":    "https",
			"X-Forwarded-Protocol": "https",
			"X-Forwarded-Ssl":      "on",
			"X-Url-Scheme":         "https",
		} {
			r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			r.Header.Set(header, value)

			assert.Equal(t, http.StatusOK, do(h, r).Code, header)
		}

		r := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		r.Header.Set("X-Forwarded-Proto", "http")
		require.Equal(t, http.StatusMovedPermanently, do(h, r).Code, "a proxy that says it did not")
	})
}
