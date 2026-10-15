package auth_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tung-dnt/lean-go/internal/auth"
	"github.com/tung-dnt/lean-go/internal/server/servertest"
	"github.com/tung-dnt/lean-go/pkg/paths"
)

// srv is the real server that these tests drive.
var srv *servertest.Server

func TestMain(m *testing.M) {
	os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s }))
}

// sessionCookie is the cookie of the visitor that holds their session, or nil when they have none.
func sessionCookie(visitor *http.Client) *http.Cookie {
	u, err := url.Parse(srv.URL)
	if err != nil {
		return nil
	}
	for _, c := range visitor.Jar.Cookies(u) {
		if c.Name == "session" {
			return c
		}
	}

	return nil
}

// page is a page that every visitor can open, and that is drawn in the layout of the site, so that it carries
// the flash messages and the menu of whoever is looking. (The home page is another domain's.)
const page = "/there-is-no-such-page"

func TestLogin__PageOffersNothingButLoggingIn(t *testing.T) {
	resp, body := srv.Visit(t, servertest.NewVisitor(t), http.MethodGet, paths.Login)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	doc := servertest.Fragment(t, body)
	assert.Equal(t, 1, doc.Find(`form#login input[name="email"]`).Length())
	assert.Equal(t, 1, doc.Find(`form#login input[name="password"]`).Length())
	assert.Equal(t, paths.Login, doc.Find("form#login").AttrOr("action", ""))
	assert.Equal(t, "post", strings.ToLower(doc.Find("form#login").AttrOr("method", "")))

	// There is one account, made on the command line. Nothing here may suggest that anyone can have another.
	links := doc.Find("a").Map(func(_ int, a *goquery.Selection) string { return a.AttrOr("href", "") })
	for _, href := range links {
		assert.NotContains(t, strings.ToLower(href), "register")
		assert.NotContains(t, strings.ToLower(href), "password")
		assert.NotEmpty(t, href)
	}
	for _, word := range []string{"Register", "Forgot password", "Don't have an account"} {
		assert.NotContains(t, body, word)
	}

	// And no token for a form to send back: a post of another site is refused by the server instead.
	assert.Zero(t, doc.Find(`input[name="csrf"]`).Length())
	assert.Zero(t, doc.Find(`meta[name="csrf-token"]`).Length())
}

func TestLogin__AdminLogsInPassesTheAdminCheckAndLogsOut(t *testing.T) {
	admin := srv.NewAccount(t, true)
	visitor := servertest.NewVisitor(t)
	// Nothing is at /admin itself, so past the admin check it is the 404 page; before it, a redirect to the login.
	adminPage := paths.Admin

	// Not yet.
	resp, _ := srv.Visit(t, visitor, http.MethodGet, adminPage)
	require.Equal(t, http.StatusFound, resp.StatusCode)

	// A wrong password is turned away with the same words as an unknown address, and opens nothing.
	for name, email := range map[string]string{"wrong password": admin.Email, "unknown address": "nobody@localhost.localhost"} {
		resp, body := srv.LogIn(t, visitor, email, "not-"+servertest.AccountPassword)
		assert.Equal(t, http.StatusOK, resp.StatusCode, name)
		assert.Contains(t, body, "Invalid credentials", name)
		resp, _ = srv.Visit(t, visitor, http.MethodGet, adminPage)
		assert.Equal(t, http.StatusFound, resp.StatusCode, name)
	}

	// The right one lands on a page, not on a repeat of the post: a form sent without htmx must not send the
	// password again to wherever it is redirected.
	resp, _ = srv.LogIn(t, visitor, admin.Email, servertest.AccountPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, paths.Home, resp.Header.Get("Location"))

	resp, _ = srv.Visit(t, visitor, http.MethodGet, adminPage)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "past the admin check")

	// And back out.
	resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Logout)
	assert.Less(t, resp.StatusCode, http.StatusBadRequest)
	resp, _ = srv.Visit(t, visitor, http.MethodGet, adminPage)
	assert.Equal(t, http.StatusFound, resp.StatusCode, "logged out: sent to log in again")
	assert.Equal(t, paths.Login, resp.Header.Get("Location"))
}

