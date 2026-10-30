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

// What the definition's OnError decides, and Advance, which takes an entity out of an idle state.

type onErrorFunc = func(ctx context.Context, e *item, t transition, attempts int, err error) workflow.Decision

// decides is a decision that does not look at what failed.
func decides(d workflow.Decision) onErrorFunc {
	return func(context.Context, *item, transition, int, error) workflow.Decision { return d }
}

// failingScenario is the pipeline with its entity in "working", and a failure of the step out of it, which is the Do of
// the transition (source "do") or the Transition of the store ("update"). The fault of the store is for the first
// Transition only, so that what OnError decides can move the entity.
func failingScenario(source string, onError onErrorFunc) (definition, *memStore) {
	store := newMemStore(item{ID: "a", Status: "working"})
	def := pipeline()
	def.OnError = func(ctx context.Context, e *item, t transition, attempts int, err error) workflow.Decision {
		store.updateErr = nil

		return onError(ctx, e, t, attempts, err)
	}
	switch source {
	case "do":
		def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error { return errBoom }
	case "update":
		store.updateErr = errBoom
	}

	return def, store
}

var failureSources = []string{"do", "update"}

func TestNew__AcceptsAnIdleStateWithExits(t *testing.T) {
	def := pipeline()
	def.Transitions = append(def.Transitions, transition{From: "parked", To: "working"})

	_, err := workflow.New(def, workflow.Entity[*item, *tx](newMemStore()))

	require.NoError(t, err)
}

func TestNew__RejectsExitsOfFinalAndFailedStatesEvenWithIdleOnes(t *testing.T) {
	for _, from := range []state{"done", "failed"} {
		def := pipeline()
		def.Transitions = append(def.Transitions,
			transition{From: "parked", To: "working"},
			transition{From: from, To: "working"},
		)

		_, err := workflow.New(def, workflow.Entity[*item, *tx](newMemStore()))

		require.ErrorIs(t, err, workflow.ErrInvalidDefinition, string(from))
	}
}

func TestRun__StopsAtAnIdleStateWithExitsAndNeverTakesThem(t *testing.T) {
	def := pipeline()
	def.Transitions = append(def.Transitions, transition{From: "parked", To: "working", Do: mark("exit")})
	store := newMemStore(item{ID: "a", Status: "parked"})

	res, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Idle}, res)
	assert.Empty(t, store.updates)
	assert.Empty(t, store.row("a").Trail)
}

// triggered is a workflow whose idle state "parked" has exits, to "working" and to "failed".
func triggered() definition {
	def := pipeline()
	def.Transitions = append(def.Transitions,
		transition{From: "parked", To: "working", Do: mark("resume")},
		transition{From: "parked", To: "failed", Do: mark("reject"), When: func(e *item) bool { return e.Flag != "keep" }},
	)

	return def
}

// resumeStep is the index of the transition from parked to working in triggered.
const resumeStep = 5

func advance(t *testing.T, def definition, store *memStore, id string, to state) (workflow.Result, error) {
	t.Helper()

	return newOrchestrator(t, def, store).Advance(t.Context(), id, to, nil)
}

func TestAdvance__TakesAnIdleEntityAlongTheTransitionToTheTargetAndNoFurther(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "parked"})

	res, err := advance(t, triggered(), store, "a", "working")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Advanced}, res)
	assert.Equal(t, []string{"parked>working"}, store.updates, "working goes on to done, which is for Run")
	assert.Equal(t, state("working"), store.row("a").Status)
	assert.Equal(t, []string{"resume"}, store.row("a").Trail, "its Do ran before the move")
}

func TestAdvance__AnEntityThatIsNotIdleIsNoTransition(t *testing.T) {
	for _, status := range []state{"queued", "working", "done", "failed"} {
		store := newMemStore(item{ID: "a", Status: status})

		res, err := advance(t, triggered(), store, "a", "working")

		require.ErrorIs(t, err, workflow.ErrNoTransition, string(status))
		assert.Equal(t, status, res.State)
		assert.Empty(t, store.updates)
		assert.Empty(t, store.row("a").Trail)
	}
}

