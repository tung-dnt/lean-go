// Package workflow drives an entity through a workflow: one Definition holds
// the states and the transitions (each with its guard, its work, its commit and
// its retry policy), an Entity stores the entity and is its only persistence
// boundary, and an Orchestrator validates the definition once and runs entities
// through it.
//
// The shape follows nestflow. The handler is bound to the transition, and
// transitions are indexed by their source state, so one step is one map lookup
// and no search for a handler. The entity's own status is its checkpoint: the
// orchestrator keeps nothing, and everything it learns it reads back from the
// Entity.
//
// A transition has two halves. Do prepares: it reads, and it does the work that
// can be done again (storing an object under a key that is the same every time).
// Commit holds every database write of the step. The orchestrator never runs
// Commit itself: it hands it to Entity.Transition, which runs it in the one
// transaction that also checks the entity's status (a compare-and-set on the state
// the entity was in) and writes the new one. So the writes of a step and its new
// status are applied together or not at all, and a peer that moved the entity
// first is reported as ErrConflict and never overwritten. The Tx type parameter is
// what a Commit writes through, whatever the Entity's transaction is.
//
// Do is replay-safe by contract: it is not part of the transaction, so it may run
// again after a failed Commit or a lost Transition.
//
// An Orchestrator holds only the immutable definition. One serves any number of
// concurrent Runs of different, or even the same, entities.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// MaxTransitions bounds the work of one Run. Reaching an idle or final state on
// the last allowed transition succeeds.
const MaxTransitions = 1000

// decisionTimeout bounds the write of a Fail or Move decision. It is made on a
// context of its own, because the run that decided it is often one that was
// stopped, and a failure that cannot be recorded is not a failure that was dealt
// with.
const decisionTimeout = 10 * time.Second

// State names a state of a definition.
type State string

// RetryPolicy says what becomes of an entity whose handler failed, for the
// workflows that leave the decision to DefaultOnError. MaxAttempts is compared
// with Entity.Attempts after the failure: at or above it the entity goes to
// Failed, below it Run returns the error and Backoff as the advice for when to
// try again. Zero is no retry.
type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// Transition takes an entity from one state to another. From "" is the initial
// transition, which only Start takes. When, if set, is the guard that must hold for
// the transition to be taken by Run or Advance, and Do, if set, the work that
// runs before the entity is moved, with the payload that Advance was given (Run
// passes nil). Commit, if set, writes what the step changes, through the
// transaction of Entity.Transition, with the new status. Retry decides what a
// failure of Do leads to. CountsAttempt has the orchestrator call Entity.Claim,
// which counts an attempt on the entity, before Do runs.
//
// A transition that only OnError takes (a Fail or a Move) is declared like any
// other, with Never as its guard so that Run and Advance leave it alone. Its Do
// and CountsAttempt are not used: a decision has no work to do and counts nothing.
type Transition[E, Tx any] struct {
	From, To      State
	When          func(E) bool
	Do            func(ctx context.Context, e E, payload any) error
	Commit        func(ctx context.Context, tx Tx, e E) error
	Retry         RetryPolicy
	CountsAttempt bool
}

// Never is the guard of a transition that only OnError takes: Run and Advance
// never pick it, a Fail or a Move decision does.
func Never[E any](E) bool { return false }

// Definition is a whole workflow. Run stops at an idle state, which waits for
// something outside the workflow (Advance takes it on from there), and at a
// final one, which is the end. Failed is the final state that an entity is sent
// to when it cannot proceed. A final or failed state has no outgoing transition;
// an idle one has, and only Advance takes them.
//
// Exactly one transition has From "": the initial one, which Start takes to a
// state that is not final.
//
// OnError decides what a failure of a transition leads to; nil is DefaultOnError.
type Definition[E, Tx any] struct {
	Name          string
	Idles, Finals []State
	Failed        State
	Transitions   []Transition[E, Tx]
	OnError       func(ctx context.Context, e E, t Transition[E, Tx], attempts int, err error) Decision
}

