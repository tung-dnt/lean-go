package components

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/pkg/config"
)

// head renders the metatags of a request for an app configuration, as the document of the head.
func head(t *testing.T, app config.AppConfig, mutate func(*ui.Request)) *goquery.Document {
	t.Helper()

	r := &ui.Request{Config: &config.Config{App: app}}
	if mutate != nil {
		mutate(r)
	}

	var b strings.Builder
	require.NoError(t, html.Head(Metatags(r)).Render(&b))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	require.NoError(t, err)

	return doc
}

// The site has no CSRF tokens: a post of another site is refused by the server (middleware.CrossOrigin), so no
// page carries a token for a script to send back.
func TestMetatags__ThereIsNoCSRFToken(t *testing.T) {
	doc := head(t, config.AppConfig{Environment: config.EnvProduction}, nil)

	assert.Zero(t, doc.Find(`meta[name="csrf-token"]`).Length())
}

func TestMetatags__TheTitleIsTheSiteAndThePage(t *testing.T) {
	app := config.AppConfig{Name: "leango"}

	assert.Equal(t, "leango", head(t, app, nil).Find("title").Text())
	assert.Equal(t, "leango | Login", head(t, app, func(r *ui.Request) { r.Title = "Login" }).Find("title").Text())
}

func TestMetatags__DescriptionAndKeywordsAreThereWhenTheyAreGiven(t *testing.T) {
	app := config.AppConfig{}

	assert.Zero(t, head(t, app, nil).Find(`meta[name="description"], meta[name="keywords"]`).Length())

	doc := head(t, app, func(r *ui.Request) {
		r.Metatags.Description = "Notes"
		r.Metatags.Keywords = []string{"a", "b"}
	})
	assert.Equal(t, "Notes", doc.Find(`meta[name="description"]`).AttrOr("content", ""))
	assert.Equal(t, "a, b", doc.Find(`meta[name="keywords"]`).AttrOr("content", ""))
}
