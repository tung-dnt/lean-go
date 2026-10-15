package log

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCtx(t *testing.T) {
	t.Run("is the default logger when the context has none", func(t *testing.T) {
		assert.Same(t, Default(), Ctx(t.Context()))
	})

	t.Run("is the logger the context carries", func(t *testing.T) {
		var out bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&out, nil)).With("request_id", "abc")

		Ctx(NewContext(t.Context(), logger)).Info("hello")

		assert.Contains(t, out.String(), "request_id=abc")
		assert.Contains(t, out.String(), "msg=hello")
	})
}
