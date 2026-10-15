package form_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/httperr"
)

// profile is a form with every kind of field that Decode reads, and a Valid method.
type profile struct {
	Name    string   `form:"name"`
	Email   string   `form:"email"`
	Admin   bool     `form:"admin"`
	Age     int      `form:"age"`
	Level   uint8    `form:"level"`
	Ratio   float64  `form:"ratio"`
	Tags    []string `form:"tags"`
	Skipped string
	hidden  string `form:"hidden"`
	form.Submission
}

func (p *profile) Valid(context.Context) map[string]string {
	problems := map[string]string{}
	if p.Name == "" {
		problems["Name"] = form.MsgRequired
	}
	if !form.IsEmail(p.Email) {
		problems["Email"] = form.MsgEmail
	}

	return problems
}

// plain is a form that checks nothing.
type plain struct {
	Q string `form:"q"`
}

func urlencoded(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return r
}

func multipartBody(t *testing.T, fields map[string]string, files map[string][]byte) (*http.Request, string) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range fields {
		require.NoError(t, w.WriteField(name, value))
	}
	for name, content := range files {
		part, err := w.CreateFormFile(name, name+".bin")
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())

	return r, w.FormDataContentType()
}

func TestDecode__URLEncoded(t *testing.T) {
	t.Run("fills every kind of field from the input that is named by its tag", func(t *testing.T) {
		r := urlencoded(url.Values{
			"name": {"Ada"}, "email": {"ada@example.com"}, "admin": {"on"}, "age": {"36"}, "level": {"7"},
			"ratio": {"0.5"}, "tags": {"a", "b"}, "Skipped": {"no tag, no field"}, "hidden": {"unexported"},
		})
		var p profile

		require.NoError(t, form.Decode(r, &p))

		assert.Equal(t, "Ada", p.Name)
		assert.Equal(t, "ada@example.com", p.Email)
		assert.True(t, p.Admin)
		assert.Equal(t, 36, p.Age)
		assert.Equal(t, uint8(7), p.Level)
		assert.InDelta(t, 0.5, p.Ratio, 0.0001)
		assert.Equal(t, []string{"a", "b"}, p.Tags)
		assert.Empty(t, p.Skipped)
		assert.Empty(t, p.hidden)
		assert.True(t, p.IsSubmitted())
		assert.True(t, p.IsValid())
	})

	t.Run("an input that is not there leaves the field as it was", func(t *testing.T) {
		p := profile{Name: "kept", Age: 5}

		require.NoError(t, form.Decode(urlencoded(url.Values{"email": {"a@b.co"}}), &p))

		assert.Equal(t, "kept", p.Name)
		assert.Equal(t, 5, p.Age)
	})

	t.Run("an input that is empty is a number or boolean left unset", func(t *testing.T) {
		var p profile

		require.NoError(t, form.Decode(urlencoded(url.Values{"age": {""}, "admin": {""}, "ratio": {""}, "level": {""}}), &p))

		assert.Zero(t, p.Age)
		assert.False(t, p.Admin)
	})

	t.Run("booleans", func(t *testing.T) {
		for value, want := range map[string]bool{"true": true, "on": true, "1": true, "false": false, "off": false, "0": false, "": false} {
			var p profile
			require.NoError(t, form.Decode(urlencoded(url.Values{"admin": {value}}), &p), value)
			assert.Equal(t, want, p.Admin, value)
		}
	})

	t.Run("the query string is not the form", func(t *testing.T) {
		r := urlencoded(url.Values{"q": {"from the body"}})
		r.URL.RawQuery = "q=from+the+url&extra=1"
		var p plain

		require.NoError(t, form.Decode(r, &p))

		assert.Equal(t, "from the body", p.Q)

		r = httptest.NewRequest(http.MethodPost, "/?q=only+the+url", nil)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		p = plain{}
		require.NoError(t, form.Decode(r, &p))
		assert.Empty(t, p.Q, "a code that was meant to stay out of the URL is not taken from it")
	})

	t.Run("a request with no body and no type is an empty form", func(t *testing.T) {
		var p plain

		require.NoError(t, form.Decode(httptest.NewRequest(http.MethodPost, "/", nil), &p))
	})

	t.Run("a value that is not of the kind of its field is a 400", func(t *testing.T) {
		for _, values := range []url.Values{{"age": {"old"}}, {"level": {"300"}}, {"level": {"-1"}}, {"ratio": {"half"}}, {"admin": {"maybe"}}} {
			var p profile

			err := form.Decode(urlencoded(values), &p)

			var he *httperr.Error
			require.ErrorAs(t, err, &he, values)
			assert.Equal(t, http.StatusBadRequest, he.Status, values)
		}
	})

	t.Run("a body that is not a form is a 400", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=%zz"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		err := form.Decode(r, &plain{})

		var he *httperr.Error
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusBadRequest, he.Status)
	})

	t.Run("another content type is a 415", func(t *testing.T) {
		for _, contentType := range []string{"application/json", "text/plain", "garbage;;;"} {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"q":"x"}`))
			r.Header.Set("Content-Type", contentType)

			err := form.Decode(r, &plain{})

			var he *httperr.Error
			require.ErrorAs(t, err, &he, contentType)
			assert.Equal(t, http.StatusUnsupportedMediaType, he.Status, contentType)
		}
	})

	t.Run("a body over the limit is the error of the limit", func(t *testing.T) {
		r := urlencoded(url.Values{"q": {strings.Repeat("x", 1000)}})
		r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 100)

		err := form.Decode(r, &plain{})

		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
		assert.Equal(t, int64(100), tooLarge.Limit)
	})
}

func TestDecode__Validation(t *testing.T) {
	t.Run("a form that is not valid has its problems, by field name, and is submitted", func(t *testing.T) {
		var p profile

		require.NoError(t, form.Decode(urlencoded(url.Values{"email": {"not an address"}}), &p), "an invalid form is not an error")

		assert.True(t, p.IsSubmitted())
		assert.False(t, p.IsValid())
		assert.Equal(t, []string{form.MsgRequired}, p.GetFieldErrors("Name"))
		assert.Equal(t, []string{form.MsgEmail}, p.GetFieldErrors("Email"))
	})

	t.Run("a form that is valid has none", func(t *testing.T) {
		var p profile

		require.NoError(t, form.Decode(urlencoded(url.Values{"name": {"Ada"}, "email": {"ada@example.com"}}), &p))

		assert.True(t, p.IsValid())
	})

	t.Run("a form that checks itself has to embed the Submission that holds the problems", func(t *testing.T) {
		err := form.Decode(urlencoded(url.Values{}), &unhoused{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Submission")
	})

	t.Run("something that is not a pointer to a struct is refused", func(t *testing.T) {
		for _, dst := range []any{plain{}, "string", nil, new(int), (*plain)(nil)} {
			require.Error(t, form.Decode(urlencoded(url.Values{}), dst), "%T", dst)
		}
	})

	t.Run("a field of a kind that cannot be read is refused", func(t *testing.T) {
		type odd struct {
			M map[string]string `form:"m"`
		}

		require.Error(t, form.Decode(urlencoded(url.Values{"m": {"x"}}), &odd{}))
	})
}

// unhoused has a Valid method and nowhere to put what it finds.
type unhoused struct{}

func (unhoused) Valid(context.Context) map[string]string { return nil }

func TestDecode__Multipart(t *testing.T) {
	t.Run("fills the fields from the text parts, and leaves the files to the request", func(t *testing.T) {
		r, _ := multipartBody(t, map[string]string{"name": "Ada", "email": "ada@example.com", "tags": "a"}, map[string][]byte{"file": []byte("png bytes")})
		var p profile

		require.NoError(t, form.Decode(r, &p))

		assert.Equal(t, "Ada", p.Name)
		assert.Equal(t, []string{"a"}, p.Tags)
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		assert.Equal(t, "file.bin", header.Filename)
		require.NoError(t, file.Close())
	})

	t.Run("the query string is not read from a multipart form either", func(t *testing.T) {
		r, _ := multipartBody(t, map[string]string{}, nil)
		r.URL.RawQuery = "q=from+the+url"
		var p plain

		require.NoError(t, form.Decode(r, &p))

		assert.Empty(t, p.Q)
	})

	t.Run("a body over the limit is the error of the limit", func(t *testing.T) {
		r, _ := multipartBody(t, nil, map[string][]byte{"file": bytes.Repeat([]byte("x"), 10_000)})
		r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 1_000)

		err := form.Decode(r, &plain{})

		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
	})

	t.Run("a body that is not multipart is a 400", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("no boundary in here"))
		r.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")

		err := form.Decode(r, &plain{})

		var he *httperr.Error
		require.ErrorAs(t, err, &he)
		assert.Equal(t, http.StatusBadRequest, he.Status)
	})
}

func TestCleanup(t *testing.T) {
	// Parsing spills to a temp file once a part passes the 32 MB kept in memory, which lands wherever TMPDIR says:
	// point it somewhere this test owns.
	spill := func(t *testing.T) string {
		t.Helper()
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)

		return tmp
	}
	big := func(t *testing.T) *http.Request {
		t.Helper()
		r, _ := multipartBody(t, nil, map[string][]byte{"file": bytes.Repeat([]byte("x"), 33<<20)})

		// An upload: the route that takes it has raised the limit, as middleware.LargeBody does.
		return r.WithContext(form.WithMaxBytes(r.Context(), 64<<20))
	}

	t.Run("removes the files of a form that was read from a copy of the request", func(t *testing.T) {
		tmp := spill(t)
		var during int

		handler := form.Cleanup(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// A copy, as the session middleware hands on, parsed the way a handler does.
			type copyKey struct{}
			copied := r.WithContext(context.WithValue(r.Context(), copyKey{}, 1))
			require.NoError(t, form.Decode(copied, &plain{}))

			entries, err := os.ReadDir(tmp)
			require.NoError(t, err)
			during = len(entries)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), big(t))

		assert.Equal(t, 1, during, "the body should have been spilled to a temp file, or this test proves nothing")
		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries, "the temp file must be gone once the request is done")
	})

	t.Run("removes them when the handler refused the request too", func(t *testing.T) {
		tmp := spill(t)

		handler := form.Cleanup(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, form.Decode(r, &plain{}))
			http.Error(w, "refused", http.StatusForbidden)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), big(t))

		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("passes a request without a form through", func(t *testing.T) {
		called := false

		form.Cleanup(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true })).
			ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

		assert.True(t, called)
	})

	t.Run("a form read without it is left to net/http", func(t *testing.T) {
		// Not an error: Decode still reads the form, and the request it was given is the one that net/http cleans up.
		r, _ := multipartBody(t, map[string]string{"q": "x"}, nil)
		var p plain

		require.NoError(t, form.Decode(r, &p))
		assert.Equal(t, "x", p.Q)
		assert.NoError(t, r.MultipartForm.RemoveAll())
	})
}

func TestDecode__TheBodyIsBounded(t *testing.T) {
	t.Run("a url-encoded body of more than a megabyte is refused unless the route says more", func(t *testing.T) {
		r := urlencoded(url.Values{"q": {strings.Repeat("x", 2<<20)}})

		err := form.Decode(r, &plain{})

		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
		assert.Equal(t, int64(1<<20), tooLarge.Limit)
	})

	t.Run("so is a multipart one, which is not written to disk", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		r, _ := multipartBody(t, nil, map[string][]byte{"file": bytes.Repeat([]byte("x"), 2<<20)})

		err := form.Decode(r, &plain{})

		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
		assert.Equal(t, int64(1<<20), tooLarge.Limit)
		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("a route can raise it", func(t *testing.T) {
		r := urlencoded(url.Values{"q": {strings.Repeat("x", 2<<20)}})
		r = r.WithContext(form.WithMaxBytes(r.Context(), 4<<20))
		var p plain

		require.NoError(t, form.Decode(r, &p))

		assert.Len(t, p.Q, 2<<20)
	})
}