func TestAdvance__ATargetNoTransitionGoesToIsNoTransition(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "parked"})

	for _, to := range []state{"done", "parked", "limbo"} {
		_, err := advance(t, triggered(), store, "a", to)

		require.ErrorIs(t, err, workflow.ErrNoTransition, string(to))
	}
	assert.Empty(t, store.updates)
	assert.Empty(t, store.row("a").Trail)
}

func TestAdvance__AGuardThatDoesNotHoldIsNoTransition(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "parked", Flag: "keep"})

	_, err := advance(t, triggered(), store, "a", "failed")

	require.ErrorIs(t, err, workflow.ErrNoTransition)
	assert.Empty(t, store.updates)
	assert.Empty(t, store.row("a").Trail, "a transition whose guard fails does not run")
}

func TestAdvance__TwoTransitionsToTheTargetAreAmbiguous(t *testing.T) {
	def := triggered()
	def.Transitions = append(def.Transitions, transition{From: "parked", To: "working"})
	store := newMemStore(item{ID: "a", Status: "parked"})

	_, err := advance(t, def, store, "a", "working")

	require.ErrorIs(t, err, workflow.ErrAmbiguousTransition)
	assert.Empty(t, store.updates)
}

func TestAdvance__AnEntityThatCannotBeLoadedIsReportedWrapped(t *testing.T) {
	_, err := advance(t, triggered(), newMemStore(), "missing", "working")

	require.ErrorIs(t, err, errNotFound)
	assert.ErrorContains(t, err, "missing")
}

func TestAdvance__ATransitionThatLosesToAPeerIsAConflictAndOverwritesNothing(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "parked"})
	def := triggered()
	// The peer moves the entity on while the handler of the transition runs.
	def.Transitions[resumeStep].Do = func(context.Context, *item, any) error {
		store.mu.Lock()
		r := store.rows["a"]
		r.Status, r.Flag = "done", "peer"
		store.rows["a"] = r
		store.mu.Unlock()

		return nil
	}

	res, err := advance(t, def, store, "a", "working")

	require.ErrorIs(t, err, workflow.ErrConflict)
	assert.NotEqual(t, workflow.Advanced, res.Outcome)
	assert.Equal(t, "peer", store.row("a").Flag)
	assert.Equal(t, state("done"), store.row("a").Status)
	assert.Empty(t, store.updates)
}

func TestAdvance__ACanceledContextStopsBeforeTheHandlerAndBypassesOnError(t *testing.T) {
	ran := false
	def := triggered()
	def.Transitions[resumeStep].Do = func(context.Context, *item, any) error {
		ran = true
		return nil
	}
	def.OnError = func(context.Context, *item, transition, int, error) workflow.Decision {
		t.Error("OnError must not be asked about a canceled context")
		return workflow.Decision{Kind: workflow.Drop}
	}
	store := newMemStore(item{ID: "a", Status: "parked"})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	o := newOrchestrator(t, def, store)

	res, err := o.Advance(ctx, "a", "working", nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Waiting}, res)
	assert.False(t, ran)
	assert.Empty(t, store.updates)
}

func TestAdvance__AFailureOfTheHandlerIsDecidedByOnError(t *testing.T) {
	failingDo := func(context.Context, *item, any) error { return errBoom }
	setup := func(onError onErrorFunc) (definition, *memStore) {
		def := triggered()
		def.Transitions[resumeStep].Do = failingDo
		def.OnError = onError
		return def, newMemStore(item{ID: "a", Status: "parked"})
	}

	t.Run("the default retries below the limit and fails at it", func(t *testing.T) {
		def, store := setup(nil)
		def.Transitions[resumeStep].Retry = workflow.RetryPolicy{MaxAttempts: 2, Backoff: time.Second}

		res, err := advance(t, def, store, "a", "working")

		assert.Equal(t, errBoom, err)
		assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Waiting, RetryAfter: time.Second}, res)

		store.rows["a"] = item{ID: "a", Status: "parked", Attempts: 2}
		res, err = advance(t, def, store, "a", "working")

		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
		assert.Equal(t, []string{"parked"}, store.fails)
	})

	t.Run("Yield is returned with ErrYield and does not move", func(t *testing.T) {
		def, store := setup(decides(workflow.Decision{Kind: workflow.Yield, After: time.Second}))

		res, err := advance(t, def, store, "a", "working")

		require.ErrorIs(t, err, workflow.ErrYield)
		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Waiting, RetryAfter: time.Second}, res)
		assert.Empty(t, store.updates)
	})

	t.Run("Drop is no transition and no error", func(t *testing.T) {
		def, store := setup(decides(workflow.Decision{Kind: workflow.Drop}))

		res, err := advance(t, def, store, "a", "working")

		require.NoError(t, err)
		assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.NoTransition}, res)
	})

	t.Run("Move is not something that Advance does", func(t *testing.T) {
		def, store := setup(decides(workflow.Decision{Kind: workflow.Move, To: "queued"}))

		res, err := advance(t, def, store, "a", "working")

		require.ErrorIs(t, err, workflow.ErrInvalidDefinition)
		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, state("parked"), res.State)
		assert.Empty(t, store.updates)
	})
}

