package htmx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRequest(t *testing.T) {
	t.Run("reads htmx v4 request headers", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(HeaderRequest, "true")
		r.Header.Set(HeaderBoosted, "true")
		r.Header.Set(HeaderSource, "button#submit")
		r.Header.Set(HeaderTarget, "main#content")
		r.Header.Set(HeaderRequestType, "fetch")

		got := GetRequest(r)

		assert.Equal(t, Request{
			Enabled:     true,
			Boosted:     true,
			Source:      "button#submit",
			Target:      "main#content",
			RequestType: "fetch",
		}, got)
	})

	t.Run("a request that is not from htmx is the zero Request", func(t *testing.T) {
		assert.Equal(t, Request{}, GetRequest(httptest.NewRequest(http.MethodGet, "/", nil)))
	})
}

func TestResponse_Apply(t *testing.T) {
	t.Run("sets supported htmx v4 response headers", func(t *testing.T) {
		rec := httptest.NewRecorder()

		Response{
			PushURL:    "a",
			Redirect:   "b",
			Refresh:    true,
			Trigger:    "c",
			ReplaceURL: "d",
		}.Apply(rec)

		h := rec.Header()
		assert.Equal(t, "a", h.Get(HeaderPushURL))
		assert.Equal(t, "b", h.Get(HeaderRedirect))
		assert.Equal(t, "true", h.Get(HeaderRefresh))
		assert.Equal(t, "c", h.Get(HeaderTrigger))
		assert.Equal(t, "d", h.Get(HeaderReplaceURL))
	})

	t.Run("NoContent answers 204", func(t *testing.T) {
		rec := httptest.NewRecorder()

		Response{NoContent: true}.Apply(rec)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}
