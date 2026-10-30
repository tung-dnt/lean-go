package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflow "github.com/tung-dnt/lean-go/pkg/workflow"
)

type (
	state      = workflow.State
	definition = workflow.Definition[*item, *tx]
	transition = workflow.Transition[*item, *tx]
)

// item is the entity of the tests: a status, how many attempts have been counted on it, what was done to it, and what
// the commits of its transitions wrote.
type item struct {
	ID       string
	Status   state
	Attempts int
	Flag     string
	Trail    []string
	Failure  error
	Commits  []string
}

// tx is what a Commit writes through: a copy of the row that the store keeps only if the whole transaction commits.
type tx struct{ row *item }

// write records a write of a Commit on the row of the transaction.
func (t *tx) write(what string) { t.row.Commits = append(slices.Clone(t.row.Commits), what) }

// memStore is an in-memory Entity that models a transaction: Insert and Transition run their commit on a copy of the row
// and keep the copy, with the new status, only if the commit returns nil. Its Transition is a compare-and-set on the
// status, as the contract says. Its faults are set by a test before the run.
type memStore struct {
	mu   sync.Mutex
	rows map[string]item

	loadErr   error
	insertErr error
	claimErr  error
	updateErr error
	failErr   error

	// updates are the transitions made, as "from>to", except those to the failed state, which are fails (the state
	// they were made from).
	updates []string
	fails   []string
	// events is what happened, in order: "claim", and whatever a test notes.
	events []string
}

func newMemStore(rows ...item) *memStore {
	s := &memStore{rows: make(map[string]item, len(rows))}
	for _, r := range rows {
		s.rows[r.ID] = r
	}

	return s
}

func (s *memStore) row(id string) item {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rows[id]
}

// note records that something happened, for the order of events to be checked.
func (s *memStore) note(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)
}

func (s *memStore) Load(_ context.Context, id string) (*item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	r, ok := s.rows[id]
	if !ok {
		return nil, errNotFound
	}

	return &r, nil
}

func (s *memStore) Insert(_ context.Context, e *item, commit func(*tx) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return false, s.insertErr
	}
	if _, exists := s.rows[e.ID]; exists {
		return false, nil
	}
	w := *e
	w.Status = "queued"
	if commit != nil {
		if err := commit(&tx{row: &w}); err != nil {
			return false, err
		}
	}
	s.rows[e.ID] = w

	return true, nil
}

func (s *memStore) Transition(_ context.Context, e *item, from, to state, commit func(*tx) error) (*item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	failing := to == "failed"
	switch {
	case failing && s.failErr != nil:
		return nil, s.failErr
	case !failing && s.updateErr != nil:
		return nil, s.updateErr
	}
	r := s.rows[e.ID]
	if r.Status != from {
		return nil, workflow.ErrConflict
	}
	w := r
	if commit != nil {
		if err := commit(&tx{row: &w}); err != nil {
			return nil, err
		}
	}
	if failing {
		w.Status, w.Failure = to, e.Failure
		s.fails = append(s.fails, string(from))
	} else {
		w.Status, w.Attempts, w.Trail = to, e.Attempts, e.Trail
		s.updates = append(s.updates, string(from)+">"+string(to))
	}
	s.rows[e.ID] = w

	return &w, nil
}

func (s *memStore) Claim(_ context.Context, e *item) (*item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	r := s.rows[e.ID]
	if r.Status != e.Status {
		return nil, workflow.ErrConflict
	}
	r.Attempts++
	s.rows[e.ID] = r
	s.events = append(s.events, "claim")

	return &r, nil
}

// WithCause is how the cause of a Fail reaches the commit of the transition to the failed state.
func (*memStore) WithCause(e *item, cause error) *item {
	failing := *e
	failing.Failure = cause

	return &failing
}

func (s *memStore) Status(e *item) state { return e.Status }
func (s *memStore) ID(e *item) string    { return e.ID }
func (s *memStore) Attempts(e *item) int { return e.Attempts }

var errNotFound = errors.New("no such item")

// mark is a Do that records that it ran.
func mark(name string) func(context.Context, *item, any) error {
	return func(_ context.Context, e *item, _ any) error {
		e.Trail = append(e.Trail, name)
		return nil
	}
}