// DecisionKind is what OnError decides a failure leads to.
type DecisionKind int

const (
	// Retry leaves the entity where it is and reports Waiting, with After as the advice for when to try again.
	Retry DecisionKind = iota + 1
	// Fail sends the entity to the definition's Failed state along the transition declared from where it is to
	// Failed, recording Cause.
	Fail
	// Move sends the entity to the state To along the transition declared from where it is to To, and Run carries on
	// from there.
	Move
	// Yield leaves the entity to the run that changed it: Waiting, with After as the advice, and ErrYield in the error.
	Yield
	// Drop is that nothing is left to do: NoTransition, and no error.
	Drop
)

// Decision is the answer of OnError. Cause is the error that is returned with the
// outcome, and is recorded by Fail; nil is the error that OnError was given. To is
// for Move and After for Retry and Yield.
type Decision struct {
	Kind  DecisionKind
	To    State
	After time.Duration
	Cause error
}

// DefaultOnError is what a Definition does that sets no OnError, and what one that
// does may fall back to: an unretriable error, or any error once the entity has
// used the transition's Retry.MaxAttempts attempts, is a Fail; below that it is
// a Retry after Retry.Backoff.
func DefaultOnError[E, Tx any](_ context.Context, _ E, t Transition[E, Tx], attempts int, err error) Decision {
	if IsUnretriable(err) || attempts >= t.Retry.MaxAttempts {
		return Decision{Kind: Fail, Cause: err}
	}

	return Decision{Kind: Retry, After: t.Retry.Backoff}
}

// Entity is what the workflow runs on, and the only thing that persists it. Tx is
// what a Commit writes through.
//
// Insert stores e, in the definition's initial state, in one transaction with
// commit (which is nil when the initial transition has none), and does nothing
// if an entity with its ID exists: inserted is then false.
//
// Transition moves the entity from one state to the next and returns it as stored.
// In one transaction it locks the entity, writes only if it is still in from (and
// at the attempt that e counted, if the store fences on it), runs commit, which is
// nil for a transition that has none, and writes to as the new state. It reports
// ErrConflict (possibly wrapped) when the entity is not as e saw it, and an error
// of commit leaves nothing of the transaction. The status is Transition's to write,
// not a Commit's, with one exception that a Commit has to state where it makes it:
// a table whose own non-deferrable CHECK ties other columns to the status (a failure
// reason that may only stand with the failed status) has them change in one
// statement, so the Commit of that transition writes the status too, and the write
// that ends Transition then finds it as it is.
//
// Claim counts an attempt on the entity under the same lock and a check of its
// state, and returns it as stored, or ErrConflict if it is no longer where it was.
//
// Load of an entity that does not exist returns an error that says so; Run passes
// it on, wrapped.
type Entity[E, Tx any] interface {
	Load(ctx context.Context, id string) (E, error)
	Insert(ctx context.Context, e E, commit func(Tx) error) (inserted bool, err error)
	Transition(ctx context.Context, e E, from, to State, commit func(Tx) error) (E, error)
	Claim(ctx context.Context, e E) (E, error)
	Status(e E) State
	ID(e E) string
	Attempts(e E) int
}

// CauseSetter is what an Entity may also implement, to have the cause of a Fail
// available to the Commit of the transition to Failed: the orchestrator calls
// WithCause with the entity before it makes the transition, and uses what it
// returns for it.
type CauseSetter[E any] interface {
	WithCause(e E, cause error) E
}

// Outcome is where a Run, a Start or an Advance stopped.
type Outcome int

const (
	// Final means the entity is in a final state, Failed included.
	Final Outcome = iota + 1
	// Idle means the entity waits in an idle state.
	Idle
	// NoTransition means the entity is in a state that has no transition whose guard holds, or that OnError dropped it.
	NoTransition
	// Waiting means the run stopped short of the end and the entity is to be run again: RetryAfter is the advice for when.
	Waiting
	// Advanced means Start inserted the entity, or Advance took it along a transition out of an idle state.
	Advanced
	// Existing means Start found the entity there already, and did nothing: State is the one it is in.
	Existing
)

