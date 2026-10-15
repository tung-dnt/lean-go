package htmx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRequest(t *testing.T) {
	t.Run("reads every header htmx sends", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(HeaderRequest, "true")
		r.Header.Set(HeaderBoosted, "true")
		r.Header.Set(HeaderTrigger, "a")
		r.Header.Set(HeaderTriggerName, "b")
		r.Header.Set(HeaderTarget, "c")
		r.Header.Set(HeaderPrompt, "d")
		r.Header.Set(HeaderHistoryRestoreRequest, "true")

		got := GetRequest(r)

		assert.True(t, got.Enabled)
		assert.True(t, got.Boosted)
		assert.True(t, got.HistoryRestore)
		assert.Equal(t, "a", got.Trigger)
		assert.Equal(t, "b", got.TriggerName)
		assert.Equal(t, "c", got.Target)
		assert.Equal(t, "d", got.Prompt)
	})

	t.Run("a request that is not from htmx is the zero Request", func(t *testing.T) {
		assert.Equal(t, Request{}, GetRequest(httptest.NewRequest(http.MethodGet, "/", nil)))
	})
}

func TestResponse_Apply(t *testing.T) {
	t.Run("sets every header", func(t *testing.T) {
		rec := httptest.NewRecorder()

		Response{
			PushURL:            "a",
			Redirect:           "b",
			ReplaceURL:         "f",
			Refresh:            true,
			Trigger:            "c",
			TriggerAfterSwap:   "d",
			TriggerAfterSettle: "e",
		}.Apply(rec)

		h := rec.Header()
		assert.Equal(t, "a", h.Get(HeaderPushURL))
		assert.Equal(t, "b", h.Get(HeaderRedirect))
		assert.Equal(t, "true", h.Get(HeaderRefresh))
		assert.Equal(t, "c", h.Get(HeaderTrigger))
		assert.Equal(t, "d", h.Get(HeaderTriggerAfterSwap))
		assert.Equal(t, "e", h.Get(HeaderTriggerAfterSettle))
		assert.Equal(t, "f", h.Get(HeaderReplaceURL))
	})

	t.Run("NoContent answers 204", func(t *testing.T) {
		rec := httptest.NewRecorder()

		Response{NoContent: true}.Apply(rec)

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
}