func TestAdvance__ATransitionThatFailsIsDecidedByOnError(t *testing.T) {
	failing := errors.New("cannot write")
	def := triggered()
	def.OnError = decides(workflow.Decision{Kind: workflow.Retry, After: time.Minute})
	store := newMemStore(item{ID: "a", Status: "parked"})
	store.updateErr = failing

	res, err := advance(t, def, store, "a", "working")

	require.ErrorIs(t, err, failing)
	assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Waiting, RetryAfter: time.Minute}, res)
}

func TestOnError__IsGivenTheEntityTheTransitionTheAttemptsAndTheError(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source, func(t *testing.T) {
			var seen struct {
				calls    int
				id       string
				from, to state
				attempts int
				err      error
			}
			def, store := failingScenario(source, func(_ context.Context, e *item, tr transition, attempts int, err error) workflow.Decision {
				seen.calls++
				seen.id, seen.from, seen.to, seen.attempts, seen.err = e.ID, tr.From, tr.To, attempts, err

				return workflow.Decision{Kind: workflow.Drop}
			})
			store.rows["a"] = item{ID: "a", Status: "working", Attempts: 2}
			def.Transitions[pipelineStep].Retry = workflow.RetryPolicy{MaxAttempts: 9}

			_, err := run(t, def, store, "a")

			require.NoError(t, err)
			assert.Equal(t, 1, seen.calls)
			assert.Equal(t, "a", seen.id)
			assert.Equal(t, state("working"), seen.from)
			assert.Equal(t, state("done"), seen.to)
			assert.Equal(t, 2, seen.attempts, "as the store counts them")
			require.ErrorIs(t, seen.err, errBoom)
		})
	}
}

func TestOnError__Retry(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source, func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Retry, After: 3 * time.Second}))

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, errBoom)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: 3 * time.Second}, res)
			assert.Equal(t, state("working"), store.row("a").Status)
			assert.Empty(t, store.fails)
		})
	}

	t.Run("with a cause of its own that is the error returned", func(t *testing.T) {
		cause := errors.New("wrapped for the operator")
		def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Retry, Cause: cause}))

		_, err := run(t, def, store, "a")

		assert.Equal(t, cause, err)
	})
}

func TestOnError__Fail(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source+" records the cause of the decision", func(t *testing.T) {
			cause := errors.New("the reason")
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Fail, Cause: cause}))

			res, err := run(t, def, store, "a")

			assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
			assert.Equal(t, cause, err)
			assert.Equal(t, state("failed"), store.row("a").Status)
			assert.Equal(t, cause, store.row("a").Failure)
			assert.Equal(t, []string{"working"}, store.fails)
		})

		t.Run(source+" without a cause records the error", func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Fail}))

			res, err := run(t, def, store, "a")

			assert.Equal(t, workflow.Final, res.Outcome)
			require.ErrorIs(t, err, errBoom)
			require.ErrorIs(t, store.row("a").Failure, errBoom)
		})

		t.Run(source+" that loses to a peer is a yield", func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Fail}))
			store.failErr = workflow.ErrConflict

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, workflow.ErrYield)
			require.ErrorIs(t, err, workflow.ErrConflict)
			require.ErrorIs(t, err, errBoom)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
		})

		t.Run(source+" that cannot be recorded is both errors", func(t *testing.T) {
			failing := errors.New("cannot write")
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Fail}))
			store.failErr = failing

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, errBoom)
			require.ErrorIs(t, err, failing)
			assert.NotErrorIs(t, err, workflow.ErrYield)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
		})
	}
}