// Result is what a Run reports. State is the state the entity was left in. After
// a failure that is to be retried, RetryAfter is the advice for how long to wait.
type Result struct {
	State      State
	Outcome    Outcome
	RetryAfter time.Duration
}

var (
	// ErrInvalidDefinition is returned by New for a definition that is not a workflow, and by a run for a Fail or a
	// Move that no declared transition takes.
	ErrInvalidDefinition = errors.New("invalid workflow definition")
	// ErrConflict is what an Entity reports when a peer moved the entity first.
	ErrConflict = errors.New("the entity was changed by another run")
	// ErrAmbiguousTransition is returned when two transitions of the state hold.
	ErrAmbiguousTransition = errors.New("more than one transition applies")
	// ErrTransitionLimit is returned when a Run would exceed MaxTransitions.
	ErrTransitionLimit = errors.New("transition limit reached")
	// ErrNoTransition is returned by Advance when the entity is not idle, or no transition of its state goes to the target.
	ErrNoTransition = errors.New("no such transition")
	// ErrYield is in the error of a Run that left the entity to another run, by a Yield decision or because a
	// failure could not be recorded for a conflict.
	ErrYield = errors.New("the entity is being run by another process")
)

// unretriable marks an error that retrying cannot cure.
type unretriable struct{ err error }

func (u *unretriable) Error() string { return u.err.Error() }

func (u *unretriable) Unwrap() error { return u.err }

// Unretriable marks err as one that a retry cannot cure: when a Do returns it, the
// entity goes to Failed at once, whatever attempts it has left. It stays
// reachable through errors.Is and errors.As. A nil error stays nil.
func Unretriable(err error) error {
	if err == nil {
		return nil
	}

	return &unretriable{err: err}
}

// IsUnretriable reports whether err, or an error it wraps, was marked by Unretriable.
func IsUnretriable(err error) bool {
	_, ok := errors.AsType[*unretriable](err)

	return ok
}

// Orchestrator runs entities through a validated definition.
type Orchestrator[E, Tx any] struct {
	name    string
	failed  State
	finals  map[State]struct{}
	idles   map[State]struct{}
	initial Transition[E, Tx]
	byFrom  map[State][]Transition[E, Tx]
	onError func(ctx context.Context, e E, t Transition[E, Tx], attempts int, err error) Decision
	store   Entity[E, Tx]
}