func TestLogin__AnEmailIsFoundWhateverItsCase(t *testing.T) {
	account := srv.NewAccount(t, false)

	resp, _ := srv.LogIn(t, servertest.NewVisitor(t), strings.ToUpper(account.Email), servertest.AccountPassword)

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

func TestLogin__ABadFormIsDrawnAgainWithWhatIsWrongWithIt(t *testing.T) {
	cases := map[string]struct {
		email, password string
		emailError      string
		passwordError   string
	}{
		"nothing":               {"", "", "This field is required.", "This field is required."},
		"no password":           {"someone@localhost.localhost", "", "", "This field is required."},
		"no email":              {"", "secret", "This field is required.", ""},
		"not an email":          {"not-an-email", "secret", "Enter a valid email address.", ""},
		"a name and an address": {"Bob <bob@example.com>", "secret", "Enter a valid email address.", ""},
	}
	for name, tc := range cases {
		visitor := servertest.NewVisitor(t)

		resp, body := srv.LogIn(t, visitor, tc.email, tc.password)

		require.Equal(t, http.StatusOK, resp.StatusCode, name)
		doc := servertest.Fragment(t, body)
		assert.Equal(t, 1, doc.Find("form#login").Length(), name)
		assert.Equal(t, tc.email, doc.Find(`input[name="email"]`).AttrOr("value", ""), "%s: what was typed comes back", name)
		assert.Empty(t, doc.Find(`input[name="password"]`).AttrOr("value", ""), "%s: and the password never does", name)
		assert.Contains(t, doc.Find("form#login").Text(), tc.emailError, name)
		assert.Contains(t, doc.Find("form#login").Text(), tc.passwordError, name)
		assert.NotContains(t, body, "Invalid credentials", "%s: nobody was asked for credentials yet", name)
		assert.Nil(t, sessionCookie(visitor), name)
	}
}

func TestLogin__AWrongPasswordMarksBothFieldsAndSaysNothingAboutEither(t *testing.T) {
	account := srv.NewAccount(t, false)

	resp, body := srv.LogIn(t, servertest.NewVisitor(t), account.Email, "not-"+servertest.AccountPassword)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	doc := servertest.Fragment(t, body)
	assert.Contains(t, doc.Find(`[role="alert"]`).Text(), "Invalid credentials. Please try again.")
	assert.True(t, doc.Find(`input[name="email"]`).HasClass("input-error"))
	assert.True(t, doc.Find(`input[name="password"]`).HasClass("input-error"))
	assert.Equal(t, account.Email, doc.Find(`input[name="email"]`).AttrOr("value", ""))
	assert.Empty(t, doc.Find(`input[name="password"]`).AttrOr("value", ""))
}

func TestLogin__AFailingLookupIsA500AndTheLogSaysWhichStepFailed(t *testing.T) {
	broken := servertest.Start(t)
	account := broken.NewAccount(t, false)
	broken.FailQuery(t, "users", "GetUserByEmail")
	logged := len(broken.Log())
	visitor := servertest.NewVisitor(t)

	resp, body := broken.LogIn(t, visitor, account.Email, servertest.AccountPassword)

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(t, body, "Please try again.", "the error page, in the layout of the site")
	assert.NotContains(t, body, "Invalid credentials", "a database that failed is not a wrong password")
	assert.Contains(t, broken.Log()[logged:], "error querying user during login: ", "the log says which step failed")
	assert.Contains(t, broken.Log()[logged:], "boom", "and what the database said")
	assert.Nil(t, sessionCookie(visitor), "nobody is logged in")

	broken.Heal(t)
	resp, _ = broken.LogIn(t, visitor, account.Email, servertest.AccountPassword)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode, "the same login works once the database does")
}

