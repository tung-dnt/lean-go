// Package log carries the logger of a request in its context, so that everything a request touches, the service
// beneath a handler included, logs with the request's id and nothing has to be handed a logger to do it.
package log

import (
	"context"
	"log/slog"
)

// ctxKey is the key of the logger in a context.
type ctxKey struct{}

// NewContext returns a copy of ctx that carries logger, for Ctx.
func NewContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// Ctx returns the logger of ctx, or the default logger when there is none: a context that did not come through
// the server, such as the one of a test or a command, logs as everything else does.
func Ctx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}

	return Default()
}

// Default returns the default logger.
func Default() *slog.Logger {
	return slog.Default()
}
