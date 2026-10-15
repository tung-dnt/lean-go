package session

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/log"
)

// key32 is a key of exactly 32 bytes, which is used as it is. It is built, not spelled, so that no scanner takes a
// test value for a secret.
var key32 = strings.Repeat("k", 32)

func newStore(t *testing.T, cfg Config) *Store {
	t.Helper()

	if cfg.Key == "" {
		cfg.Key = key32
	}
	s, err := NewStore(cfg)
	require.NoError(t, err)

	return s
}

// cookieOf is the cookie a response set, or nil.
func cookieOf(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}

	return nil
}

// serve runs handler behind the middleware with a request that carries cookies, and returns what it answered.
func serve(s *Store, handler http.HandlerFunc, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Middleware(handler).ServeHTTP(rec, r)

	return rec
}

func TestNewStore(t *testing.T) {
	t.Run("refuses an empty key", func(t *testing.T) {
		_, err := NewStore(Config{})
		require.Error(t, err)
	})

	t.Run("takes a key of any length", func(t *testing.T) {
		for _, key := range []string{"a", "short", key32, "a key that is a good deal longer than thirty-two bytes"} {
			s, err := NewStore(Config{Key: key})
			require.NoError(t, err, key)

			value, err := s.seal(map[string]string{"a": "b"})
			require.NoError(t, err, key)
			values, _, err := s.open(value)
			require.NoError(t, err, key)
			assert.Equal(t, map[string]string{"a": "b"}, values, key)
		}
	})
}

func TestSealOpen(t *testing.T) {
	s := newStore(t, Config{})

	t.Run("a value opens to what was sealed", func(t *testing.T) {
		value, err := s.seal(map[string]string{"user_id": "7", "flash": "héllo"})
		require.NoError(t, err)

		values, previous, err := s.open(value)

		require.NoError(t, err)
		assert.False(t, previous)
		assert.Equal(t, map[string]string{"user_id": "7", "flash": "héllo"}, values)
	})

	t.Run("a value is different every time it is sealed", func(t *testing.T) {
		a, err := s.seal(map[string]string{"a": "b"})
		require.NoError(t, err)
		b, err := s.seal(map[string]string{"a": "b"})
		require.NoError(t, err)

		assert.NotEqual(t, a, b, "the nonce is random")
	})

	t.Run("a value that was changed does not open", func(t *testing.T) {
		value, err := s.seal(map[string]string{"user_id": "7"})
		require.NoError(t, err)
		raw, err := base64.RawURLEncoding.DecodeString(value)
		require.NoError(t, err)

		for i := range raw {
			tampered := append([]byte(nil), raw...)
			tampered[i] ^= 0x01
			_, _, err := s.open(base64.RawURLEncoding.EncodeToString(tampered))
			require.Error(t, err, "byte %d of the value", i)
		}
	})

	t.Run("a value cut short, or that is not base64, does not open", func(t *testing.T) {
		for _, value := range []string{"", "a", "!!!!", "AAAA", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
			_, _, err := s.open(value)
			assert.Error(t, err, value)
		}
	})

	t.Run("a value does not open under the name of another cookie", func(t *testing.T) {
		value, err := s.seal(map[string]string{"user_id": "7"})
		require.NoError(t, err)

		other := newStore(t, Config{Name: "other"})
		_, _, err = other.open(value)

		require.Error(t, err, "the name of the cookie is bound to the value")
	})

	t.Run("a value does not open under another key", func(t *testing.T) {
		value, err := s.seal(map[string]string{"user_id": "7"})
		require.NoError(t, err)

		_, _, err = newStore(t, Config{Key: "another key"}).open(value)

		require.Error(t, err)
	})

	t.Run("a value stops opening when it expires", func(t *testing.T) {
		now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
		s := newStore(t, Config{MaxAge: time.Hour})
		s.now = func() time.Time { return now }
		value, err := s.seal(map[string]string{"user_id": "7"})
		require.NoError(t, err)

		s.now = func() time.Time { return now.Add(time.Hour - time.Second) }
		_, _, err = s.open(value)
		require.NoError(t, err, "just before it expires")

		s.now = func() time.Time { return now.Add(time.Hour) }
		_, _, err = s.open(value)
		require.Error(t, err, "when it expires")
	})
}

func TestKeyRotation(t *testing.T) {
	old := newStore(t, Config{Key: "the old key"})
	value, err := old.seal(map[string]string{"user_id": "7"})
	require.NoError(t, err)

	t.Run("the previous key still opens a value, and says so", func(t *testing.T) {
		rotated := newStore(t, Config{Key: "the new key", PreviousKey: "the old key"})

		values, previous, err := rotated.open(value)

		require.NoError(t, err)
		assert.True(t, previous)
		assert.Equal(t, map[string]string{"user_id": "7"}, values)
	})

	t.Run("without it the value is not good", func(t *testing.T) {
		_, _, err := newStore(t, Config{Key: "the new key"}).open(value)

		require.Error(t, err)
	})

	t.Run("a cookie of the previous key is written again with the new one", func(t *testing.T) {
		rotated := newStore(t, Config{Key: "the new key", PreviousKey: "the old key"})

		rec := serve(rotated, func(_ http.ResponseWriter, _ *http.Request) {}, &http.Cookie{Name: DefaultName, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})

		c := cookieOf(t, rec, DefaultName)
		require.NotNil(t, c, "the visitor did nothing, and the cookie is written for the key's sake")
		values, previous, err := rotated.open(c.Value)
		require.NoError(t, err)
		assert.False(t, previous, "it opens with the current key now")
		assert.Equal(t, "7", values["user_id"])
	})

	t.Run("a previous key that is the current one adds nothing", func(t *testing.T) {
		s := newStore(t, Config{Key: "same", PreviousKey: "same"})

		assert.Len(t, s.keys, 1)
	})
}

