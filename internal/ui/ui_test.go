package ui

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestPublicFile(t *testing.T) {
	path := "abc.txt"
	got := PublicFile(path)
	expected := fmt.Sprintf("/%s/%s", "files", path)
	assert.Equal(t, expected, got)
}

func TestStaticFile(t *testing.T) {
	t.Run("embedded file is versioned", func(t *testing.T) {
		got := StaticFile("favicon.png")
		assert.True(t, strings.HasPrefix(got, "/static/favicon.png?v="), got)
	})

	t.Run("unknown file has no version", func(t *testing.T) {
		assert.Equal(t, "/static/abc.txt", StaticFile("abc.txt"))
	})
}

func TestHashStatic(t *testing.T) {
	versions := hashStatic(fstest.MapFS{
		"static/a.css":     {Data: []byte("same")},
		"static/b.css":     {Data: []byte("same")},
		"static/js/app.js": {Data: []byte("different")},
		"other/c.css":      {Data: []byte("outside static")},
	})

	assert.Len(t, versions, 3)
	assert.Equal(t, versions["a.css"], versions["b.css"], "identical content must share a version")
	assert.NotEqual(t, versions["a.css"], versions["js/app.js"], "changed content must change the version")
}
