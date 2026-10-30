package workflow_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflow "github.com/tung-dnt/lean-go/pkg/workflow"
)

func TestAdvance__HandsItsPayloadToDoAndRunPassesNil(t *testing.T) {
	var got []any
	record := func(_ context.Context, _ *item, payload any) error {
		got = append(got, payload)

		return nil
	}

	def := triggered()
	for i := range def.Transitions {
		def.Transitions[i].Do = record
	}

	o := newOrchestrator(t, def, newMemStore(item{ID: "a", Status: "parked"}, item{ID: "b", Status: "queued"}))

	res, err := o.Advance(t.Context(), "a", "working", struct{ note string }{"why"})
	require.NoError(t, err)
	assert.Equal(t, workflow.Advanced, res.Outcome)
	require.Equal(t, []any{struct{ note string }{"why"}}, got, "Advance hands its payload to Do")

	got = nil
	_, err = o.Run(t.Context(), "b")
	require.NoError(t, err)
	require.NotEmpty(t, got, "Run took at least one transition with a Do")
	for _, payload := range got {
		assert.Nil(t, payload, "Run has no payload to give")
	}
}