// commits is a Commit that records what it wrote.
func commits(name string) func(context.Context, *tx, *item) error {
	return func(_ context.Context, t *tx, _ *item) error {
		t.write(name)
		return nil
	}
}

// initial is the transition that Start takes.
func initial(to state) transition { return transition{To: to} }

// failsTo is the declared transition that a Fail from the state takes.
func failsTo(from state) transition {
	return transition{From: from, To: "failed", When: workflow.Never[*item]}
}

// pipeline is queued > working > done, with a state that waits (parked) and the failed state. A Fail from working and a
// Move from working to parked are declared, and only OnError takes them.
func pipeline() definition {
	return definition{
		Name:   "pipeline",
		Idles:  []state{"parked"},
		Finals: []state{"done"},
		Failed: "failed",
		Transitions: []transition{
			initial("queued"),
			{From: "queued", To: "working"},
			{From: "working", To: "done", Do: mark("work")},
			failsTo("working"),
			{From: "working", To: "parked", When: workflow.Never[*item]},
		},
	}
}

// pipelineStep is the index of the transition from working to done in pipeline.
const pipelineStep = 2

func newOrchestrator(t *testing.T, def definition, store *memStore) *workflow.Orchestrator[*item, *tx] {
	t.Helper()

	o, err := workflow.New(def, workflow.Entity[*item, *tx](store))
	require.NoError(t, err)

	return o
}

func run(t *testing.T, def definition, store *memStore, id string) (workflow.Result, error) {
	t.Helper()

	return newOrchestrator(t, def, store).Run(t.Context(), id)
}

func TestNew__RejectsWhatIsNoWorkflow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*definition)
	}{
		{"no name", func(d *definition) { d.Name = "" }},
		{"no failed state", func(d *definition) { d.Failed = "" }},
		{"failed state that idles", func(d *definition) { d.Idles = append(d.Idles, "failed") }},
		{"state both idle and final", func(d *definition) { d.Finals = append(d.Finals, "parked") }},
		{"transition out of a final state", func(d *definition) {
			d.Transitions = append(d.Transitions, transition{From: "done", To: "working"})
		}},
		{"transition out of an idle state into nothing", func(d *definition) {
			d.Transitions = append(d.Transitions, transition{From: "parked", To: "limbo"})
		}},
		{"transition out of the failed state", func(d *definition) {
			d.Transitions = append(d.Transitions, transition{From: "failed", To: "working"})
		}},
		{"transition into nothing", func(d *definition) {
			d.Transitions = append(d.Transitions, transition{From: "queued", To: "limbo", When: func(*item) bool { return false }})
		}},
		{"a second initial transition", func(d *definition) { d.Transitions = append(d.Transitions, transition{To: "done"}) }},
		{"transition without a target", func(d *definition) { d.Transitions = append(d.Transitions, transition{From: "queued"}) }},
		{"no initial transition", func(d *definition) { d.Transitions = d.Transitions[1:] }},
		{"initial transition into a final state", func(d *definition) { d.Transitions[0] = initial("done") }},
		{"initial transition into the failed state", func(d *definition) { d.Transitions[0] = initial("failed") }},
		{"initial transition into nothing", func(d *definition) { d.Transitions[0] = initial("limbo") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def := pipeline()
			tc.change(&def)

			o, err := workflow.New(def, workflow.Entity[*item, *tx](newMemStore()))

			require.ErrorIs(t, err, workflow.ErrInvalidDefinition)
			assert.Nil(t, o)
		})
	}

	t.Run("no store", func(t *testing.T) {
		_, err := workflow.New[*item, *tx](pipeline(), nil)

		require.ErrorIs(t, err, workflow.ErrInvalidDefinition)
	})
}

func TestNew__AcceptsAnInitialTransitionIntoAnIdleState(t *testing.T) {
	def := pipeline()
	def.Transitions[0] = initial("parked")

	_, err := workflow.New(def, workflow.Entity[*item, *tx](newMemStore()))

	require.NoError(t, err)
}

func TestNew__AcceptsATransitionIntoEveryKindOfState(t *testing.T) {
	def := pipeline()
	def.Transitions = append(def.Transitions,
		transition{From: "queued", To: "parked", When: func(e *item) bool { return e.Flag == "park" }},
		transition{From: "queued", To: "failed", When: func(e *item) bool { return e.Flag == "reject" }},
	)

	_, err := workflow.New(def, workflow.Entity[*item, *tx](newMemStore()))

	require.NoError(t, err)
}

