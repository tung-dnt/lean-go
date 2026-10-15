// Package session keeps what a browser carries from one request to the next, who is logged in and the flash
// messages that wait for the next page, in one cookie that the server seals.
//
// The cookie is AES-256-GCM: the value is a random nonce followed by the sealed payload, which is JSON that holds
// the values and the moment the cookie expires. GCM authenticates what it seals, so a cookie that was changed in
// any way does not open, and there is no separate signature. The name of the cookie is bound to the value as its
// additional data, so a value cannot be moved to a cookie of another name. Nothing is kept on the server: a
// session is only what its cookie says, which is also why a cookie that was copied before the visitor logged out
// is good until it expires.
//
// A cookie that does not open, because it was changed, has expired, or was sealed with a key that is gone, is a
// session with nothing in it. It is never an error page: a visitor whose cookie went bad is a visitor who is
// not logged in, and the response removes the cookie.
//
// The handlers do not save the session. Store.Middleware loads it into the context of the request, a handler
// changes it through From, and the middleware writes the cookie, only when something changed, as the response
// starts. Nothing has to be done before the page is written, and nothing is lost when a handler redirects.
package session

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/tung-dnt/lean-go/pkg/log"
)

const (
	// DefaultName is the name of the cookie when the Config gives none.
	DefaultName = "session"

	// DefaultMaxAge is how long a cookie lasts when the Config gives no age: 30 days.
	DefaultMaxAge = 30 * 24 * time.Hour

	// maxValueLen is the longest cookie value that is sent. Browsers drop a cookie of more than 4096 bytes in
	// all, name and attributes included, and a dropped cookie is a visitor who is silently logged out.
	maxValueLen = 4000

	// userIDKey is the key of the id of the logged in user.
	userIDKey = "user_id"
)

// errInvalid is why a cookie does not open. It is never shown: the session is empty instead.
var errInvalid = errors.New("session: the cookie is not valid")

// Config is what a Store is made of.
type Config struct {
	// Name is the name of the cookie. DefaultName when it is empty.
	Name string

	// Key seals the cookies: config App.EncryptionKey. A key of exactly 32 bytes is the AES-256 key as it
	// is, any other length is stretched to 32 bytes with SHA-256. It may not be empty.
	Key string

	// PreviousKey is the key that Key replaced, or empty. A cookie that only it opens is accepted, and sealed
	// with Key the next time the cookie is written, so that rotating the key logs nobody out.
	PreviousKey string

	// MaxAge is how long a cookie is good for, counted from the response that wrote it. DefaultMaxAge when it
	// is not positive.
	MaxAge time.Duration

	// Secure marks the cookie Secure whatever the request, for when TLS is on. A request that came in over TLS,
	// or through a proxy that says so with X-Forwarded-Proto, gets a Secure cookie in any case.
	Secure bool
}

// Store seals and opens the session cookie of the application.
type Store struct {
	name   string
	keys   []cipher.AEAD // the key first, then the previous key
	maxAge time.Duration
	always bool // the cookie is Secure whatever the request, for when TLS is on
	now    func() time.Time
}

// NewStore makes the Store for cfg. It fails when there is no key, since a session that anybody can write is
// worse than none.
func NewStore(cfg Config) (*Store, error) {
	if cfg.Key == "" {
		return nil, errors.New("session: no encryption key (config app.encryptionKey)")
	}

	current, err := newAEAD(cfg.Key)
	if err != nil {
		return nil, err
	}
	keys := []cipher.AEAD{current}

	if cfg.PreviousKey != "" && cfg.PreviousKey != cfg.Key {
		previous, err := newAEAD(cfg.PreviousKey)
		if err != nil {
			return nil, err
		}
		keys = append(keys, previous)
	}

	s := &Store{name: cfg.Name, keys: keys, maxAge: cfg.MaxAge, always: cfg.Secure, now: time.Now}
	if s.name == "" {
		s.name = DefaultName
	}
	if s.maxAge <= 0 {
		s.maxAge = DefaultMaxAge
	}

	return s, nil
}

// newAEAD is AES-256-GCM under key, stretched to 32 bytes when it is not that long already.
func newAEAD(key string) (cipher.AEAD, error) {
	k := []byte(key)
	if len(k) != 32 {
		sum := sha256.Sum256(k)
		k = sum[:]
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("session: create the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("session: create GCM: %w", err)
	}

	return aead, nil
}

// payload is what a cookie seals.
type payload struct {
	// Exp is when the cookie stops being good, in Unix seconds.
	Exp    int64             `json:"exp"`
	Values map[string]string `json:"v"`
}

// seal is the cookie value for values: valid for the age of the Store from now, under the current key.
func (s *Store) seal(values map[string]string) (string, error) {
	plain, err := json.Marshal(payload{Exp: s.now().Add(s.maxAge).Unix(), Values: values})
	if err != nil {
		return "", fmt.Errorf("session: encode: %w", err)
	}

	aead := s.keys[0]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("session: nonce: %w", err)
	}

	// The nonce is the front of the value, and the ciphertext is appended to it.
	return base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte(s.name))), nil
}

// open is the values a cookie value seals, and whether it was the previous key that opened it. A value that
// does not open, or has expired, is errInvalid.
func (s *Store) open(value string) (values map[string]string, previous bool, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false, errInvalid
	}

	for i, aead := range s.keys {
		if len(raw) < aead.NonceSize() {
			return nil, false, errInvalid
		}
		nonce, sealed := raw[:aead.NonceSize()], raw[aead.NonceSize():]

		plain, err := aead.Open(nil, nonce, sealed, []byte(s.name))
		if err != nil {
			continue
		}

		var p payload
		if err := json.Unmarshal(plain, &p); err != nil || s.now().Unix() >= p.Exp {
			return nil, false, errInvalid
		}
		if p.Values == nil {
			p.Values = map[string]string{}
		}

		return p.Values, i > 0, nil
	}

	return nil, false, errInvalid
}

