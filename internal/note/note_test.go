package note_test

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/note"
	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/server/servertest"
)

// srv is the real server that these tests drive: server.Run, over a real PostgreSQL, started once for all of them.
var srv *servertest.Server

func TestMain(m *testing.M) {
	os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s }))
}

// unique is a title that no other note has: the tests share a database.
func unique(prefix string) string {
	return prefix + " " + strings.ToLower(rand.Text())
}

// post submits the form of a new note as an anonymous visitor.
func post(t *testing.T, title, body string) (*http.Response, string) {
	t.Helper()

	return srv.PostForm(t, servertest.NewVisitor(t), paths.Notes, url.Values{"title": {title}, "body": {body}})
}

// newNote makes a note the way the tests that are not about posting need one: straight in the database.
func newNote(t *testing.T, s *servertest.Server, title string) db.Note {
	t.Helper()

	n, err := s.Queries.CreateNote(t.Context(), db.CreateNoteParams{Title: title, Body: "the body of " + title})
	require.NoError(t, err)

	return n
}

func TestNotes__APostedNoteHasItsPageAndIsListed(t *testing.T) {
	title := unique("Groceries")

	resp, _ := post(t, "  "+title+"  ", "milk\neggs")

	// See Other, so that a reload of where it lands does not post the note again.
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	location := resp.Header.Get("Location")
	require.True(t, strings.HasPrefix(location, paths.Notes+"/"), location)

	doc := servertest.Fragment(t, srv.Get(t, location, http.StatusOK))
	assert.Equal(t, title, doc.Find("h1").First().Text(), "the title is kept without the spaces around it")
	assert.Equal(t, "milk\neggs", doc.Find("#note p.whitespace-pre-wrap").Text())

	list := servertest.Fragment(t, srv.Get(t, paths.Home, http.StatusOK))
	link := list.Find(fmt.Sprintf(`#notes a[href=%q]`, location))
	assert.Equal(t, title, link.Text(), "the list links to it")
}

func TestNotes__AFormFilledInWrongIsAnsweredWithItsProblemsAndMakesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		title, body, field, problem string
	}{
		"no title":         {"   ", "body", "title", "This field is required."},
		"a title too long": {strings.Repeat("é", note.MaxTitle+1), "body", "title", "Keep the title to 120 characters."},
		"a body too long":  {unique("Long"), strings.Repeat("é", note.MaxBody+1), "body", "Keep the note to 5000 characters."},
	} {
		before, err := srv.Queries.CountNotes(t.Context())
		require.NoError(t, err)

		resp, body := post(t, tc.title, tc.body)

		require.Equal(t, http.StatusOK, resp.StatusCode, name)
		doc := servertest.Fragment(t, body)
		assert.Contains(t, doc.Find("form#new-note").Text(), tc.problem, name)
		assert.Equal(t, tc.body, doc.Find(`form#new-note textarea[name="body"]`).Text(), "%s: what was typed is kept", name)

		after, err := srv.Queries.CountNotes(t.Context())
		require.NoError(t, err)
		assert.Equal(t, before, after, "%s: no note is made", name)
	}
}

