// Package msg is the flash messages: what a handler tells the visitor on the next page they see, usually the one a
// redirect leads to, kept in the session until that page asks for them.
package msg

import (
	"context"
	"encoding/json"

	"github.com/tung-dnt/lean-go/pkg/log"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// Type is a message type.
type Type string

const (
	// TypeSuccess represents a success message type.
	TypeSuccess Type = "success"

	// TypeInfo represents a info message type.
	TypeInfo Type = "info"

	// TypeWarning represents a warning message type.
	TypeWarning Type = "warning"

	// TypeError represents an error message type.
	TypeError Type = "error"
)

// keyPrefix starts the session key of the messages of a type.
const keyPrefix = "flash:"

// Success sets a success flash message.
func Success(ctx context.Context, message string) {
	Set(ctx, TypeSuccess, message)
}

// Info sets an info flash message.
func Info(ctx context.Context, message string) {
	Set(ctx, TypeInfo, message)
}

// Warning sets a warning flash message.
func Warning(ctx context.Context, message string) {
	Set(ctx, TypeWarning, message)
}

// Error sets an error flash message.
func Error(ctx context.Context, message string) {
	Set(ctx, TypeError, message)
}

// Set adds a flash message of a given type to the session of the request that ctx belongs to, to be shown by
// the next page that asks for them (Get). A message that cannot be kept is logged and dropped, since there is
// nobody to tell: the request has no session when it did not go through the session middleware, and the
// session cannot hold what does not fit in a cookie.
func Set(ctx context.Context, typ Type, message string) {
	sess, ok := session.From(ctx)
	if !ok {
		log.Ctx(ctx).Error("flash message dropped: the request has no session", "type", typ)

		return
	}

	key := keyPrefix + string(typ)
	messages := decode(ctx, sess.Get(key))
	messages = append(messages, message)

	encoded, err := json.Marshal(messages)
	if err != nil {
		log.Ctx(ctx).Error("flash message dropped: cannot encode it", "type", typ, "error", err)

		return
	}
	sess.Set(key, string(encoded))
}

// Get takes the flash messages of a given type out of the session, in the order they were set, and returns them:
// a message is shown once. A request with no session has none.
func Get(ctx context.Context, typ Type) []string {
	sess, ok := session.From(ctx)
	if !ok {
		return nil
	}

	key := keyPrefix + string(typ)
	raw := sess.Get(key)
	if raw == "" {
		return nil
	}
	sess.Delete(key)

	return decode(ctx, raw)
}

// decode is the messages in raw, which is what Set kept: a JSON array of strings, or nothing.
func decode(ctx context.Context, raw string) []string {
	if raw == "" {
		return nil
	}

	var messages []string
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		log.Ctx(ctx).Warn("flash messages dropped: cannot decode them", "error", err)

		return nil
	}

	return messages
}