func TestMiddleware(t *testing.T) {
	t.Run("a request with no cookie, which changes nothing, gets none", func(t *testing.T) {
		s := newStore(t, Config{})

		rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, ok := From(r.Context())
			require.True(t, ok)
			assert.Empty(t, sess.Get("anything"))
			_, loggedIn := sess.UserID()
			assert.False(t, loggedIn)
		})

		assert.Empty(t, rec.Result().Cookies())
	})

	t.Run("a change is written as an HttpOnly, SameSite=Strict cookie of the age of the store", func(t *testing.T) {
		s := newStore(t, Config{MaxAge: 2 * time.Hour})

		rec := serve(s, func(w http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.SetUserID(42)
			_, err := w.Write([]byte("hello"))
			require.NoError(t, err)
		})

		c := cookieOf(t, rec, DefaultName)
		require.NotNil(t, c)
		assert.True(t, c.HttpOnly)
		assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
		assert.Equal(t, "/", c.Path)
		assert.Equal(t, 7200, c.MaxAge)
		assert.False(t, c.Secure, "this request came over plain HTTP")
		assert.Equal(t, "hello", rec.Body.String())
	})

	t.Run("a cookie sets the session of the next request", func(t *testing.T) {
		s := newStore(t, Config{})
		first := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.SetUserID(42)
			sess.Set("greeting", "hi")
		})

		var userID int64
		var greeting string
		second := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			userID, _ = sess.UserID()
			greeting = sess.Get("greeting")
		}, cookieOf(t, first, DefaultName))

		assert.Equal(t, int64(42), userID)
		assert.Equal(t, "hi", greeting)
		assert.Empty(t, second.Result().Cookies(), "nothing changed, so nothing is written")
	})

	t.Run("the cookie is written however the handler answers", func(t *testing.T) {
		s := newStore(t, Config{})

		for name, handler := range map[string]http.HandlerFunc{
			"a body":         func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) },
			"a status":       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			"a redirect":     func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusSeeOther) },
			"nothing at all": func(_ http.ResponseWriter, _ *http.Request) {},
			"a flush":        func(w http.ResponseWriter, _ *http.Request) { w.(http.Flusher).Flush() },
		} {
			wrapped := func(w http.ResponseWriter, r *http.Request) {
				sess, _ := From(r.Context())
				sess.Set("k", "v")
				handler(w, r)
			}

			rec := serve(s, wrapped)

			assert.NotNil(t, cookieOf(t, rec, DefaultName), name)
		}
	})

	t.Run("the cookie is written before the headers, so a change made after the body started is lost", func(t *testing.T) {
		s := newStore(t, Config{})

		rec := serve(s, func(w http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			_, err := w.Write([]byte("body"))
			require.NoError(t, err)
			sess.Set("too", "late")
		})

		assert.Nil(t, cookieOf(t, rec, DefaultName), "the headers had gone out")
	})

	t.Run("Secure follows TLS", func(t *testing.T) {
		set := func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Set("k", "v")
		}
		always := newStore(t, Config{Secure: true})
		never := newStore(t, Config{})

		assert.True(t, cookieOf(t, serve(always, set), DefaultName).Secure, "configured")

		overTLS := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
		overTLS.TLS = &tls.ConnectionState{}
		rec := httptest.NewRecorder()
		never.Middleware(http.HandlerFunc(set)).ServeHTTP(rec, overTLS)
		assert.True(t, cookieOf(t, rec, DefaultName).Secure, "the request came over TLS")

		proxied := httptest.NewRequest(http.MethodGet, "/", nil)
		proxied.Header.Set("X-Forwarded-Proto", "https")
		rec = httptest.NewRecorder()
		never.Middleware(http.HandlerFunc(set)).ServeHTTP(rec, proxied)
		assert.True(t, cookieOf(t, rec, DefaultName).Secure, "a proxy that terminated TLS said so")
	})

	t.Run("a cookie that is no good is an empty session and is removed", func(t *testing.T) {
		s := newStore(t, Config{})
		valid := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.SetUserID(42)
		})
		good := cookieOf(t, valid, DefaultName)
		tampered := &http.Cookie{Name: DefaultName, Value: good.Value[:len(good.Value)-2] + "AA", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}

		for name, cookie := range map[string]*http.Cookie{
			"tampered":     tampered,
			"garbage":      {Name: DefaultName, Value: "not a sealed value", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode},
			"other key":    {Name: DefaultName, Value: sealedWith(t, "another key"), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode},
			"another name": {Name: "other", Value: good.Value, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode},
		} {
			var loggedIn bool
			rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
				sess, _ := From(r.Context())
				_, loggedIn = sess.UserID()
			}, cookie)

			assert.False(t, loggedIn, name)
			c := cookieOf(t, rec, DefaultName)
			if cookie.Name == DefaultName {
				require.NotNil(t, c, name)
				assert.Equal(t, -1, c.MaxAge, "%s: the browser is told to drop it", name)
			} else {
				assert.Nil(t, c, "%s: there was no session cookie to drop", name)
			}
		}
	})

	t.Run("a cookie that expired is an empty session", func(t *testing.T) {
		s := newStore(t, Config{MaxAge: time.Hour})
		now := time.Now()
		s.now = func() time.Time { return now.Add(-2 * time.Hour) }
		value, err := s.seal(map[string]string{"user_id": "7"})
		require.NoError(t, err)
		s.now = time.Now

		var loggedIn bool
		serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			_, loggedIn = sess.UserID()
		}, &http.Cookie{Name: DefaultName, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})

		assert.False(t, loggedIn)
	})

	t.Run("clearing the session removes the cookie", func(t *testing.T) {
		s := newStore(t, Config{})
		login := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.SetUserID(42)
		})

		rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Clear()
		}, cookieOf(t, login, DefaultName))

		c := cookieOf(t, rec, DefaultName)
		require.NotNil(t, c)
		assert.Equal(t, -1, c.MaxAge)
	})

	t.Run("a session that comes to nothing, with no cookie to begin with, writes none", func(t *testing.T) {
		s := newStore(t, Config{})

		rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Set("flash", "hello")
			sess.Delete("flash")
		})

		assert.Empty(t, rec.Result().Cookies())
	})

	t.Run("setting a value to the one it has rewrites nothing", func(t *testing.T) {
		s := newStore(t, Config{})
		first := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Set("k", "v")
		})

		rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Set("k", "v")
			sess.Delete("missing")
		}, cookieOf(t, first, DefaultName))

		assert.Empty(t, rec.Result().Cookies())
	})

	t.Run("a session too big for a cookie is not written", func(t *testing.T) {
		s := newStore(t, Config{})

		rec := serve(s, func(_ http.ResponseWriter, r *http.Request) {
			sess, _ := From(r.Context())
			sess.Set("big", string(make([]byte, 5000)))
		})

		assert.Nil(t, cookieOf(t, rec, DefaultName), "a browser would drop it, and the visitor would be logged out")
	})

	t.Run("a request without the middleware has no session", func(t *testing.T) {
		_, ok := From(t.Context())

		assert.False(t, ok)
	})

	t.Run("reaches the connection through the writer", func(t *testing.T) {
		s := newStore(t, Config{})
		var reached bool

		serve(s, func(w http.ResponseWriter, _ *http.Request) {
			_, ok := w.(interface{ Unwrap() http.ResponseWriter })
			reached = ok
		})

		assert.True(t, reached, "http.ResponseController needs Unwrap for deadlines")
	})
}

