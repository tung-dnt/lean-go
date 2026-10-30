package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflow "github.com/tung-dnt/lean-go/pkg/workflow"
)

// What becomes of an entity whose handler fails, whose store refuses, or whose run is cut short.

var errBoom = errors.New("boom")

// flaky is a workflow whose one step counts an attempt on the entity, as a store does when it starts a run, and then
// fails with err. After the failure the retry policy decides.
func flaky(err error, retry workflow.RetryPolicy) definition {
	return definition{
		Name:   "flaky",
		Finals: []state{"done"},
		Failed: "failed",
		Transitions: []transition{
			initial("working"),
			{
				From: "working", To: "done", Retry: retry,
				Do: func(_ context.Context, e *item, _ any) error {
					e.Attempts++
					return err
				},
			},
			failsTo("working"),
		},
	}
}

// flakyStep is the index of the transition that fails in flaky.
const flakyStep = 1

func TestRun__AnUnretriableErrorFailsTheEntityAtOnceWhateverAttemptsAreLeft(t *testing.T) {
	cause := workflow.Unretriable(errBoom)
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, flaky(cause, workflow.RetryPolicy{MaxAttempts: 5, Backoff: time.Minute}), store, "a")

	// Failed, recorded, and the caller is still told why.
	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
	require.ErrorIs(t, err, errBoom)
	assert.True(t, workflow.IsUnretriable(err))
	assert.Equal(t, state("failed"), store.row("a").Status)
	assert.Equal(t, cause, store.row("a").Failure, "the entity is given the cause")
	assert.Equal(t, []string{"working"}, store.fails)
	assert.Empty(t, store.updates)
}

func TestRun__AnErrorOnTheLastAttemptFailsTheEntity(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working", Attempts: 2})

	res, err := run(t, flaky(errBoom, workflow.RetryPolicy{MaxAttempts: 3}), store, "a")

	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
	require.ErrorIs(t, err, errBoom)
	assert.False(t, workflow.IsUnretriable(err), "the cause is returned as it was")
	assert.Equal(t, state("failed"), store.row("a").Status)
}

func TestRun__AnErrorBeforeTheLastAttemptIsReturnedWithTheBackoffAndLeavesTheEntity(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working", Attempts: 1})

	res, err := run(t, flaky(errBoom, workflow.RetryPolicy{MaxAttempts: 3, Backoff: 7 * time.Second}), store, "a")

	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: 7 * time.Second}, res)
	assert.Equal(t, errBoom, err, "exactly the error of the handler")
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Empty(t, store.fails)
	assert.Empty(t, store.updates)
}

func TestRun__NoRetryPolicyMeansNoRetry(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, flaky(errBoom, workflow.RetryPolicy{}), store, "a")

	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, state("failed"), res.State)
	assert.Equal(t, state("failed"), store.row("a").Status)
}

func TestRun__TheAttemptsAreReadBackFromTheEntityAfterTheFailure(t *testing.T) {
	// The count is the store's, and the handler may refresh the entity with it: a run that finds peers' attempts on
	// the entity is that much closer to its limit.
	store := newMemStore(item{ID: "a", Status: "working"})
	def := flaky(errBoom, workflow.RetryPolicy{MaxAttempts: 3})
	def.Transitions[flakyStep].Do = func(_ context.Context, e *item, _ any) error {
		e.Attempts = 3
		return errBoom
	}

	_, err := run(t, def, store, "a")

	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, state("failed"), store.row("a").Status)
}

func TestRun__AFailureThatCannotBeRecordedIsReportedWithItsCause(t *testing.T) {
	failing := errors.New("cannot write")
	store := newMemStore(item{ID: "a", Status: "working"})
	store.failErr = failing

	res, err := run(t, flaky(workflow.Unretriable(errBoom), workflow.RetryPolicy{MaxAttempts: 3}), store, "a")

	require.ErrorIs(t, err, errBoom, "why it failed")
	require.ErrorIs(t, err, failing, "why that is not on record")
	assert.NotEqual(t, workflow.Final, res.Outcome)
	assert.Equal(t, state("working"), store.row("a").Status)
}

