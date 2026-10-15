package form

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"

	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
)

// maxMemory is how much of a multipart body is kept in memory. net/http spills the rest, and the files of a part
// that is bigger, to temporary files on disk.
const maxMemory = 32 << 20

// defaultMaxBytes is how much of a body Decode reads, unless the context of the request says more: a form of text
// is a few hundred bytes, and a body that goes on for megabytes is somebody who is not filling in a form.
const defaultMaxBytes = 1 << 20

// maxBytesKey is the key of the most that Decode reads of a body, in a context.
type maxBytesKey struct{}

// WithMaxBytes returns a copy of ctx that has Decode read up to n bytes of the body of the request, in place of the
// 1 MiB that it reads of every other: it is for the route that takes an upload, which middleware.LargeBody sets
// up. A form that is meant to be bigger than that says so at the route, and every other form is held to the
// default, so that a post of gigabytes to the login form is cut off at the first megabyte and not written to disk.
func WithMaxBytes(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, maxBytesKey{}, n)
}

// maxBytes is the most that Decode reads of the body of a request with the context ctx.
func maxBytes(ctx context.Context) int64 {
	if n, ok := ctx.Value(maxBytesKey{}).(int64); ok {
		return n
	}

	return defaultMaxBytes
}

// state is what Decode needs of the Submission a form embeds.
type state interface {
	markSubmitted()
	SetFieldError(fieldName, message string)
}

// Decode reads the body of the request, a url-encoded form or a multipart one, into dst, a pointer to a struct,
// and marks the form as submitted. A field is filled from the input named by its `form` tag; a field with no tag
// is not touched. Strings, booleans (an input of "on" or "true"), integers, floating point numbers and slices of
// strings are read, and an input that is not there leaves its field as it was. The query string is never read: it
// is not the body of the form, and a value that was meant to stay out of the URL has to.
//
// A dst that has a Valid method (a Validator) is checked once it is filled, and the problems are recorded on the
// Submission that it embeds, for the form to be drawn with. A form with a Valid method has to embed one; Decode
// refuses a form that does not, since its problems would be lost.
//
// The error says why the request cannot be read, and is meant for ui.Error: an *httperr.Error of 400 for a body
// that is not a form, or of 415 for one of another type, and for a body over the limit of http.MaxBytesReader
// the *http.MaxBytesError itself, which the error page answers with a 413. The files of a multipart body are
// not read into dst: take them from r (r.FormFile) after Decode, and add Cleanup to the server's middleware so
// that the temporary files they may be in are removed when the request is done.
func Decode(r *http.Request, dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("form: Decode needs a pointer to a struct, not %T", dst)
	}

	submission, hasState := dst.(state)
	validator, validates := dst.(Validator)
	if validates && !hasState {
		return fmt.Errorf("form: %T has a Valid method and does not embed form.Submission, which holds its problems", dst)
	}

	values, err := body(r)
	if err != nil {
		return err
	}
	if err := fill(v.Elem(), values); err != nil {
		return err
	}

	if hasState {
		submission.markSubmitted()
	}
	if validates {
		for field, message := range validator.Valid(r.Context()) {
			submission.SetFieldError(field, message)
		}
	}

	return nil
}

// body is the values of the form that the body of r holds, which is none for a request that has no body.
func body(r *http.Request) (url.Values, error) {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" && r.ContentLength == 0 {
		return url.Values{}, nil
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, httperr.New(http.StatusUnsupportedMediaType, fmt.Sprintf("unreadable content type %q", contentType))
	}

	// There is no ResponseWriter to give the limit: the server finds a body that was not read to its end and closes
	// the connection, which is what the writer would have been asked for.
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes(r.Context()))

	switch mediaType {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return nil, readError(err)
		}

		return r.PostForm, nil

	case "multipart/form-data":
		// The body is bounded above, so it is read with the reader of the parts, which is all ParseMultipartForm
		// does, and the form is left on the request for r.FormFile to find the files in.
		reader, err := r.MultipartReader()
		if err != nil {
			return nil, readError(err)
		}
		multipartForm, err := reader.ReadForm(maxMemory)
		if err != nil {
			return nil, readError(err)
		}
		r.MultipartForm = multipartForm
		remove(r)

		return multipartForm.Value, nil
	}

	return nil, httperr.New(http.StatusUnsupportedMediaType, fmt.Sprintf("a form is not sent as %s", mediaType))
}