func TestNew__CopiesTheDefinition(t *testing.T) {
	def := pipeline()
	store := newMemStore(item{ID: "a", Status: "queued"})
	o, err := workflow.New(def, workflow.Entity[*item, *tx](store))
	require.NoError(t, err)

	// A change made afterwards to the caller's slice is not the workflow's.
	def.Transitions[pipelineStep].Do = mark("changed")
	def.Transitions[pipelineStep].To = "parked"
	def.Transitions[0].To = "working"
	res, err := o.Run(t.Context(), "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res)
	assert.Equal(t, []string{"work"}, store.row("a").Trail)

	res, err = o.Start(t.Context(), "b", &item{ID: "b"})

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "queued", Outcome: workflow.Advanced}, res)
}

func TestRun__TakesAnEntityThroughEveryStateToTheFinalOne(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "queued"})

	res, err := run(t, pipeline(), store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res)
	assert.Equal(t, []string{"queued>working", "working>done"}, store.updates)
	assert.Equal(t, state("done"), store.row("a").Status)
	assert.Equal(t, []string{"work"}, store.row("a").Trail, "a transition with no Do moves the entity and runs nothing")
}

func TestRun__ContinuesFromTheStateTheEntityIsIn(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, pipeline(), store, "a")

	require.NoError(t, err)
	assert.Equal(t, state("done"), res.State)
	assert.Equal(t, []string{"working>done"}, store.updates)
}

func TestRun__AFinalEntityIsLeftAloneAndNothingRuns(t *testing.T) {
	for _, status := range []state{"done", "failed"} {
		t.Run(string(status), func(t *testing.T) {
			store := newMemStore(item{ID: "a", Status: status})

			res, err := run(t, pipeline(), store, "a")

			require.NoError(t, err)
			assert.Equal(t, workflow.Result{State: status, Outcome: workflow.Final}, res)
			assert.Empty(t, store.updates)
			assert.Empty(t, store.row("a").Trail)
		})
	}
}

func TestRun__StopsAtAnIdleState(t *testing.T) {
	def := pipeline()
	def.Transitions[1] = transition{From: "queued", To: "parked", Do: mark("park")}
	store := newMemStore(item{ID: "a", Status: "queued"})

	res, err := run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "parked", Outcome: workflow.Idle}, res)
	assert.Equal(t, []string{"queued>parked"}, store.updates)

	// Run again, it is where it was and nothing more happens.
	res, err = run(t, def, store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Outcome(workflow.Idle), res.Outcome)
	assert.Equal(t, []string{"queued>parked"}, store.updates)
	assert.Equal(t, []string{"park"}, store.row("a").Trail)
}

func TestRun__TheGuardsChooseTheTransition(t *testing.T) {
	def := definition{
		Name:   "routing",
		Finals: []state{"fast", "slow"},
		Failed: "failed",
		Transitions: []transition{
			initial("start"),
			{From: "start", To: "fast", When: func(e *item) bool { return e.Flag == "fast" }, Do: mark("fast")},
			{From: "start", To: "slow", When: func(e *item) bool { return e.Flag != "fast" }, Do: mark("slow")},
		},
	}
	store := newMemStore(item{ID: "a", Status: "start", Flag: "fast"}, item{ID: "b", Status: "start"})

	a, err := run(t, def, store, "a")
	require.NoError(t, err)
	b, err := run(t, def, store, "b")
	require.NoError(t, err)

	assert.Equal(t, state("fast"), a.State)
	assert.Equal(t, state("slow"), b.State)
	assert.Equal(t, []string{"fast"}, store.row("a").Trail)
	assert.Equal(t, []string{"slow"}, store.row("b").Trail)
}

func TestRun__NoPassingGuardIsNoTransitionAndNotAnError(t *testing.T) {
	def := definition{
		Name:   "guarded",
		Finals: []state{"done"},
		Failed: "failed",
		Transitions: []transition{
			initial("start"),
			{From: "start", To: "done", When: func(*item) bool { return false }, Do: mark("never")},
		},
	}
	store := newMemStore(item{ID: "a", Status: "start"}, item{ID: "b", Status: "unheard-of"})

	for _, id := range []string{"a", "b"} {
		res, err := run(t, def, store, id)

		require.NoError(t, err)
		assert.Equal(t, workflow.NoTransition, res.Outcome)
		assert.Equal(t, store.row(id).Status, res.State)
		assert.Empty(t, store.row(id).Trail)
	}
	assert.Empty(t, store.updates)
}