func TestRun__AFailureThatLosesToAPeerIsAConflict(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})
	def := flaky(workflow.Unretriable(errBoom), workflow.RetryPolicy{})
	// The peer settles the entity while the handler runs.
	do := def.Transitions[flakyStep].Do
	def.Transitions[flakyStep].Do = func(ctx context.Context, e *item, payload any) error {
		store.mu.Lock()
		r := store.rows["a"]
		r.Status = "done"
		store.rows["a"] = r
		store.mu.Unlock()

		return do(ctx, e, payload)
	}

	_, err := run(t, def, store, "a")

	require.ErrorIs(t, err, workflow.ErrConflict)
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, state("done"), store.row("a").Status, "the peer's outcome stands")
}

func TestRun__ATransitionThatLosesToAPeerIsAConflictAndDoesNotOverwrite(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "queued"})
	def := pipeline()
	// The peer takes the entity to the final state while this run is in the handler of the second step.
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		store.mu.Lock()
		r := store.rows["a"]
		r.Status, r.Flag = "done", "peer"
		store.rows["a"] = r
		store.mu.Unlock()

		return nil
	}

	_, err := run(t, def, store, "a")

	require.ErrorIs(t, err, workflow.ErrConflict)
	assert.Equal(t, "peer", store.row("a").Flag)
	assert.Equal(t, []string{"queued>working"}, store.updates, "only the step that was this run's")
}

func TestRun__ATransitionThatFailsIsDecidedByOnErrorLikeAHandlerThatFails(t *testing.T) {
	failing := errors.New("cannot write")

	t.Run("below the attempt limit it is returned wrapped for another try", func(t *testing.T) {
		store := newMemStore(item{ID: "a", Status: "working"})
		store.updateErr = failing

		res, err := run(t, flaky(nil, workflow.RetryPolicy{MaxAttempts: 2, Backoff: time.Second}), store, "a")

		require.ErrorIs(t, err, failing)
		assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: time.Second}, res)
		assert.Empty(t, store.fails)
	})

	t.Run("at the limit the entity is failed", func(t *testing.T) {
		store := newMemStore(item{ID: "a", Status: "working", Attempts: 9})
		store.updateErr = failing

		res, err := run(t, flaky(nil, workflow.RetryPolicy{MaxAttempts: 1}), store, "a")

		require.ErrorIs(t, err, failing)
		assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
		assert.Equal(t, []string{"working"}, store.fails)
	})
}

func TestRun__ACanceledContextStopsBeforeTheHandler(t *testing.T) {
	ran := false
	def := pipeline()
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		ran = true
		return nil
	}
	store := newMemStore(item{ID: "a", Status: "working"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	o := newOrchestrator(t, def, store)

	_, err := o.Run(ctx, "a")

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ran)
	assert.Empty(t, store.updates)
}

func TestRun__AContextThatEndsInTheHandlerStopsBeforeTheTransition(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	def := pipeline()
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		cancel()
		return nil
	}
	store := newMemStore(item{ID: "a", Status: "working"})
	o := newOrchestrator(t, def, store)

	res, err := o.Run(ctx, "a")

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, state("working"), res.State)
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Empty(t, store.fails, "being stopped is not a failure of the entity")
}

func TestUnretriable__KeepsItsCauseAndNilStaysNil(t *testing.T) {
	assert.NoError(t, workflow.Unretriable(nil))
	assert.False(t, workflow.IsUnretriable(nil))
	assert.False(t, workflow.IsUnretriable(errBoom))

	marked := workflow.Unretriable(errBoom)
	assert.ErrorIs(t, marked, errBoom)
	assert.EqualError(t, marked, "boom")
	assert.True(t, workflow.IsUnretriable(marked))

	// Wrapping the marked error does not unmark it.
	assert.True(t, workflow.IsUnretriable(errors.Join(errors.New("context"), marked)))
}
