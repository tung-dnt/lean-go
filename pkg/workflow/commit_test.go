package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflow "github.com/tung-dnt/lean-go/pkg/workflow"
)

// What the orchestrator hands to the store for a transition: its Commit, to run in the store's transaction, its claim, its
// first transition, and the edges that a failure may take.

var errCommit = errors.New("the commit failed")

// partial is a Commit that makes a write and then fails, as a step does whose second statement is refused.
func partial(_ context.Context, t *tx, _ *item) error {
	t.write("first")

	return errCommit
}

func TestRun__TheCommitIsWrittenWithTheNewStatusAndNotBefore(t *testing.T) {
	def := pipeline()
	def.Transitions[pipelineStep].Commit = commits("work")
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res)
	assert.Equal(t, []string{"work"}, store.row("a").Commits)
	assert.Equal(t, state("done"), store.row("a").Status)
}

func TestRun__ACommitThatFailsLeavesTheStatusAndEveryWriteOfItUnchanged(t *testing.T) {
	def := pipeline()
	def.Transitions[pipelineStep].Commit = partial
	def.OnError = decides(workflow.Decision{Kind: workflow.Retry})
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, errCommit)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	assert.Equal(t, state("working"), store.row("a").Status, "the status is part of the transaction")
	assert.Empty(t, store.row("a").Commits, "the write that came before the failure is rolled back with it")
	assert.Empty(t, store.updates)
}

func TestRun__ACommitThatFailsIsDecidedByOnErrorLikeAHandlerThatFails(t *testing.T) {
	def := pipeline()
	def.Transitions[pipelineStep].Commit = partial
	def.Transitions[pipelineStep].Retry = workflow.RetryPolicy{MaxAttempts: 1}
	store := newMemStore(item{ID: "a", Status: "working", Attempts: 1})

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, errCommit)
	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
	assert.Empty(t, store.row("a").Commits)
}

func TestAdvance__ACommitThatFailsLeavesTheStatusUnchanged(t *testing.T) {
	def := triggered()
	def.Transitions[resumeStep].Commit = partial
	def.OnError = decides(workflow.Decision{Kind: workflow.Retry})
	store := newMemStore(item{ID: "a", Status: "parked"})

	res, err := advance(t, def, store, "a", "working")

	require.ErrorIs(t, err, errCommit)
	assert.Equal(t, workflow.Waiting, res.Outcome)
	assert.Equal(t, state("parked"), store.row("a").Status)
	assert.Empty(t, store.row("a").Commits)
}

func TestAdvance__ACommitRunsAfterTheDoWithThePayloadItKept(t *testing.T) {
	def := triggered()
	def.Transitions[resumeStep].Do = func(_ context.Context, e *item, payload any) error {
		e.Flag = payload.(string)

		return nil
	}
	var seen string
	def.Transitions[resumeStep].Commit = func(_ context.Context, t *tx, e *item) error {
		seen = e.Flag
		t.write("resume")

		return nil
	}
	store := newMemStore(item{ID: "a", Status: "parked"})

	res, err := newOrchestrator(t, def, store).Advance(t.Context(), "a", "working", "carried")

	require.NoError(t, err)
	assert.Equal(t, workflow.Advanced, res.Outcome)
	assert.Equal(t, "carried", seen, "the commit sees what the Do made of the payload")
	assert.Equal(t, []string{"resume"}, store.row("a").Commits)
}

func TestRun__ACommitDoesNotRunWhenThePeerWonTheRaceAndItsWritesAreNotMade(t *testing.T) {
	ran := false
	def := pipeline()
	def.OnError = decides(workflow.Decision{Kind: workflow.Yield})
	def.Transitions[pipelineStep].Commit = func(context.Context, *tx, *item) error {
		ran = true

		return nil
	}
	store := newMemStore(item{ID: "a", Status: "working"})
	// The peer settles the entity after the Load, while the Do of the transition runs.
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		store.mu.Lock()
		r := store.rows["a"]
		r.Status = "done"
		store.rows["a"] = r
		store.mu.Unlock()

		return nil
	}

	_, err := run(t, def, store, "a")

	require.ErrorIs(t, err, workflow.ErrConflict)
	assert.False(t, ran, "the store checks the status before it runs the commit")
	assert.Empty(t, store.row("a").Commits)
	assert.Equal(t, state("done"), store.row("a").Status)
}