// New validates def and indexes its transitions by source state. It copies the
// definition, so changing def afterwards changes nothing. Every error is
// ErrInvalidDefinition.
//
// The definition must have a name and a Failed state, which is final and not
// idle. Idle and final states must not overlap, and no final or failed state
// may have an outgoing transition; an idle one may. Exactly one transition is the
// initial one (From ""), and it goes to a state that is not final. Every other
// transition names its From and To, and every To is a state that the definition
// knows: an idle, final or failed state, or the From of some transition. Do and
// Commit may be nil.
func New[E, Tx any](def Definition[E, Tx], store Entity[E, Tx]) (*Orchestrator[E, Tx], error) {
	if store == nil {
		return nil, fmt.Errorf("%w: no entity store", ErrInvalidDefinition)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("%w: no name", ErrInvalidDefinition)
	}
	if def.Failed == "" {
		return nil, fmt.Errorf("%w: %s has no failed state", ErrInvalidDefinition, def.Name)
	}

	o := &Orchestrator[E, Tx]{
		name:    def.Name,
		failed:  def.Failed,
		finals:  make(map[State]struct{}, len(def.Finals)+1),
		idles:   make(map[State]struct{}, len(def.Idles)),
		byFrom:  make(map[State][]Transition[E, Tx], len(def.Transitions)),
		onError: def.OnError,
		store:   store,
	}
	if o.onError == nil {
		o.onError = DefaultOnError[E, Tx]
	}
	for _, s := range def.Idles {
		o.idles[s] = struct{}{}
	}
	for _, s := range slices.Concat(def.Finals, []State{def.Failed}) {
		if _, idle := o.idles[s]; idle {
			return nil, fmt.Errorf("%w: %s: state %q is both idle and final", ErrInvalidDefinition, def.Name, s)
		}
		o.finals[s] = struct{}{}
	}

	initials := 0
	for i, t := range def.Transitions {
		if t.To == "" {
			return nil, fmt.Errorf("%w: %s: transition %d has no target", ErrInvalidDefinition, def.Name, i)
		}
		if t.From == "" {
			if initials++; initials > 1 {
				return nil, fmt.Errorf("%w: %s: transition %d is a second initial transition", ErrInvalidDefinition, def.Name, i)
			}
			o.initial = t

			continue
		}
		if _, final := o.finals[t.From]; final {
			return nil, fmt.Errorf("%w: %s: %q is final and cannot be left (transition %d)", ErrInvalidDefinition, def.Name, t.From, i)
		}
		o.byFrom[t.From] = append(o.byFrom[t.From], t)
	}
	if initials == 0 {
		return nil, fmt.Errorf("%w: %s has no initial transition (one from \"\")", ErrInvalidDefinition, def.Name)
	}
	if _, final := o.finals[o.initial.To]; final {
		return nil, fmt.Errorf("%w: %s: the initial transition goes to %q, which is final", ErrInvalidDefinition, def.Name, o.initial.To)
	}
	for i, t := range def.Transitions {
		if !o.known(t.To) {
			return nil, fmt.Errorf("%w: %s: transition %d goes to %q, which is no state of the workflow", ErrInvalidDefinition, def.Name, i, t.To)
		}
	}

	return o, nil
}

func (o *Orchestrator[E, Tx]) terminal(s State) bool {
	_, final := o.finals[s]
	_, idle := o.idles[s]

	return final || idle
}

// Start takes in a new entity: it runs the initial transition, which has the
// Entity insert e, in the state the transition goes to, in one transaction with the
// transition's Commit. key is the ID of e. It does no more than that: whatever
// leaves the state it goes to is for Run, which the caller wakes. The result is
// Advanced in the initial transition's target.
//
// An entity with that ID that exists already is left as it is: Start loads it,
// runs nothing, and reports Existing in the state that it is in. So Start with the
// same key twice, from two requests or the same request twice, stores one entity.
// Errors of Insert and Load are returned wrapped.
func (o *Orchestrator[E, Tx]) Start(ctx context.Context, key string, e E) (Result, error) {
	if id := o.store.ID(e); id != key {
		return Result{}, fmt.Errorf("%s: starting %q: the entity has the ID %q", o.name, key, id)
	}
	if err := ctx.Err(); err != nil {
		return Result{Outcome: Waiting}, err
	}

	inserted, err := o.store.Insert(ctx, e, o.committer(ctx, e, &o.initial))
	if err != nil {
		return Result{}, fmt.Errorf("%s: starting %q: %w", o.name, key, err)
	}
	if inserted {
		return Result{State: o.initial.To, Outcome: Advanced}, nil
	}

	existing, err := o.store.Load(ctx, key)
	if err != nil {
		return Result{}, fmt.Errorf("%s: loading %q: %w", o.name, key, err)
	}

	return Result{State: o.store.Status(existing), Outcome: Existing}, nil
}