func TestOnError__Move(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source+" to an idle state ends the run there", func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Move, To: "parked"}))

			res, err := run(t, def, store, "a")

			require.NoError(t, err)
			assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Idle}, res)
			assert.Equal(t, state("parked"), store.row("a").Status)
			assert.Equal(t, []string{"working>parked"}, store.updates)
		})

		t.Run(source+" to a final state ends the run there", func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Move, To: "done"}))

			res, err := run(t, def, store, "a")

			require.NoError(t, err)
			assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res)
			assert.Equal(t, []string{"working>done"}, store.updates)
		})

		t.Run(source+" to a state that no declared transition goes to is an invalid definition", func(t *testing.T) {
			for _, to := range []state{"limbo", "queued"} {
				def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Move, To: to}))

				res, err := run(t, def, store, "a")

				require.ErrorIs(t, err, workflow.ErrInvalidDefinition, string(to))
				require.ErrorIs(t, err, errBoom, string(to))
				assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
				assert.Equal(t, state("working"), store.row("a").Status)
				assert.Empty(t, store.updates)
			}
		})

		t.Run(source+" that cannot be written is both errors", func(t *testing.T) {
			failing := errors.New("cannot write")
			def, store := failingScenario(source, nil)
			def.OnError = func(context.Context, *item, transition, int, error) workflow.Decision {
				store.updateErr = failing

				return workflow.Decision{Kind: workflow.Move, To: "parked"}
			}

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, errBoom)
			require.ErrorIs(t, err, failing)
			assert.NotErrorIs(t, err, workflow.ErrYield)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
		})
	}

	t.Run("that loses to a peer is a yield", func(t *testing.T) {
		def, store := failingScenario("do", decides(workflow.Decision{Kind: workflow.Move, To: "parked"}))
		// The peer settles the entity while the handler runs, and the handler then fails.
		def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
			store.mu.Lock()
			r := store.rows["a"]
			r.Status = "done"
			store.rows["a"] = r
			store.mu.Unlock()

			return errBoom
		}

		res, err := run(t, def, store, "a")

		require.ErrorIs(t, err, workflow.ErrYield)
		require.ErrorIs(t, err, workflow.ErrConflict)
		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, workflow.Waiting, res.Outcome)
		assert.Equal(t, state("done"), store.row("a").Status, "the peer's outcome stands")
	})

	t.Run("update that loses to a peer is a yield", func(t *testing.T) {
		def, store := failingScenario("update", decides(workflow.Decision{Kind: workflow.Move, To: "parked"}))
		def.OnError = decides(workflow.Decision{Kind: workflow.Move, To: "parked"})
		store.updateErr = workflow.ErrConflict

		_, err := run(t, def, store, "a")

		require.ErrorIs(t, err, workflow.ErrYield)
		require.ErrorIs(t, err, workflow.ErrConflict)
	})

	t.Run("carries on from the state it moves to", func(t *testing.T) {
		store := newMemStore(item{ID: "a", Status: "working"})
		def := pipeline()
		def.Transitions = append(def.Transitions, transition{From: "working", To: "queued", When: workflow.Never[*item]})
		failures := 0
		def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
			if failures++; failures == 1 {
				return errBoom
			}

			return nil
		}
		def.OnError = decides(workflow.Decision{Kind: workflow.Move, To: "queued"})

		res, err := run(t, def, store, "a")

		require.NoError(t, err)
		assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res)
		assert.Equal(t, []string{"working>queued", "queued>working", "working>done"}, store.updates)
		assert.Equal(t, 2, failures)
	})
}

func TestOnError__Yield(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source, func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Yield, After: 2 * time.Second}))

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, workflow.ErrYield)
			require.ErrorIs(t, err, errBoom)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: 2 * time.Second}, res)
			assert.Equal(t, state("working"), store.row("a").Status)
			assert.Empty(t, store.fails)
		})
	}
}

func TestOnError__Drop(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source, func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.Drop}))

			res, err := run(t, def, store, "a")

			require.NoError(t, err)
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.NoTransition}, res)
			assert.Equal(t, state("working"), store.row("a").Status)
			assert.Empty(t, store.fails)
		})
	}
}