// What a visitor types is text: it is escaped where it is shown, never markup that runs.
func TestNotes__ANoteIsShownAsTextNotMarkup(t *testing.T) {
	resp, _ := post(t, unique("<b>bold</b>"), `<script>alert("x")</script>`)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	body := srv.Get(t, resp.Header.Get("Location"), http.StatusOK)

	assert.NotContains(t, body, `<script>alert("x")</script>`)
	assert.Contains(t, body, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`)
	assert.NotContains(t, body, "<b>bold</b>")
}

func TestNotes__AnIdThatIsNoNotesIsNotFound(t *testing.T) {
	n := newNote(t, srv, unique("Gone"))
	_, err := srv.Queries.DeleteNote(t.Context(), n.ID)
	require.NoError(t, err)

	for _, path := range []string{paths.Note(n.ID), "/notes/0", "/notes/-1", "/notes/abc", "/notes/99999999999999999999"} {
		status, body := srv.BodyOf(t, path)

		assert.Equal(t, http.StatusNotFound, status, path)
		assert.Contains(t, body, "to go return home", "%s: the error page", path)
	}
}

func TestNotes__OnlyTheAdminDeletes(t *testing.T) {
	n := newNote(t, srv, unique("Keep"))
	deletePath := paths.AdminNoteDelete(n.ID)

	t.Run("a visitor is sent to log in, and the note stays", func(t *testing.T) {
		resp, _ := srv.Visit(t, servertest.NewVisitor(t), http.MethodPost, deletePath)

		assert.Equal(t, http.StatusFound, resp.StatusCode)
		assert.Equal(t, paths.Login, resp.Header.Get("Location"))
		srv.Get(t, paths.Note(n.ID), http.StatusOK)
	})

	t.Run("an account that is not the admin is refused, and sees no button", func(t *testing.T) {
		visitor, _ := srv.LogInAs(t, false)

		resp, _ := srv.Visit(t, visitor, http.MethodPost, deletePath)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		_, body := srv.Visit(t, visitor, http.MethodGet, paths.Note(n.ID))
		assert.Zero(t, servertest.Fragment(t, body).Find(fmt.Sprintf(`form[action=%q]`, deletePath)).Length())
	})

	t.Run("the admin sees the button, deletes, and is sent to the list", func(t *testing.T) {
		visitor, _ := srv.LogInAs(t, true)

		_, body := srv.Visit(t, visitor, http.MethodGet, paths.Note(n.ID))
		form := servertest.Fragment(t, body).Find(fmt.Sprintf(`form[action=%q]`, deletePath))
		require.Equal(t, 1, form.Length(), "the delete button")
		assert.Equal(t, "post", strings.ToLower(form.AttrOr("method", "")), "a get that changes something is fired by whatever fetches links")

		resp, _ := srv.Visit(t, visitor, http.MethodPost, deletePath)
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
		assert.Equal(t, paths.Home, resp.Header.Get("Location"))
		srv.Get(t, paths.Note(n.ID), http.StatusNotFound)

		resp, _ = srv.Visit(t, visitor, http.MethodPost, deletePath)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a note that is already gone")
	})
}

// The list is paged, newest first, and a page past the end is the last one. It needs a database of its own, since it
// counts every note there is.
func TestNotes__TheListIsPagedNewestFirst(t *testing.T) {
	own := servertest.Start(t)
	var ids []int64
	for i := range note.PerPage + 1 {
		ids = append(ids, newNote(t, own, fmt.Sprintf("Note %02d", i)).ID)
	}
	linksOf := func(page string) []string {
		doc := servertest.Fragment(t, own.Get(t, paths.Home+page, http.StatusOK))

		return doc.Find("#notes a").Map(func(_ int, s *goquery.Selection) string { return s.AttrOr("href", "") })
	}

	first := linksOf("")
	require.Len(t, first, note.PerPage)
	assert.Equal(t, paths.Note(ids[len(ids)-1]), first[0], "the newest is first")

	assert.Equal(t, []string{paths.Note(ids[0])}, linksOf("?page=2"), "the oldest is alone on the last page")
	assert.Equal(t, []string{paths.Note(ids[0])}, linksOf("?page=99"), "a page past the end is the last one")
}

// A query that fails is the error page, a 500 that says nothing of the failure, and the log says what it was.
func TestNotes__AFailingQueryIsTheErrorPage(t *testing.T) {
	own := servertest.Start(t)
	newNote(t, own, "there is a row for the query to read")
	own.FailQuery(t, "notes", "ListNotes")

	logged := len(own.Log())
	status, body := own.BodyOf(t, paths.Home)

	assert.Equal(t, http.StatusInternalServerError, status)
	assert.NotContains(t, body, "boom", "the failure is for the log, not the visitor")
	assert.Contains(t, own.Log()[logged:], "list notes")
	assert.Contains(t, own.Log()[logged:], "boom")
}