func TestRun__ATransitionThatCountsAttemptsClaimsBeforeItsDo(t *testing.T) {
	def := pipeline()
	def.Transitions[pipelineStep].CountsAttempt = true
	store := newMemStore(item{ID: "a", Status: "working"})
	def.Transitions[pipelineStep].Do = func(_ context.Context, e *item, _ any) error {
		store.note("do")
		assert.Equal(t, 1, e.Attempts, "the Do is given the entity as the claim stored it")

		return nil
	}
	def.Transitions[pipelineStep].Commit = func(context.Context, *tx, *item) error {
		// The store holds its lock while it runs a commit, as a database holds a row.
		store.events = append(store.events, "commit")

		return nil
	}

	_, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, []string{"claim", "do", "commit"}, store.events)
	assert.Equal(t, 1, store.row("a").Attempts)
}

func TestRun__ATransitionThatDoesNotCountAttemptsClaimsNothing(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "queued"})

	_, err := run(t, pipeline(), store, "a")

	require.NoError(t, err)
	assert.NotContains(t, store.events, "claim")
	assert.Zero(t, store.row("a").Attempts)
}

func TestRun__AClaimThatLosesToAPeerIsAConflictDecidedByOnErrorAndNothingRuns(t *testing.T) {
	ran := false
	var asked error
	def := pipeline()
	def.Transitions[pipelineStep].CountsAttempt = true
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		ran = true

		return nil
	}
	def.OnError = func(_ context.Context, _ *item, _ transition, _ int, err error) workflow.Decision {
		asked = err

		return workflow.Decision{Kind: workflow.Drop}
	}
	store := newMemStore(item{ID: "a", Status: "working"})
	store.claimErr = workflow.ErrConflict

	res, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.NoTransition, res.Outcome)
	require.ErrorIs(t, asked, workflow.ErrConflict)
	assert.False(t, ran, "the Do of a claim that lost does not run")
	assert.Empty(t, store.updates)
}

func TestStart__InsertsTheEntityOnceAndRunsTheInitialCommitInTheSameTransaction(t *testing.T) {
	def := pipeline()
	def.Transitions[0].Commit = commits("insert")
	store := newMemStore()
	o := newOrchestrator(t, def, store)

	res, err := o.Start(t.Context(), "a", &item{ID: "a", Flag: "first"})

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "queued", Outcome: workflow.Advanced}, res)
	assert.Equal(t, item{ID: "a", Status: "queued", Flag: "first", Commits: []string{"insert"}}, store.row("a"))
}

func TestStart__ASecondStartOfTheSameKeyReturnsExistingAndRunsNoCommit(t *testing.T) {
	commitRuns := 0
	def := pipeline()
	def.Transitions[0].Commit = func(_ context.Context, t *tx, _ *item) error {
		commitRuns++
		t.write("insert")

		return nil
	}
	store := newMemStore()
	o := newOrchestrator(t, def, store)

	_, err := o.Start(t.Context(), "a", &item{ID: "a", Flag: "first"})
	require.NoError(t, err)
	res, err := o.Start(t.Context(), "a", &item{ID: "a", Flag: "second"})

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "queued", Outcome: workflow.Existing}, res)
	assert.Equal(t, 1, commitRuns, "the commit of a start that found the entity does not run")
	assert.Equal(t, "first", store.row("a").Flag, "the entity is as the first start left it")
	assert.Equal(t, []string{"insert"}, store.row("a").Commits)
}

func TestStart__ReportsTheStateAnExistingEntityHasMovedTo(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := newOrchestrator(t, pipeline(), store).Start(t.Context(), "a", &item{ID: "a"})

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Existing}, res)
}

func TestStart__DoesNotRunTheNextTransition(t *testing.T) {
	store := newMemStore()

	_, err := newOrchestrator(t, pipeline(), store).Start(t.Context(), "a", &item{ID: "a"})

	require.NoError(t, err)
	assert.Equal(t, state("queued"), store.row("a").Status, "going on from the initial state is for Run")
	assert.Empty(t, store.updates)
}

func TestStart__ACommitThatFailsStoresNothing(t *testing.T) {
	def := pipeline()
	def.Transitions[0].Commit = partial
	store := newMemStore()

	res, err := newOrchestrator(t, def, store).Start(t.Context(), "a", &item{ID: "a"})

	require.ErrorIs(t, err, errCommit)
	assert.Zero(t, res)
	assert.Empty(t, store.rows, "no entity and no write of the commit")
}