// Run loads the entity and takes it through the workflow until it is in an idle
// or final state: in each state it takes the one transition whose guard holds,
// counts an attempt if the transition says so, runs its Do and then has the Entity
// make the transition, with its Commit. It checks ctx before every Claim and Do
// and every Transition, and returns its error as it is, with Waiting. An idle
// state is where Run stops, whatever transitions leave it: only Advance takes
// those.
//
// A state with no passing guard ends the Run with NoTransition and no error, and
// one with several is ErrAmbiguousTransition.
//
// What a failure of a Claim, a Do or a Transition leads to is for the definition's
// OnError to decide, from the error and the attempts that Entity.Attempts counts
// after it:
//
//   - Retry returns the cause with Waiting and the decision's After as RetryAfter.
//   - Fail has the Entity make the transition declared from the entity's state to
//     Failed, with its Commit and, if the Entity is a CauseSetter, the cause, and
//     returns Final in the Failed state with the cause, so that the caller can tell
//     a failure that was recorded from a success. If recording it fails, the error
//     is both, with Waiting; if it loses to a peer (ErrConflict) the decision becomes
//     a Yield. A state with no such transition is ErrInvalidDefinition, and nothing
//     is written.
//   - Move has the Entity make the transition declared to the decision's To, with
//     its Commit, and carries on from there; if that loses to a peer it is a Yield.
//     A To that no transition from the state goes to is ErrInvalidDefinition, and
//     nothing is written.
//   - Yield returns Waiting with an error that wraps ErrYield and the cause.
//   - Drop returns NoTransition and no error.
//
// Both write on a context of their own that is not cancelled with ctx, for
// decisionTimeout. An unknown kind is a Retry, with an error that says so. Errors
// of Load and Transition are returned wrapped, so ErrConflict can be found with
// errors.Is.
func (o *Orchestrator[E, Tx]) Run(ctx context.Context, id string) (Result, error) {
	e, err := o.store.Load(ctx, id)
	if err != nil {
		return Result{}, fmt.Errorf("%s: loading %q: %w", o.name, id, err)
	}

	for steps := 0; ; steps++ {
		status := o.store.Status(e)
		if _, final := o.finals[status]; final {
			return Result{State: status, Outcome: Final}, nil
		}
		if _, idle := o.idles[status]; idle {
			return Result{State: status, Outcome: Idle}, nil
		}

		t, err := o.pick(e, status)
		if err != nil {
			return Result{State: status}, err
		}
		if t == nil {
			return Result{State: status, Outcome: NoTransition}, nil
		}
		if steps >= MaxTransitions {
			return Result{State: status}, fmt.Errorf("%w: %s: %q is still not at rest after %d transitions", ErrTransitionLimit, o.name, id, MaxTransitions)
		}

		next, carry, res, err := o.perform(ctx, e, t, nil, false)
		if !carry {
			return res, err
		}
		e = next
	}
}

// Advance takes an entity that waits in an idle state along the one transition
// from there to the state to whose guard holds: it loads the entity, counts an
// attempt if the transition says so, runs the transition's Do, if it has one, with
// payload, and has the Entity make the transition, with its Commit. That is all:
// the entity is not run on from the state it reaches, and the result is Advanced
// in to.
//
// An entity that is not idle, a target that no transition of its state goes to
// and a guard that does not hold are ErrNoTransition; two transitions that hold
// are ErrAmbiguousTransition. A Transition that loses to a peer is ErrConflict,
// wrapped. A failure of Do or of any other Transition is for OnError to decide as in
// Run, except that nothing is carried on: Retry and Yield return the error, Fail
// fails the entity, and Drop is NoTransition with no error. A Move is not
// something that Advance does, and is reported as an error.
func (o *Orchestrator[E, Tx]) Advance(ctx context.Context, id string, to State, payload any) (Result, error) {
	e, err := o.store.Load(ctx, id)
	if err != nil {
		return Result{}, fmt.Errorf("%s: loading %q: %w", o.name, id, err)
	}

	status := o.store.Status(e)
	if _, idle := o.idles[status]; !idle {
		return Result{State: status}, fmt.Errorf("%w: %s: %q is in %q, which is not idle", ErrNoTransition, o.name, id, status)
	}

	var t *Transition[E, Tx]
	candidates := o.byFrom[status]
	for i := range candidates {
		if candidates[i].To != to || (candidates[i].When != nil && !candidates[i].When(e)) {
			continue
		}
		if t != nil {
			return Result{State: status}, fmt.Errorf("%w: %s: %q has several transitions from %q to %q", ErrAmbiguousTransition, o.name, id, status, to)
		}
		t = &candidates[i]
	}
	if t == nil {
		return Result{State: status}, fmt.Errorf("%w: %s: %q cannot go from %q to %q", ErrNoTransition, o.name, id, status, to)
	}

	if _, carry, res, err := o.perform(ctx, e, t, payload, true); !carry {
		return res, err
	}

	return Result{State: to, Outcome: Advanced}, nil
}

