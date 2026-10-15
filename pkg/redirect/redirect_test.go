package redirect

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tung-dnt/lean-go/pkg/htmx"
)

func TestTo(t *testing.T) {
	t.Run("redirects with the status it was given", func(t *testing.T) {
		for _, code := range []int{http.StatusSeeOther, http.StatusFound, http.StatusTemporaryRedirect} {
			rec := httptest.NewRecorder()

			To(rec, httptest.NewRequest(http.MethodPost, "/login", nil), "/path/one?a=1&b=2", code)

			assert.Equal(t, code, rec.Code)
			assert.Equal(t, "/path/one?a=1&b=2", rec.Header().Get("Location"))
			assert.Empty(t, rec.Header().Get(htmx.HeaderRedirect))
		}
	})

	t.Run("takes an absolute URL", func(t *testing.T) {
		rec := httptest.NewRecorder()

		To(rec, httptest.NewRequest(http.MethodGet, "/", nil), "https://localhost.dev?a=1", http.StatusFound)

		assert.Equal(t, "https://localhost.dev?a=1", rec.Header().Get("Location"))
	})

	t.Run("a boosted request is told to navigate, and is not redirected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.Header.Set(htmx.HeaderBoosted, "true")

		To(rec, r, "/path/one?a=1&b=2", http.StatusSeeOther)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "/path/one?a=1&b=2", rec.Header().Get(htmx.HeaderRedirect))
		assert.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("an htmx request that is not boosted is redirected like any other", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(htmx.HeaderRequest, "true")

		To(rec, r, "/", http.StatusSeeOther)

		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Empty(t, rec.Header().Get(htmx.HeaderRedirect))
	})
}
