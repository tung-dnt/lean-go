package server_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/server/servertest"
	"github.com/tung-dnt/lean-go/internal/ui"
	files "github.com/tung-dnt/lean-go/public"
)

// cookiesOf are the cookies a response sets.
func cookiesOf(resp *http.Response) []string { return resp.Header.Values("Set-Cookie") }

func TestHealthz(t *testing.T) {
	resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, paths.Healthz)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", body)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
}

// A miss on a route outside the pages still renders the error page, which reads the app config from the request
// context; it must get a real 404, not a dropped connection.
func TestStaticMissesAreNotFound(t *testing.T) {
	for _, path := range []string{"/static/nope.png", "/files/nope", "/static/", "/static/js", "/static/js/"} {
		resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, path)

		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
		assert.Contains(t, body, "to go return home", "%s: the error page, with the layout of the site", path)
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"), path)
		assert.Empty(t, resp.Header.Get("Cache-Control"), "%s: a file that is not there now may be after the next deploy", path)
	}
}

// A path that climbs out of the directory is cleaned by the router before it is looked at, which sends the
// visitor to where it would have gone, a page of the site and not a file of the repository.
func TestStaticFilesCannotBeClimbedOutOf(t *testing.T) {
	for _, path := range []string{"/static/../go.mod", "/files/../config/config.yaml", "/static/js/../../../go.mod"} {
		resp, _ := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, path)

		assert.GreaterOrEqual(t, resp.StatusCode, http.StatusMovedPermanently, path)
		assert.Less(t, resp.StatusCode, http.StatusBadRequest, path)
		location, err := resp.Location()
		require.NoError(t, err, path)

		_, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, location.RequestURI())
		assert.NotContains(t, body, "module github.com/tung-dnt/lean-go", "%s: go.mod was not served", path)
		assert.NotContains(t, body, "encryptionKey", "%s: config.yaml was not served", path)
	}
}

func TestStaticFiles(t *testing.T) {
	t.Run("a file is served with the cache of a file that never changes under its name", func(t *testing.T) {
		resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, ui.StaticFile("favicon.png"))

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))
		assert.Equal(t, "public, max-age=15768000", resp.Header.Get("Cache-Control"), "config cache.expiration.publicFile is 4380h")
		assert.NotEmpty(t, body)
	})

	t.Run("a HEAD of it works", func(t *testing.T) {
		resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodHead, "/static/favicon.png")

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))
		assert.Empty(t, body)
	})

	t.Run("an image is not compressed, a stylesheet and a script are", func(t *testing.T) {
		for _, name := range []string{"favicon.png", "main.css", "js/app.js"} {
			if _, err := fs.Stat(files.Static, "static/"+name); err != nil {
				continue // main.css and app.js are build output, and CI does not run `make js`
			}

			req := srv.NewRequest(t, http.MethodGet, "/static/"+name, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			resp, _ := srv.Do(t, servertest.NewVisitor(t), req)

			require.Equal(t, http.StatusOK, resp.StatusCode, name)
			if strings.HasSuffix(name, ".png") {
				assert.Empty(t, resp.Header.Get("Content-Encoding"), name)
			} else {
				assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"), name)
			}
		}
	})

	t.Run("a file is none of the pages: no session, no request id, no cookie", func(t *testing.T) {
		for _, path := range []string{"/static/favicon.png", "/static/nope", "/files/nope", "/healthz"} {
			resp, _ := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, path)

			assert.Empty(t, cookiesOf(resp), path)
			assert.Empty(t, resp.Header.Get("X-Request-Id"), path)
			assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"), "%s: the headers of the site are on everything", path)
		}
	})
}

func TestUnknownRoutesAreTheErrorPage(t *testing.T) {
	for _, path := range []string{"/there-is-no-such-page", "/notes/", "/a/b/c", "/login/x"} {
		resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, path)

		if strings.HasSuffix(path, "/") {
			assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode, path)

			continue
		}
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, path)
		doc := servertest.Fragment(t, body)
		assert.Equal(t, "Not Found", doc.Find("h1").First().Text(), path)
		assert.Contains(t, doc.Find("p").Text(), "to go return home", path)
		assert.Equal(t, paths.Home, doc.Find("p a").AttrOr("href", ""), path)
	}

	t.Run("whatever the method", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			resp, _ := srv.Visit(t, servertest.NewVisitor(t), method, "/there-is-no-such-page")

			assert.Equal(t, http.StatusNotFound, resp.StatusCode, method)
		}
	})
}

func TestPagesHaveTheHeadersOfTheSite(t *testing.T) {
	resp, _ := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, paths.Login)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "1; mode=block", resp.Header.Get("X-XSS-Protection"))
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "SAMEORIGIN", resp.Header.Get("X-Frame-Options"))
	assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Len(t, resp.Header.Get("X-Request-Id"), 26, "every page has its own id, which its lines in the log have too")
	assert.Empty(t, cookiesOf(resp), "a visitor who changes nothing is given no cookie")
}

func TestARequestIdThatCameWithTheRequestIsKept(t *testing.T) {
	req := srv.NewRequest(t, http.MethodGet, paths.Login, nil)
	req.Header.Set("X-Request-Id", "from-the-proxy-123")

	resp, _ := srv.Do(t, servertest.NewVisitor(t), req)

	assert.Equal(t, "from-the-proxy-123", resp.Header.Get("X-Request-Id"))
}