// sealedWith is a value that another store sealed, with the key given.
func sealedWith(t *testing.T, key string) string {
	t.Helper()

	value, err := newStore(t, Config{Key: key}).seal(map[string]string{"user_id": "7"})
	require.NoError(t, err)

	return value
}

func TestSession(t *testing.T) {
	newSession := func() *Session { return &Session{values: map[string]string{}} }

	t.Run("the user id round-trips", func(t *testing.T) {
		sess := newSession()
		_, ok := sess.UserID()
		assert.False(t, ok)

		sess.SetUserID(9)

		id, ok := sess.UserID()
		assert.True(t, ok)
		assert.Equal(t, int64(9), id)
	})

	t.Run("a user id that is not a number is no user", func(t *testing.T) {
		sess := newSession()
		sess.Set(userIDKey, "nine")

		_, ok := sess.UserID()

		assert.False(t, ok)
	})

	t.Run("Clear empties it", func(t *testing.T) {
		sess := newSession()
		sess.Set("a", "b")

		sess.Clear()

		assert.Empty(t, sess.Get("a"))
	})
}

func TestMiddleware__AChangeAfterTheResponseStartedIsLoggedAsLost(t *testing.T) {
	s := newStore(t, Config{})
	var out bytes.Buffer
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(log.NewContext(r.Context(), slog.New(slog.NewTextHandler(&out, nil))))

	rec := httptest.NewRecorder()
	s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := From(r.Context())
		w.WriteHeader(http.StatusNotFound)
		sess.Set("too", "late")
	})).ServeHTTP(rec, r)

	assert.Nil(t, cookieOf(t, rec, DefaultName))
	assert.Contains(t, out.String(), "the session was changed after the response started")

	// A change made in time is not.
	out.Reset()
	s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := From(r.Context())
		sess.Set("in", "time")
		w.WriteHeader(http.StatusNotFound)
	})).ServeHTTP(httptest.NewRecorder(), r)
	assert.Empty(t, out.String())
}
