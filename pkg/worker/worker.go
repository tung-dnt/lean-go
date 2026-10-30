// Package worker runs serial business callbacks under a process-owned context.
// It owns wake signals, timers and lifecycle; callbacks own durable work and retry policy.
package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// State describes execution in this process, not the state of a durable job.
type State uint32

const (
	// Ready means Run has not been called.
	Ready State = iota
	// Running means a Work or Tick callback may be executing.
	Running
	// Waiting means the worker is waiting for a wake, deadline or tick.
	Waiting
	// Stopped means Run has returned and this instance cannot run again.
	Stopped
)

// ErrAlreadyStarted prevents concurrent or repeated Run calls on one instance.
var ErrAlreadyStarted = errors.New("worker has already started")

// Config supplies business callbacks. They run serially and must honor context cancellation.
// Errors and per-job retry policy belong to the callbacks, not the worker.
type Config struct {
	// Work runs at startup, on Wake and at its returned deadline. Zero means wait
	// for Wake; a due deadline runs again immediately. Every return replaces the deadline.
	Work func(context.Context) time.Time
	// Tick runs at Interval without changing Work's deadline. Both fields may be
	// omitted. Slow callbacks can delay or coalesce ticks; ticks never overlap work.
	Tick     func(context.Context)
	Interval time.Duration
}

// Worker is constructed with New and must not be copied. Wake and State are safe
// to call concurrently with Run. The owner starts Run and joins it on shutdown.
type Worker struct {
	cfg   Config
	wake  chan struct{}
	state atomic.Uint32
}

// New requires Work and, when Tick is supplied, a positive Interval.
func New(cfg Config) (*Worker, error) {
	if cfg.Work == nil {
		return nil, errors.New("worker requires a Work callback")
	}
	if cfg.Tick != nil && cfg.Interval <= 0 {
		return nil, errors.New("worker Tick requires a positive Interval")
	}
	if cfg.Tick == nil && cfg.Interval != 0 {
		return nil, errors.New("worker Interval requires a Tick callback")
	}

	return &Worker{cfg: cfg, wake: make(chan struct{}, 1)}, nil
}

// State returns a snapshot of this worker's in-memory lifecycle.
func (w *Worker) State() State { return State(w.state.Load()) }

// Wake requests another Work pass without blocking. Pending signals coalesce,
// including signals sent before Run or during a callback. Wake is not a job queue.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled and the active callback returns, then returns
// nil. It cannot force a callback to stop. A second Run returns ErrAlreadyStarted.
// Callback panics are not recovered; cancellation is not treated as a job failure.
func (w *Worker) Run(ctx context.Context) error {
	if !w.state.CompareAndSwap(uint32(Ready), uint32(Running)) {
		return ErrAlreadyStarted
	}
	defer w.state.Store(uint32(Stopped))

	// Reuse one timer across work passes rather than allocating on every wake or tick.
	timer := time.NewTimer(0)
	timer.Stop()
	defer timer.Stop()
	var due, ticks <-chan time.Time
	if w.cfg.Tick != nil {
		ticker := time.NewTicker(w.cfg.Interval)
		defer ticker.Stop()
		ticks = ticker.C
	}

	runWork := true
	for ctx.Err() == nil {
		w.state.Store(uint32(Running))
		if runWork {
			next := w.cfg.Work(ctx)
			timer.Stop()
			due = nil
			if !next.IsZero() {
				timer.Reset(time.Until(next))
				due = timer.C
			}
		} else {
			w.cfg.Tick(ctx)
		}
		if ctx.Err() != nil {
			return nil
		}

		w.state.Store(uint32(Waiting))
		select {
		case <-ctx.Done():
			return nil
		case <-w.wake:
			runWork = true
		case <-due:
			runWork = true
		case <-ticks:
			runWork = false
		}
	}

	return nil
}