// Session is the values of one visitor for the length of one request. It is safe for use from several
// goroutines, since a handler may hand the request to some.
type Session struct {
	mu     sync.Mutex
	values map[string]string

	// present is whether the request came with a cookie, which a session that ends up empty has to remove.
	present bool

	// dirty is whether the cookie has to be written with the response.
	dirty bool
}

// Get is the value of key, or "" when there is none.
func (s *Session) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.values[key]
}

// Set gives key the value. A value that is the one it had changes nothing, and does not rewrite the cookie.
func (s *Session) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if old, ok := s.values[key]; ok && old == value {
		return
	}
	s.values[key] = value
	s.dirty = true
}

// Delete removes key.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.values[key]; !ok {
		return
	}
	delete(s.values, key)
	s.dirty = true
}

// Clear removes everything, and with it the cookie. It is what logging out is.
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.values) == 0 {
		return
	}
	clear(s.values)
	s.dirty = true
}

// ClearUserID forgets who is logged in, and keeps the rest of the session.
func (s *Session) ClearUserID() {
	s.Delete(userIDKey)
}

// UserID is the id of the logged in user, and false when nobody is logged in: the one fact the application keeps
// of every visitor.
func (s *Session) UserID() (int64, bool) {
	id, err := strconv.ParseInt(s.Get(userIDKey), 10, 64)
	if err != nil {
		return 0, false
	}

	return id, true
}

// SetUserID logs the user in.
func (s *Session) SetUserID(id int64) {
	s.Set(userIDKey, strconv.FormatInt(id, 10))
}

// ctxKey is the key of the session in a context.
type ctxKey struct{}

// NewContext returns a copy of ctx that carries sess, for From. Middleware does this for every request; it is
// public so that a test can make a context with a session in it.
func NewContext(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, sess)
}

// From is the session of the request that ctx belongs to. It reports false for a request that did not go
// through Store.Middleware, such as one for a static file: it has no session, and a handler that has to change one
// has to say so (an error) and not lose the change.
func From(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(ctxKey{}).(*Session)

	return sess, ok
}

// load is the session of a request: what its cookie holds, or nothing.
func (s *Store) load(r *http.Request) *Session {
	sess := &Session{values: map[string]string{}}

	c, err := r.Cookie(s.name)
	if err != nil {
		return sess
	}
	sess.present = true

	values, previous, err := s.open(c.Value)
	if err != nil {
		// Not a session, so no session, and the response sees to it that the browser stops sending the cookie.
		sess.dirty = true

		return sess
	}

	sess.values = values
	// Opened with the previous key: write it again, with the current one.
	sess.dirty = previous

	return sess
}

// secure reports whether the cookie of r is to be marked Secure.
func (s *Store) secure(r *http.Request) bool {
	return s.always || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// commit writes the cookie that sess comes to, if it came to anything other than what the request carried.
func (s *Store) commit(w http.ResponseWriter, r *http.Request, sess *Session) {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if !sess.dirty {
		return
	}
	sess.dirty = false

	c := &http.Cookie{
		Name:     s.name,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	if s.secure(r) {
		c.Secure = true
	}

	if len(sess.values) == 0 {
		if !sess.present {
			return // there is no cookie to remove
		}
		c.MaxAge = -1
		http.SetCookie(w, c)

		return
	}

	value, err := s.seal(sess.values)
	if err != nil {
		log.Ctx(r.Context()).Error("session not saved", "error", err)

		return
	}
	if len(value) > maxValueLen {
		log.Ctx(r.Context()).Error("session not saved: it holds too much for a cookie", "bytes", len(value))

		return
	}

	c.Value = value
	c.MaxAge = int(s.maxAge / time.Second)
	http.SetCookie(w, c)
}

// Middleware loads the session of every request into its context and writes the cookie when the handler changed
// the session. The cookie goes out with the headers of the response, whenever the handler sends them, or at the
// end when it sent none. A change made after the headers are out cannot be saved, and is logged: it is a handler
// that renders a page after it has written its status.
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.load(r)
		cw := &commitWriter{ResponseWriter: w, commit: func() { s.commit(w, r, sess) }}

		next.ServeHTTP(cw, r.WithContext(NewContext(r.Context(), sess)))

		if cw.committed {
			sess.mu.Lock()
			late := sess.dirty
			sess.mu.Unlock()
			if late {
				log.Ctx(r.Context()).Warn("the session was changed after the response started, and the change is lost")
			}
		}
		cw.commitOnce()
	})
}

// commitWriter runs commit once, before the headers of the response are written.
type commitWriter struct {
	http.ResponseWriter
	commit    func()
	committed bool
}

func (w *commitWriter) commitOnce() {
	if w.committed {
		return
	}
	w.committed = true
	w.commit()
}

// WriteHeader implements http.ResponseWriter.
func (w *commitWriter) WriteHeader(code int) {
	// A 1xx is informational and the final headers are still to come: the cookie goes with those.
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.commitOnce()
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (w *commitWriter) Write(b []byte) (int, error) {
	w.commitOnce()

	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher: what is flushed is the headers as well.
func (w *commitWriter) Flush() {
	w.commitOnce()
	// A writer that cannot flush has nothing to flush, which is not worth a word; any other failure is the
	// connection's, and the handler is about to find that out for itself.
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Default().Debug("session: flush", "error", err)
	}
}

// Unwrap lets http.ResponseController reach the writer beneath, for its deadlines and for hijacking.
func (w *commitWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