func TestTrailingSlashesAreRedirectedForGood(t *testing.T) {
	for target, want := range map[string]string{
		"/login/":           "/login",
		"/login/?x=1":       "/login?x=1",
		"/admin/notes/":     "/admin/notes",
		"/anything/at/all/": "/anything/at/all",
		"/notes/?a=1&b=2":   "/notes?a=1&b=2",
	} {
		resp, _ := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, target)

		assert.Equal(t, http.StatusMovedPermanently, resp.StatusCode, target)
		assert.Equal(t, want, resp.Header.Get("Location"), target)
	}
}

func TestPagesAreGzipped(t *testing.T) {
	_, plain := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, paths.Login)

	req := srv.NewRequest(t, http.MethodGet, paths.Login, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, raw := srv.Do(t, servertest.NewVisitor(t), req)

	require.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
	assert.Contains(t, resp.Header.Get("Vary"), "Accept-Encoding")
	zr, err := gzip.NewReader(bytes.NewReader([]byte(raw)))
	require.NoError(t, err)
	unzipped, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, plain, string(unzipped), "the same page, compressed")
}

// A post that a page of another site made on behalf of the visitor is refused, with the error page of a 403, before
// the handler looks at it, and one from the site itself, or from a client that is not a browser, is not.
func TestCrossOriginPostsAreRefused(t *testing.T) {
	account := srv.NewAccount(t, true)
	login := func(headers map[string]string) (*http.Response, string, *http.Client) {
		visitor := servertest.NewVisitor(t)
		req := srv.NewRequest(t, http.MethodPost, paths.Login, strings.NewReader(url.Values{
			"email": {account.Email}, "password": {servertest.AccountPassword},
		}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, body := srv.Do(t, visitor, req)

		return resp, body, visitor
	}

	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site"},
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
	} {
		resp, body, _ := login(headers)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%v", headers)
		assert.Contains(t, body, "You are not authorized", "%v: the error page", headers)
		assert.Empty(t, cookiesOf(resp), "%v: and nobody was logged in", headers)
	}

	for _, headers := range []map[string]string{
		nil, // a client that is not a browser sends neither header
		{"Sec-Fetch-Site": "same-origin", "Origin": srv.URL},
		{"Origin": srv.URL},
		{"Sec-Fetch-Site": "none"},
	} {
		resp, _, visitor := login(headers)

		assert.Equal(t, http.StatusSeeOther, resp.StatusCode, "%v", headers)
		assert.Equal(t, paths.Home, resp.Header.Get("Location"), "%v", headers)
		assert.NotEmpty(t, visitor.Jar.Cookies(&url.URL{Scheme: "http", Host: strings.TrimPrefix(srv.URL, "http://")}), "%v: logged in", headers)
	}
}

// Everything below /admin is for the admin, the paths nobody has written a route for and the methods nobody
// expects included: the check is in front of the router. Someone who is not logged in is sent to log in, with
// the same answer for a path that is there and a path that is not, so that nothing tells them which is which.
func TestAdminPathsAreHeldToTheAdminCheck(t *testing.T) {
	adminPaths := []string{
		"/admin", "/admin/notes/1/delete", "/admin/nothing/here", "/admin/../admin/notes/1/delete",
	}
	methods := []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}

	t.Run("a visitor who is not logged in is sent to log in", func(t *testing.T) {
		visitor := servertest.NewVisitor(t)

		for _, path := range adminPaths {
			for _, method := range methods {
				resp, body := srv.Visit(t, visitor, method, path)

				assert.Equal(t, http.StatusFound, resp.StatusCode, "%s %s", method, path)
				assert.Equal(t, "/login", resp.Header.Get("Location"), "%s %s", method, path)
				assert.Empty(t, body, "%s %s: nothing behind the check is served", method, path)
			}
		}
	})

	t.Run("a request of a boosted link is told to go to the login page, not redirected", func(t *testing.T) {
		req := srv.NewRequest(t, http.MethodGet, "/admin/nothing/here", nil)
		req.Header.Set("HX-Boosted", "true")

		resp, _ := srv.Do(t, servertest.NewVisitor(t), req)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "/login", resp.Header.Get("HX-Redirect"))
	})

	t.Run("someone who is logged in and is not the admin is refused", func(t *testing.T) {
		visitor, _ := srv.LogInAs(t, false)

		for _, path := range []string{"/admin/notes/1/delete", "/admin/nothing/here"} {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				resp, body := srv.Visit(t, visitor, method, path)

				assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", method, path)
				assert.Contains(t, body, "You are not authorized", "%s %s", method, path)
			}
		}
	})

	t.Run("the admin is let in, to what is there and not to what is not", func(t *testing.T) {
		visitor, _ := srv.LogInAs(t, true)

		n, err := srv.Queries.CreateNote(t.Context(), db.CreateNoteParams{Title: "let in", Body: ""})
		require.NoError(t, err)
		resp, _ := srv.Visit(t, visitor, http.MethodPost, paths.AdminNoteDelete(n.ID))
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode, "the route behind the check is reached")

		resp, _ = srv.Visit(t, visitor, http.MethodGet, "/admin/nothing/here")
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