func TestStart__AnInsertThatFailsIsReportedWrapped(t *testing.T) {
	store := newMemStore()
	store.insertErr = errCommit

	_, err := newOrchestrator(t, pipeline(), store).Start(t.Context(), "a", &item{ID: "a"})

	require.ErrorIs(t, err, errCommit)
	assert.ErrorContains(t, err, `"a"`)
}

func TestStart__AnExistingEntityThatCannotBeLoadedIsReportedWrapped(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})
	store.loadErr = errCommit

	_, err := newOrchestrator(t, pipeline(), store).Start(t.Context(), "a", &item{ID: "a"})

	require.ErrorIs(t, err, errCommit)
}

func TestStart__TheKeyIsTheIDOfTheEntity(t *testing.T) {
	store := newMemStore()

	_, err := newOrchestrator(t, pipeline(), store).Start(t.Context(), "a", &item{ID: "b"})

	require.Error(t, err)
	assert.Empty(t, store.rows)
}

func TestStart__ACanceledContextStartsNothing(t *testing.T) {
	store := newMemStore()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := newOrchestrator(t, pipeline(), store).Start(ctx, "a", &item{ID: "a"})

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, store.rows)
}

func TestOnError__AFailRunsTheCommitOfTheDeclaredTransitionToFailedWithTheCause(t *testing.T) {
	cause := errors.New("the reason")
	var seen error
	def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Fail, Cause: cause}))
	def.Transitions[3].Commit = func(_ context.Context, t *tx, e *item) error {
		seen = e.Failure
		t.write("failure")

		return nil
	}

	res, err := run(t, def, store, "a")

	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
	assert.Equal(t, cause, err)
	assert.Equal(t, cause, seen, "the commit reads the cause that the store was given through CauseSetter")
	assert.Equal(t, []string{"failure"}, store.row("a").Commits)
}

func TestOnError__AFailWhoseCommitFailsLeavesTheEntityWhereItWasAndIsBothErrors(t *testing.T) {
	def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Fail}))
	def.Transitions[3].Commit = partial

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, errBoom)
	require.ErrorIs(t, err, errCommit)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Empty(t, store.row("a").Commits)
}

func TestOnError__AFailWithNoDeclaredTransitionToFailedIsAnErrorAndWritesNothing(t *testing.T) {
	def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Fail}))
	def.Transitions = append(def.Transitions[:3], def.Transitions[4:]...)

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, workflow.ErrInvalidDefinition)
	require.ErrorIs(t, err, errBoom, "why it failed is still told")
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Empty(t, store.fails)
	assert.Empty(t, store.updates)
}

func TestOnError__AMoveRunsTheCommitOfTheDeclaredTransition(t *testing.T) {
	def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Move, To: "parked"}))
	def.Transitions[4].Commit = commits("give back")

	res, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Idle}, res)
	assert.Equal(t, []string{"give back"}, store.row("a").Commits)
}

func TestOnError__AMoveWhoseCommitFailsLeavesTheEntityWhereItWas(t *testing.T) {
	def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Move, To: "parked"}))
	def.Transitions[4].Commit = partial

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, errBoom)
	require.ErrorIs(t, err, errCommit)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Empty(t, store.row("a").Commits)
}

func TestOnError__ADecisionIsMadeOnAContextThatTheRunsEndDoesNotCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	def := pipeline()
	def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
		cancel()

		return errBoom
	}
	def.OnError = decides(workflow.Decision{Kind: workflow.Fail})
	var failureCtxErr error
	def.Transitions[3].Commit = func(ctx context.Context, _ *tx, _ *item) error {
		failureCtxErr = ctx.Err()

		return nil
	}
	store := newMemStore(item{ID: "a", Status: "working"})

	// Do fails and then the context ends; OnError is asked about the failure of Do, which came first.
	res, err := newOrchestrator(t, def, store).Run(ctx, "a")

	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, workflow.Final, res.Outcome)
	assert.NoError(t, failureCtxErr, "the failure is recorded although the run was stopped")
	assert.Equal(t, state("failed"), store.row("a").Status)
}

func TestNever__IsAGuardThatNeverHolds(t *testing.T) {
	assert.False(t, workflow.Never(&item{}))
}
