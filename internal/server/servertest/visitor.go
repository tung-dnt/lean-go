package servertest

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/paths"
)

// AccountPassword is the password that NewAccount gives every account it makes.
const AccountPassword = "password"

// NewAccount creates an account, and makes it the admin when admin is set. Its email is not any other's.
func (s *Server) NewAccount(t *testing.T, admin bool) *db.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(AccountPassword), bcrypt.MinCost)
	require.NoError(t, err)

	seed := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), strings.ToLower(rand.Text()))
	u, err := s.Queries.CreateUser(t.Context(), db.CreateUserParams{
		Name:     "Test User " + seed,
		Email:    fmt.Sprintf("testuser-%s@localhost.localhost", seed),
		Password: string(hash),
		Verified: false,
		Admin:    admin,
	})
	require.NoError(t, err)

	return &u
}

// NewVisitor is a browser that has never been to the site: it keeps its own cookies, and shows the redirect
// it is sent on instead of following it, because where a request is sent is often what a test is about.
func NewVisitor(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// NewRequest is a request for a path of the site, with the context of the test. body may be nil.
func (s *Server) NewRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, s.URL+path, body)
	require.NoError(t, err)

	return req
}

// Do sends the request as the visitor and returns the response with its body, which is read and closed.
func (s *Server) Do(t *testing.T, visitor *http.Client, req *http.Request) (*http.Response, string) {
	t.Helper()

	resp, err := visitor.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return resp, string(body)
}

// Visit makes a request as the visitor and returns the response with its body, which is read and closed.
func (s *Server) Visit(t *testing.T, visitor *http.Client, method, path string) (*http.Response, string) {
	t.Helper()

	return s.Do(t, visitor, s.NewRequest(t, method, path, nil))
}

// PostForm posts the form values to the path as the visitor, as a browser does, and returns the response with
// its body. It sends no Origin and no Sec-Fetch-Site, which is how a client that is not a browser posts: the
// server's cross-origin protection lets it through.
func (s *Server) PostForm(t *testing.T, visitor *http.Client, path string, values url.Values) (*http.Response, string) {
	t.Helper()

	req := s.NewRequest(t, http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return s.Do(t, visitor, req)
}

// LogIn submits the login form as the visitor, the way a person does, and returns the response with its body.
func (s *Server) LogIn(t *testing.T, visitor *http.Client, email, password string) (*http.Response, string) {
	t.Helper()

	return s.PostForm(t, visitor, paths.Login, url.Values{"email": {email}, "password": {password}})
}

// LogInAs is a visitor who is logged in as a new account, an admin when admin is set. The login is the real
// one, through the login form.
func (s *Server) LogInAs(t *testing.T, admin bool) (*http.Client, *db.User) {
	t.Helper()

	account := s.NewAccount(t, admin)
	visitor := NewVisitor(t)
	resp, _ := s.LogIn(t, visitor, account.Email, AccountPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "the login of the test account")

	return visitor, account
}

// Get fetches a path as an anonymous visitor, requires the status, and returns the body.
func (s *Server) Get(t *testing.T, path string, status int) string {
	t.Helper()

	resp, body := s.Visit(t, NewVisitor(t), http.MethodGet, path)
	require.Equal(t, status, resp.StatusCode, path)

	return body
}

// BodyOf fetches a path as an anonymous visitor, and returns the status and the body.
func (s *Server) BodyOf(t *testing.T, path string) (int, string) {
	t.Helper()

	resp, body := s.Visit(t, NewVisitor(t), http.MethodGet, path)

	return resp.StatusCode, body
}

// Fragment parses HTML, a whole page or a piece of one.
func Fragment(t *testing.T, html string) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	return doc
}

// Between is the text of src from the first occurrence of from to the next one of to.
func Between(t *testing.T, src, from, to string) string {
	t.Helper()

	start := strings.Index(src, from)
	require.NotEqual(t, -1, start, "%q is in the source", from)
	end := strings.Index(src[start:], to)
	require.NotEqual(t, -1, end, "%q follows %q", to, from)

	return src[start : start+end]
}

// Code is the JavaScript of src without its comment lines, so that a rule about what a script does is not
// broken by a comment that says what it does not.
func Code(src string) string {
	var lines []string
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}
