package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/middleware"
)

func TestCrossOrigin(t *testing.T) {
	mw, err := middleware.CrossOrigin(fail, "https://leango.example")
	require.NoError(t, err)
	h := mw(okHandler)

	post := func(headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://leango.local/login", strings.NewReader("a=b"))
		for k, v := range headers {
			r.Header.Set(k, v)
		}

		return do(h, r)
	}

	t.Run("refuses a post that the browser says is from another site", func(t *testing.T) {
		for _, headers := range []map[string]string{
			{"Sec-Fetch-Site": "cross-site"},
			{"Sec-Fetch-Site": "same-site"},
			{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
			{"Origin": "https://evil.example"},
			{"Origin": "http://leango.local.evil.example"},
		} {
			assert.Equal(t, http.StatusForbidden, post(headers).Code, "%v", headers)
		}
	})

	t.Run("lets a post of the site itself through", func(t *testing.T) {
		for _, headers := range []map[string]string{
			{"Sec-Fetch-Site": "same-origin"},
			{"Sec-Fetch-Site": "none"},
			{"Origin": "http://leango.local"},
			{"Sec-Fetch-Site": "same-origin", "Origin": "http://leango.local"},
		} {
			assert.Equal(t, http.StatusOK, post(headers).Code, "%v", headers)
		}
	})

	t.Run("lets a post that is not from a browser through: it has neither header", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, post(nil).Code)
	})

	t.Run("lets the trusted origins through, which is the site's address behind a proxy that changes the Host", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, post(map[string]string{"Origin": "https://leango.example"}).Code)
		assert.Equal(t, http.StatusForbidden, post(map[string]string{"Origin": "http://leango.example"}).Code, "the scheme is part of it")
	})

	t.Run("a safe method is never refused", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
			r := httptest.NewRequest(method, "/", nil)
			r.Header.Set("Sec-Fetch-Site", "cross-site")

			assert.Equal(t, http.StatusOK, do(h, r).Code, method)
		}
	})

	t.Run("the refusal is the error page's, and not the standard library's", func(t *testing.T) {
		rec := post(map[string]string{"Sec-Fetch-Site": "cross-site"})

		assert.Contains(t, rec.Body.String(), "cross-origin request refused", "it went through fail")
	})
}

func TestCrossOrigin__AnOriginThatCannotBeOneIsAnError(t *testing.T) {
	for _, origin := range []string{"leango.example", "https://", "https://leango.example/path", "://x"} {
		_, err := middleware.CrossOrigin(fail, origin)

		assert.Error(t, err, origin)
	}
}