// perform takes the entity along t: Claim if t counts attempts, Do, Transition.
// If that went through, carry is true and next is the entity as stored. If not,
// carry is false, and res and err are what the run ends with, as decided by OnError
// (or, for a context that ended, not decided at all: being stopped is not a failure
// of the entity). If OnError moved the entity on, carry is true and next is the
// entity as stored. advancing is for Advance, which does not carry on from a move
// and returns a conflict of its Transition as it is.
func (o *Orchestrator[E, Tx]) perform(ctx context.Context, e E, t *Transition[E, Tx], payload any, advancing bool) (next E, carry bool, res Result, err error) {
	waiting := Result{State: t.From, Outcome: Waiting}
	if err := ctx.Err(); err != nil {
		return next, false, waiting, err
	}

	if t.CountsAttempt {
		claimed, err := o.store.Claim(ctx, e)
		if err != nil {
			return o.decide(ctx, e, t, err, !advancing)
		}
		e = claimed

		if err := ctx.Err(); err != nil {
			return next, false, waiting, err
		}
	}
	if t.Do != nil {
		if err := t.Do(ctx, e, payload); err != nil {
			return o.decide(ctx, e, t, err, !advancing)
		}
	}
	if err := ctx.Err(); err != nil {
		return next, false, waiting, err
	}

	next, err = o.store.Transition(ctx, e, t.From, t.To, o.committer(ctx, e, t))
	if err != nil {
		err = fmt.Errorf("%s: moving %q from %q to %q: %w", o.name, o.store.ID(e), t.From, t.To, err)
		if advancing && errors.Is(err, ErrConflict) {
			return next, false, Result{State: t.From}, err
		}

		return o.decide(ctx, e, t, err, !advancing)
	}

	return next, true, Result{}, nil
}

// committer is the commit that the Entity runs for t in its transaction: t's Commit
// with e, or nil if t has none.
func (*Orchestrator[E, Tx]) committer(ctx context.Context, e E, t *Transition[E, Tx]) func(Tx) error {
	if t.Commit == nil {
		return nil
	}

	return func(tx Tx) error { return t.Commit(ctx, tx, e) }
}

// pick is the one transition of the entity's state whose guard holds, or nil.
func (o *Orchestrator[E, Tx]) pick(e E, status State) (*Transition[E, Tx], error) {
	var picked *Transition[E, Tx]
	candidates := o.byFrom[status]
	for i := range candidates {
		if candidates[i].When != nil && !candidates[i].When(e) {
			continue
		}
		if picked != nil {
			return nil, fmt.Errorf("%w: %s: %q has several transitions for entity %q", ErrAmbiguousTransition, o.name, status, o.store.ID(e))
		}
		picked = &candidates[i]
	}

	return picked, nil
}

// known reports whether s is a state of the definition.
func (o *Orchestrator[E, Tx]) known(s State) bool {
	_, leaves := o.byFrom[s]

	return leaves || o.terminal(s)
}

// declared is the transition that a Fail or a Move decision from from to to takes:
// the first declared one. Its guard is not asked, for that is what keeps Run from
// taking it. It is nil if the definition declares none.
func (o *Orchestrator[E, Tx]) declared(from, to State) *Transition[E, Tx] {
	candidates := o.byFrom[from]
	for i := range candidates {
		if candidates[i].To == to {
			return &candidates[i]
		}
	}

	return nil
}