func TestLogin__AFlashMessageComesOnceOnTheNextPage(t *testing.T) {
	account := srv.NewAccount(t, false)
	visitor := servertest.NewVisitor(t)

	resp, _ := srv.LogIn(t, visitor, account.Email, servertest.AccountPassword)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	_, body := srv.Visit(t, visitor, http.MethodGet, page)
	assert.Contains(t, servertest.Fragment(t, body).Find(`[role="alert"]`).Text(), "Welcome back, "+account.Name+". You are now logged in.")

	_, body = srv.Visit(t, visitor, http.MethodGet, page)
	assert.Zero(t, servertest.Fragment(t, body).Find(`[role="alert"]`).Length(), "it was shown once")

	resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Logout)
	require.Less(t, resp.StatusCode, http.StatusBadRequest)
	_, body = srv.Visit(t, visitor, http.MethodGet, page)
	assert.Contains(t, servertest.Fragment(t, body).Find(`[role="alert"]`).Text(), "You have been logged out successfully.")
}

func TestLogin__ABoostedFormIsToldWhereToGoInsteadOfBeingRedirected(t *testing.T) {
	account := srv.NewAccount(t, false)
	req := srv.NewRequest(t, http.MethodPost, paths.Login, strings.NewReader(url.Values{
		"email": {account.Email}, "password": {servertest.AccountPassword},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Boosted", "true")

	resp, _ := srv.Do(t, servertest.NewVisitor(t), req)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, paths.Home, resp.Header.Get("HX-Redirect"))
	assert.Empty(t, resp.Header.Get("Location"))
}

func TestLogin__TheSessionIsACookieThatTheBrowserKeepsToItself(t *testing.T) {
	visitor, _ := srv.LogInAs(t, false)

	c := sessionCookie(visitor)
	require.NotNil(t, c, "logged in: there is a cookie")

	// What the response said of it, which the jar does not keep.
	account := srv.NewAccount(t, false)
	resp, _ := srv.LogIn(t, servertest.NewVisitor(t), account.Email, servertest.AccountPassword)
	var set *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session" {
			set = cookie
		}
	}
	require.NotNil(t, set)
	assert.True(t, set.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, set.SameSite)
	assert.Equal(t, int(srv.Config.App.SessionMaxAge/time.Second), set.MaxAge, "config app.sessionMaxAge")
	assert.False(t, set.Secure, "the test server speaks plain HTTP")
	assert.NotContains(t, set.Value, account.Email, "what is in it is sealed")
	assert.NotContains(t, set.Value, ":", "and is not a plain value with a name in front")
}

func TestSession__ACookieThatWasChangedIsNoSession(t *testing.T) {
	visitor, _ := srv.LogInAs(t, true)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	good := sessionCookie(visitor)
	require.NotNil(t, good)

	// Nothing is at /admin itself: past the admin check it is the 404 page, and before it a redirect to the login.
	resp, _ := srv.Visit(t, visitor, http.MethodGet, paths.Admin)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "the cookie as it was set")

	for name, value := range map[string]string{
		"a character changed": good.Value[:10] + "x" + good.Value[11:],
		"cut short":           good.Value[:len(good.Value)-4],
		"made up":             "bm90IGEgc2VhbGVkIHZhbHVl",
		"empty":               "x",
	} {
		other := servertest.NewVisitor(t)
		req := srv.NewRequest(t, http.MethodGet, paths.Admin, nil)
		req.Header.Set("Cookie", "session="+value)
		require.Empty(t, other.Jar.Cookies(u), "the cookie is sent by hand, and the visitor has none of their own")

		resp, _ := srv.Do(t, other, req)

		assert.Equal(t, http.StatusFound, resp.StatusCode, "%s: not logged in, and not an error page", name)
		assert.Equal(t, paths.Login, resp.Header.Get("Location"), name)
		var dropped bool
		for _, c := range resp.Cookies() {
			dropped = dropped || (c.Name == "session" && c.MaxAge < 0)
		}
		assert.True(t, dropped, "%s: the browser is told to stop sending it", name)
	}
}