// readError is the error of a body that could not be read: the *http.MaxBytesError that cut it off, to be
// answered with a 413, or a 400, since it is what the client sent that is not right.
func readError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("form: %w", tooLarge)
	}

	return httperr.New(http.StatusBadRequest, fmt.Sprintf("the form cannot be read: %v", err))
}

// fill sets the fields of the struct s from values.
func fill(s reflect.Value, values url.Values) error {
	t := s.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		name, ok := field.Tag.Lookup("form")
		if !ok || !field.IsExported() {
			continue
		}

		got, present := values[name]
		if !present {
			continue
		}
		if err := set(s.Field(i), got); err != nil {
			return httperr.New(http.StatusBadRequest, fmt.Sprintf("the input %q: %v", name, err))
		}
	}

	return nil
}

// set puts the values of an input in a field of the kind that Decode reads. A value that is empty is a field
// that was left empty, and is not read as a number or a boolean.
func set(f reflect.Value, got []string) error {
	if f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String {
		f.Set(reflect.ValueOf(append([]string(nil), got...)).Convert(f.Type()))

		return nil
	}

	value := ""
	if len(got) > 0 {
		value = got[0]
	}

	switch f.Kind() {
	case reflect.String:
		f.SetString(value)

	case reflect.Bool:
		switch value {
		case "", "false", "off", "0":
			f.SetBool(false)
		case "true", "on", "1":
			f.SetBool(true)
		default:
			return fmt.Errorf("%q is not true or false", value)
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value == "" {
			return nil
		}
		n, err := strconv.ParseInt(value, 10, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not a whole number that fits", value)
		}
		f.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value == "" {
			return nil
		}
		n, err := strconv.ParseUint(value, 10, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not a positive whole number that fits", value)
		}
		f.SetUint(n)

	case reflect.Float32, reflect.Float64:
		if value == "" {
			return nil
		}
		n, err := strconv.ParseFloat(value, f.Type().Bits())
		if err != nil {
			return fmt.Errorf("%q is not a number", value)
		}
		f.SetFloat(n)

	default:
		return fmt.Errorf("form: a field of type %s cannot be read from a form", f.Type())
	}

	return nil
}

// cleanupKey is the key of the registry of multipart forms in a context.
type cleanupKey struct{}

// registry holds the multipart forms of one request, to have their temporary files removed when it is done.
type registry struct {
	mu    sync.Mutex
	forms []*multipart.Form
}

// remove has the files of the multipart form of r removed when the request is done, if Cleanup is there to do it.
func remove(r *http.Request) {
	reg, ok := r.Context().Value(cleanupKey{}).(*registry)
	if !ok {
		return
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.forms = append(reg.forms, r.MultipartForm)
}

// Cleanup removes, when a request is done, the temporary files that the multipart forms read by Decode were
// spilled to, and has to be on the server's middleware for that.
//
// net/http removes them itself, but only the ones of the request it handed to the server. A middleware that adds
// something to the context, as the session does, hands the rest of the chain a copy of the request, and a form
// read from the copy is invisible to the original: its files would stay in the temp directory for good. They
// are there once a body passes 32 MB, so each large upload, one that was refused included, would leak a file of
// its size. Install it outside every middleware that makes such a copy.
func Cleanup(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg := &registry{}
		defer reg.removeAll(r.Context())

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cleanupKey{}, reg)))
	})
}

// removeAll deletes the files of every form of the request.
func (reg *registry) removeAll(ctx context.Context) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	for _, f := range reg.forms {
		if err := f.RemoveAll(); err != nil {
			log.Ctx(ctx).Warn("failed to remove the temporary files of a multipart form", "error", err)
		}
	}
	reg.forms = nil
}