func TestRun__TwoPassingGuardsAreAnError(t *testing.T) {
	def := definition{
		Name:   "ambiguous",
		Finals: []state{"x", "y"},
		Failed: "failed",
		Transitions: []transition{
			initial("start"),
			{From: "start", To: "x", Do: mark("x")},
			{From: "start", To: "y", When: func(*item) bool { return true }, Do: mark("y")},
		},
	}
	store := newMemStore(item{ID: "a", Status: "start"})

	_, err := run(t, def, store, "a")

	require.ErrorIs(t, err, workflow.ErrAmbiguousTransition)
	assert.Empty(t, store.row("a").Trail, "neither ran")
	assert.Empty(t, store.updates)
}

func TestRun__ADeclaredFailureOrMoveIsNeverPickedByARun(t *testing.T) {
	store := newMemStore(item{ID: "a", Status: "working"})

	res, err := run(t, pipeline(), store, "a")

	require.NoError(t, err)
	assert.Equal(t, workflow.Result{State: "done", Outcome: workflow.Final}, res, "the guard Never keeps Run off the edges to failed and parked")
	assert.Empty(t, store.fails)
}

func TestRun__AnEntityThatCannotBeLoadedIsReportedWrapped(t *testing.T) {
	store := newMemStore()

	_, err := run(t, pipeline(), store, "missing")
	require.ErrorIs(t, err, errNotFound)

	store.loadErr = errors.New("database is down")
	_, err = run(t, pipeline(), store, "missing")
	require.ErrorContains(t, err, "database is down")
}

func TestRun__StopsAtTheTransitionLimit(t *testing.T) {
	// An entity that goes round for ever is stopped, and keeps what it was given.
	loop := definition{
		Name:   "loop",
		Finals: []state{"done"},
		Failed: "failed",
		Transitions: []transition{
			initial("ping"),
			{From: "ping", To: "pong"},
			{From: "pong", To: "ping"},
		},
	}
	store := newMemStore(item{ID: "a", Status: "ping"})

	res, err := run(t, loop, store, "a")

	require.ErrorIs(t, err, workflow.ErrTransitionLimit)
	assert.Len(t, store.updates, workflow.MaxTransitions)
	assert.Zero(t, res.Outcome)
}

func TestRun__ReachingTheEndOnTheLastAllowedTransitionSucceeds(t *testing.T) {
	chain := func(length int) definition {
		def := definition{Name: "chain", Finals: []state{state(fmt.Sprintf("s%d", length))}, Failed: "failed", Transitions: []transition{initial("s0")}}
		for i := range length {
			def.Transitions = append(def.Transitions, transition{From: state(fmt.Sprintf("s%d", i)), To: state(fmt.Sprintf("s%d", i+1))})
		}

		return def
	}

	store := newMemStore(item{ID: "a", Status: "s0"})
	res, err := run(t, chain(workflow.MaxTransitions), store, "a")
	require.NoError(t, err)
	assert.Equal(t, workflow.Final, res.Outcome)

	store = newMemStore(item{ID: "a", Status: "s0"})
	_, err = run(t, chain(workflow.MaxTransitions+1), store, "a")
	require.ErrorIs(t, err, workflow.ErrTransitionLimit)
}

func TestRun__OneOrchestratorServesConcurrentEntities(t *testing.T) {
	const entities = 32
	rows := make([]item, entities)
	for i := range rows {
		rows[i] = item{ID: fmt.Sprintf("e%d", i), Status: "queued"}
	}
	store := newMemStore(rows...)
	o := newOrchestrator(t, pipeline(), store)

	var wg sync.WaitGroup
	results := make([]workflow.Result, entities)
	errs := make([]error, entities)
	for i := range rows {
		wg.Go(func() { results[i], errs[i] = o.Run(t.Context(), rows[i].ID) })
	}
	wg.Wait()

	for i := range rows {
		require.NoError(t, errs[i])
		assert.Equal(t, state("done"), results[i].State)
		assert.Equal(t, []string{"work"}, store.row(rows[i].ID).Trail)
	}
}
