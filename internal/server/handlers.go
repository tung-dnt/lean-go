package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
)

// HandleHealthz answers whether the server can serve: it is ready when the database answers. It is the readiness
// probe of the container platform, and the test server's way of knowing that it is up.
func HandleHealthz(pool *pgxpool.Pool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if err := pool.Ping(ctx); err != nil {
			log.Ctx(r.Context()).Error("not ready: the database does not answer", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := fmt.Fprint(w, "database unavailable"); err != nil {
				log.Ctx(r.Context()).Debug("cannot write the probe", "error", err)
			}

			return
		}

		if _, err := fmt.Fprint(w, "ok"); err != nil {
			log.Ctx(r.Context()).Debug("cannot write the probe", "error", err)
		}
	})
}

// HandleNotFound is the answer to a request that no other route takes: the error page of a 404. It is the
// catch-all of the router, which would otherwise answer in plain text.
func HandleNotFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		layouts.Error(w, r, httperr.New(http.StatusNotFound, "no such page: "+r.URL.Path))
	})
}

// HandleStatic serves the files of fsys at the {path...} of the route that it is mounted on. A file that is
// not there is a 404 with the error page, which a bare file server would answer in plain text, and so is a
// directory, whose listing nobody asked for.
//
// Mount it with the middleware that suits the files: the cache control that keeps them for months (they are
// asked for by a name that has a hash of the content in it, see ui.StaticFile) and the gzip of the files that
// compress.
func HandleStatic(fsys fs.FS) http.Handler {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		// Nothing to keep for months: a name that is not a file now may be one after the next deploy.
		w.Header().Del("Cache-Control")
		layouts.Error(w, r, httperr.New(http.StatusNotFound, "no such file: "+r.URL.Path))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("path")
		// A name that fs.FS would not take, with a .. or a leading slash in it, is no file of this one.
		if !fs.ValidPath(name) {
			notFound(w, r)

			return
		}

		f, err := fsys.Open(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				notFound(w, r)
			} else {
				layouts.Error(w, r, fmt.Errorf("open %q: %w", name, err))
			}

			return
		}
		defer func() {
			if err := f.Close(); err != nil {
				log.Ctx(r.Context()).Warn("cannot close a file", "name", name, "error", err)
			}
		}()

		info, err := f.Stat()
		if err != nil {
			layouts.Error(w, r, fmt.Errorf("stat %q: %w", name, err))

			return
		}
		content, ok := f.(io.ReadSeeker)
		if info.IsDir() || !ok {
			notFound(w, r)

			return
		}

		http.ServeContent(w, r, info.Name(), info.ModTime(), content)
	})
}
