package authtest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/server/servertest"
)

func TestLogout__GETShowsTheFormAndKeepsTheSession(t *testing.T) {
	visitor, _ := srv.LogInAs(t, true)

	resp, body := srv.Visit(t, visitor, http.MethodGet, paths.Logout)

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	form := servertest.Fragment(t, body).Find("form#logout")
	require.Equal(t, 1, form.Length(), "the form that posts to log out")
	assert.Equal(t, paths.Logout, form.AttrOr("action", ""))
	assert.Equal(t, "post", strings.ToLower(form.AttrOr("method", "")))
	assert.Equal(t, 1, form.Find("button").Length(), "with one button to log out")
	for _, cookie := range resp.Cookies() {
		assert.False(t, cookie.Name == "session" && cookie.MaxAge < 0, "a GET does not clear the session")
	}
	resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Admin)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "past the admin check: the visitor is still logged in")
}

func TestLogout__POSTLogsOutAndSendsHome(t *testing.T) {
	visitor, _ := srv.LogInAs(t, true)

	resp, body := srv.Visit(t, visitor, http.MethodPost, paths.Logout)

	require.Equal(t, http.StatusSeeOther, resp.StatusCode, body)
	assert.Equal(t, paths.Home, resp.Header.Get("Location"))
	resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Admin)
	assert.Equal(t, http.StatusFound, resp.StatusCode, "logged out: sent to log in again")
	assert.Equal(t, paths.Login, resp.Header.Get("Location"))
}

func TestLogout__APostOfAnotherSiteIsRefusedAndKeepsTheSession(t *testing.T) {
	visitor, _ := srv.LogInAs(t, true)

	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "https://evil.example"},
	} {
		req := srv.NewRequest(t, http.MethodPost, paths.Logout, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, _ := srv.Do(t, visitor, req)

		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%v", headers)
		resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Admin)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "%v: still logged in", headers)
	}
}
