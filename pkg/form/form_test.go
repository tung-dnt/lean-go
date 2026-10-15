package form

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSubmission(t *testing.T) {
	t.Run("a form that was not submitted is valid and has no errors", func(t *testing.T) {
		var s Submission

		assert.False(t, s.IsSubmitted())
		assert.True(t, s.IsValid())
		assert.False(t, s.FieldHasErrors("Name"))
		assert.Empty(t, s.GetFieldErrors("Name"))
		assert.NotNil(t, s.GetFieldErrors("Name"), "a slice, so that it ranges")
	})

	t.Run("an error is on its field, and makes the form invalid", func(t *testing.T) {
		var s Submission
		s.markSubmitted()

		s.SetFieldError("Email", "Enter a valid email address.")
		s.SetFieldError("Email", "And another thing.")

		assert.True(t, s.IsSubmitted())
		assert.False(t, s.IsValid())
		assert.True(t, s.FieldHasErrors("Email"))
		assert.False(t, s.FieldHasErrors("Password"))
		assert.Equal(t, []string{"Enter a valid email address.", "And another thing."}, s.GetFieldErrors("Email"))
	})

	t.Run("an error with no words still marks the field", func(t *testing.T) {
		var s Submission

		s.SetFieldError("Password", "")

		assert.True(t, s.FieldHasErrors("Password"))
		assert.False(t, s.IsValid())
	})
}

func TestIsEmail(t *testing.T) {
	for _, ok := range []string{"a@b.co", "someone@localhost.localhost", "first.last+tag@example.com"} {
		assert.True(t, IsEmail(ok), ok)
	}
	for _, bad := range []string{"", "nobody", "a@", "@b.co", "a b@c.co", "Bob <bob@example.com>", "<bob@example.com>", "a@b.co, c@d.co"} {
		assert.False(t, IsEmail(bad), bad)
	}
}