func TestOnError__AnUnknownDecisionIsARetryThatSaysSo(t *testing.T) {
	for _, source := range failureSources {
		t.Run(source, func(t *testing.T) {
			def, store := failingScenario(source, decides(workflow.Decision{Kind: workflow.DecisionKind(99), After: time.Second}))

			res, err := run(t, def, store, "a")

			require.ErrorIs(t, err, errBoom)
			assert.ErrorContains(t, err, "unknown decision")
			assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: time.Second}, res)
			assert.Empty(t, store.fails)
		})
	}
}

func TestOnError__MayFallBackToDefaultOnError(t *testing.T) {
	def := flaky(errBoom, workflow.RetryPolicy{MaxAttempts: 3, Backoff: time.Second})
	def.OnError = func(ctx context.Context, e *item, tr transition, attempts int, err error) workflow.Decision {
		return workflow.DefaultOnError(ctx, e, tr, attempts, err)
	}

	store := newMemStore(item{ID: "a", Status: "working", Attempts: 1})
	res, err := run(t, def, store, "a")
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting, RetryAfter: time.Second}, res)

	store = newMemStore(item{ID: "a", Status: "working", Attempts: 2})
	res, err = run(t, def, store, "a")
	require.ErrorIs(t, err, errBoom)
	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
}

func TestDefaultOnError(t *testing.T) {
	retry := transition{Retry: workflow.RetryPolicy{MaxAttempts: 3, Backoff: 4 * time.Second}}
	decide := func(tr transition, attempts int, err error) workflow.Decision {
		return workflow.DefaultOnError(t.Context(), &item{}, tr, attempts, err)
	}

	t.Run("retries below the limit after the backoff", func(t *testing.T) {
		assert.Equal(t, workflow.Decision{Kind: workflow.Retry, After: 4 * time.Second}, decide(retry, 2, errBoom))
	})
	t.Run("fails at the limit and above it with the error", func(t *testing.T) {
		for _, attempts := range []int{3, 4} {
			assert.Equal(t, workflow.Decision{Kind: workflow.Fail, Cause: errBoom}, decide(retry, attempts, errBoom))
		}
	})
	t.Run("fails an unretriable error whatever attempts are left", func(t *testing.T) {
		cause := workflow.Unretriable(errBoom)

		assert.Equal(t, workflow.Decision{Kind: workflow.Fail, Cause: cause}, decide(retry, 0, cause))
	})
	t.Run("no retry policy fails at once", func(t *testing.T) {
		assert.Equal(t, workflow.Fail, decide(transition{}, 0, errBoom).Kind)
	})
}

func TestOnError__IsNotAskedAboutAContextThatEnded(t *testing.T) {
	asked := false
	onError := func(context.Context, *item, transition, int, error) workflow.Decision {
		asked = true
		return workflow.Decision{Kind: workflow.Fail}
	}

	t.Run("before the handler", func(t *testing.T) {
		def := pipeline()
		def.OnError = onError
		store := newMemStore(item{ID: "a", Status: "working"})
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		o := newOrchestrator(t, def, store)

		res, err := o.Run(ctx, "a")

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	})

	t.Run("between the handler and the transition", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		def := pipeline()
		def.OnError = onError
		def.Transitions[pipelineStep].Do = func(context.Context, *item, any) error {
			cancel()
			return nil
		}
		store := newMemStore(item{ID: "a", Status: "working"})
		o := newOrchestrator(t, def, store)

		res, err := o.Run(ctx, "a")

		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, workflow.Result{State: "working", Outcome: workflow.Waiting}, res)
	})

	assert.False(t, asked, "being stopped is not a failure of the entity")
}

func TestOnError__IsAskedAboutATransitionThatFailsWithAnEndedContext(t *testing.T) {
	// A store whose write is cut short by the context reports it as the failure of the write, and the workflow may
	// still have a failure to record, which it does on a context of its own.
	def, store := failingScenario("update", decides(workflow.Decision{Kind: workflow.Fail}))
	store.updateErr = context.Canceled

	res, err := run(t, def, store, "a")

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, workflow.Result{State: "failed", Outcome: workflow.Final}, res)
}