func TestSession__ALoggedInVisitorIsKnownToEveryPage(t *testing.T) {
	visitor, _ := srv.LogInAs(t, true)
	other := servertest.NewVisitor(t)

	_, admin := srv.Visit(t, visitor, http.MethodGet, page)
	_, anonymous := srv.Visit(t, other, http.MethodGet, page)

	assert.Positive(t, servertest.Fragment(t, admin).Find(`a[href="/logout"]`).Length(), "the admin's menu has the way out")
	assert.Zero(t, servertest.Fragment(t, admin).Find(`a[href="/login"]`).Length(), "and not the way in")
	assert.Zero(t, servertest.Fragment(t, anonymous).Find(`a[href="/logout"]`).Length(), "nobody else's has the way out")
	assert.Positive(t, servertest.Fragment(t, anonymous).Find(`a[href="/login"]`).Length(), "but the way in")

	member, _ := srv.LogInAs(t, false)
	_, body := srv.Visit(t, member, http.MethodGet, page)
	assert.Positive(t, servertest.Fragment(t, body).Find(`a[href="/logout"]`).Length(), "someone who is logged in and is no admin has the way out too")
}

func TestSession__AUserWhoIsGoneIsNotLoggedIn(t *testing.T) {
	visitor, account := srv.LogInAs(t, true)
	resp, _ := srv.Visit(t, visitor, http.MethodGet, paths.Admin)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "past the admin check")

	_, err := srv.DB.Exec(t.Context(), "DELETE FROM users WHERE id = $1", account.ID)
	require.NoError(t, err)

	resp, _ = srv.Visit(t, visitor, http.MethodGet, paths.Admin)
	assert.Equal(t, http.StatusFound, resp.StatusCode, "the session names an account that is not there: a visitor like any other")
	assert.Equal(t, paths.Login, resp.Header.Get("Location"))
}

func TestGuards__LogoutIsForTheLoggedInAndLoginForTheOthers(t *testing.T) {
	anonymous := servertest.NewVisitor(t)
	resp, _ := srv.Visit(t, anonymous, http.MethodGet, paths.Logout)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "nobody to log out")

	member, _ := srv.LogInAs(t, false)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		resp, body := srv.Visit(t, member, method, paths.Login)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s: somebody who is logged in has nothing to log in to", method)
		assert.Contains(t, body, "You are not authorized to view the requested page.", method)
	}

	resp, _ = srv.Visit(t, member, http.MethodGet, paths.Logout)
	assert.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	assert.Equal(t, paths.Home, resp.Header.Get("Location"))
}

func TestAuthRoutes__ThereIsNoWayToRegisterOrResetAPassword(t *testing.T) {
	visitor := servertest.NewVisitor(t)

	routes := []string{
		"/register", "/forgot-password", "/reset-password",
		"/user/register", "/user/password", "/user/login",
		"/user/password/reset/token/1/1/token", "/email/verify/token",
	}
	for _, p := range routes {
		resp, _ := srv.Visit(t, visitor, http.MethodGet, p)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "GET "+p)

		resp, _ = srv.PostForm(t, visitor, p, url.Values{"email": {"someone@localhost.localhost"}, "password": {"a-new-password"}})
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "POST "+p)
	}

	// Nothing an unregistered route could have left behind: no account came out of any of it.
	_, err := srv.Queries.GetUserByEmail(t.Context(), "someone@localhost.localhost")
	assert.Error(t, err)
}

func TestHashPassword(t *testing.T) {
	hash, err := auth.HashPassword("testcheckpassword")
	require.NoError(t, err)

	assert.NotEqual(t, "testcheckpassword", hash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("testcheckpassword")))
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("another")))

	again, err := auth.HashPassword("testcheckpassword")
	require.NoError(t, err)
	assert.NotEqual(t, hash, again, "every hash has a salt of its own")
}

func TestRandomToken(t *testing.T) {
	const length = 64
	a, err := auth.RandomToken(length)
	require.NoError(t, err)
	b, err := auth.RandomToken(length)
	require.NoError(t, err)

	assert.Len(t, a, length)
	assert.Len(t, b, length)
	assert.NotEqual(t, a, b)

	for _, n := range []int{1, 10, 11} {
		token, err := auth.RandomToken(n)
		require.NoError(t, err)
		assert.Len(t, token, n)
	}
}