// decide has the definition's OnError say what the failure of t, with the
// attempts that the entity now counts, leads to, and carries that out. If the
// entity was moved on, carry is true and moved is the entity as stored, and the
// result and error are not to be used; Run goes on with it. A decision to move
// is only carried out if canMove.
func (o *Orchestrator[E, Tx]) decide(ctx context.Context, e E, t *Transition[E, Tx], failure error, canMove bool) (moved E, carry bool, res Result, err error) {
	d := o.onError(ctx, e, *t, o.store.Attempts(e), failure)
	cause := d.Cause
	if cause == nil {
		cause = failure
	}
	id := o.store.ID(e)
	wait := Result{State: t.From, Outcome: Waiting, RetryAfter: d.After}

	switch d.Kind {
	case Retry:
		return moved, false, wait, cause
	case Yield:
		return moved, false, wait, fmt.Errorf("%w: %w", ErrYield, cause)
	case Drop:
		return moved, false, Result{State: t.From, Outcome: NoTransition}, nil
	case Fail:
		edge := o.declared(t.From, o.failed)
		if edge == nil {
			return moved, false, Result{State: t.From, Outcome: Waiting}, errors.Join(cause,
				fmt.Errorf("%w: %s: %q: no transition is declared from %q to the failed state %q", ErrInvalidDefinition, o.name, id, t.From, o.failed))
		}
		failing := e
		if setter, ok := o.store.(CauseSetter[E]); ok {
			failing = setter.WithCause(e, cause)
		}
		if _, ferr := o.apply(ctx, failing, edge); ferr != nil {
			both := errors.Join(cause, fmt.Errorf("%s: failing %q from %q: %w", o.name, id, t.From, ferr))
			if errors.Is(ferr, ErrConflict) {
				return moved, false, Result{State: t.From, Outcome: Waiting}, fmt.Errorf("%w: %w", ErrYield, both)
			}

			return moved, false, Result{State: t.From, Outcome: Waiting}, both
		}

		return moved, false, Result{State: o.failed, Outcome: Final}, cause
	case Move:
		return o.move(ctx, e, t, d, cause, canMove)
	}

	return moved, false, Result{State: t.From, Outcome: Waiting, RetryAfter: d.After}, fmt.Errorf("%s: %q: unknown decision %d: %w", o.name, id, d.Kind, cause)
}

// move carries out a decision to move the entity from t.From to d.To, along the
// transition that is declared between them.
func (o *Orchestrator[E, Tx]) move(ctx context.Context, e E, t *Transition[E, Tx], d Decision, cause error, canMove bool) (moved E, carry bool, res Result, err error) {
	id := o.store.ID(e)
	wait := Result{State: t.From, Outcome: Waiting}
	if !canMove {
		return moved, false, wait, fmt.Errorf("%w: %s: %q: OnError moved the entity during an Advance, which does not carry on: %w", ErrInvalidDefinition, o.name, id, cause)
	}
	edge := o.declared(t.From, d.To)
	if edge == nil {
		return moved, false, wait, fmt.Errorf("%w: %s: %q: OnError moved the entity to %q, but no transition from %q goes there: %w", ErrInvalidDefinition, o.name, id, d.To, t.From, cause)
	}

	moved, uerr := o.apply(ctx, e, edge)
	if uerr != nil {
		both := errors.Join(cause, fmt.Errorf("%s: moving %q from %q to %q: %w", o.name, id, t.From, d.To, uerr))
		if errors.Is(uerr, ErrConflict) {
			return moved, false, wait, fmt.Errorf("%w: %w", ErrYield, both)
		}

		return moved, false, wait, both
	}

	return moved, true, Result{}, nil
}

// apply makes the declared transition t that a decision chose, on a context that
// outlives ctx.
func (o *Orchestrator[E, Tx]) apply(ctx context.Context, e E, t *Transition[E, Tx]) (E, error) {
	decision, cancel := context.WithTimeout(context.WithoutCancel(ctx), decisionTimeout)
	defer cancel()

	return o.store.Transition(decision, e, t.From, t.To, o.committer(decision, e, t))
}
