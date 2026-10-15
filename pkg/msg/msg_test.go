package msg_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// visit runs handler behind the session middleware with the cookies the visitor has, and returns the cookies
// they have after it. It is the way a browser carries the messages from one request to the next.
func visit(t *testing.T, store *session.Store, cookies []*http.Cookie, handler http.HandlerFunc) []*http.Cookie {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	store.Middleware(handler).ServeHTTP(rec, r)

	if set := rec.Result().Cookies(); len(set) > 0 {
		return set
	}

	return cookies
}

func newStore(t *testing.T) *session.Store {
	t.Helper()

	store, err := session.NewStore(session.Config{Key: "test key"})
	require.NoError(t, err)

	return store
}

func TestMessages(t *testing.T) {
	store := newStore(t)

	t.Run("a message set by one request is shown by the next, once", func(t *testing.T) {
		for typ, set := range map[msg.Type]func(ctx context.Context, text string){
			msg.TypeSuccess: msg.Success,
			msg.TypeInfo:    msg.Info,
			msg.TypeWarning: msg.Warning,
			msg.TypeError:   msg.Error,
		} {
			text := "hello " + string(typ)
			cookies := visit(t, store, nil, func(w http.ResponseWriter, r *http.Request) {
				set(r.Context(), text)
				w.WriteHeader(http.StatusSeeOther)
			})

			cookies = visit(t, store, cookies, func(_ http.ResponseWriter, r *http.Request) {
				assert.Equal(t, []string{text}, msg.Get(r.Context(), typ), "the next page shows it")
				assert.Empty(t, msg.Get(r.Context(), typ), "and shows it once")
			})

			visit(t, store, cookies, func(_ http.ResponseWriter, r *http.Request) {
				assert.Empty(t, msg.Get(r.Context(), typ), "a page after that does not")
			})
		}
	})

	t.Run("messages come back in the order they were set, by type", func(t *testing.T) {
		cookies := visit(t, store, nil, func(_ http.ResponseWriter, r *http.Request) {
			msg.Success(r.Context(), "one")
			msg.Error(r.Context(), "other")
			msg.Success(r.Context(), "two")
			msg.Set(r.Context(), msg.TypeSuccess, "three")
		})

		visit(t, store, cookies, func(_ http.ResponseWriter, r *http.Request) {
			assert.Equal(t, []string{"one", "two", "three"}, msg.Get(r.Context(), msg.TypeSuccess))
			assert.Equal(t, []string{"other"}, msg.Get(r.Context(), msg.TypeError))
			assert.Empty(t, msg.Get(r.Context(), msg.TypeInfo))
		})
	})

	t.Run("a message set and shown by the same request leaves nothing behind", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		rec := httptest.NewRecorder()

		store.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			msg.Error(r.Context(), "Invalid credentials.")
			assert.Equal(t, []string{"Invalid credentials."}, msg.Get(r.Context(), msg.TypeError))
		})).ServeHTTP(rec, r)

		assert.Empty(t, rec.Result().Cookies())
	})

	t.Run("messages are text, whatever is in them", func(t *testing.T) {
		const text = `<b>"quoted"</b> & “curly” \n ✓`
		cookies := visit(t, store, nil, func(_ http.ResponseWriter, r *http.Request) {
			msg.Info(r.Context(), text)
		})

		visit(t, store, cookies, func(_ http.ResponseWriter, r *http.Request) {
			assert.Equal(t, []string{text}, msg.Get(r.Context(), msg.TypeInfo))
		})
	})

	t.Run("a request with no session has none, and cannot keep one", func(t *testing.T) {
		assert.Empty(t, msg.Get(t.Context(), msg.TypeSuccess))

		assert.NotPanics(t, func() { msg.Success(t.Context(), "nowhere to go") })
	})
}
