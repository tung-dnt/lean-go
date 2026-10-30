package worker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/worker"
)

func TestWorker__WakeDuringWorkIsCoalescedAndNeverRunsConcurrently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		var calls int
		w, err := worker.New(worker.Config{Work: func(context.Context) time.Time {
			calls++
			if calls == 1 {
				<-release
			}
			return time.Time{}
		}})
		require.NoError(t, err)
		assert.Equal(t, worker.Ready, w.State())
		ended := runWorker(ctx, w)
		synctest.Wait()
		assert.Equal(t, worker.Running, w.State())
		for range 100 {
			w.Wake()
		}
		synctest.Wait()
		assert.Equal(t, 1, calls, "a wake must not start work while its previous callback is running")
		require.ErrorIs(t, w.Run(ctx), worker.ErrAlreadyStarted)

		close(release)
		synctest.Wait()
		assert.Equal(t, 2, calls, "the pending wake survives the callback but does not queue 100 runs")
		assert.Equal(t, worker.Waiting, w.State())
		cancel()
		synctest.Wait()
		require.NoError(t, <-ended)
		assert.Equal(t, worker.Stopped, w.State())
		require.ErrorIs(t, w.Run(ctx), worker.ErrAlreadyStarted)
	})
}

func TestWorker__TicksPreserveWorkDeadlineAndZeroDisarmsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var runs, ticks atomic.Int32
		w, err := worker.New(worker.Config{
			Work: func(context.Context) time.Time {
				if runs.Add(1) == 1 {
					return time.Now().Add(10 * time.Second)
				}
				return time.Time{}
			},
			Tick: func(context.Context) { ticks.Add(1) }, Interval: 3 * time.Second,
		})
		require.NoError(t, err)
		ended := runWorker(ctx, w)
		synctest.Wait()
		time.Sleep(9 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 3, ticks.Load())
		assert.EqualValues(t, 1, runs.Load(), "cleanup ticks must not run work early")
		time.Sleep(time.Second)
		synctest.Wait()
		assert.EqualValues(t, 2, runs.Load(), "cleanup ticks must not postpone the deadline")
		time.Sleep(20 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 2, runs.Load(), "a zero deadline must disarm the old timer")
		assert.EqualValues(t, 10, ticks.Load())
		cancel()
		synctest.Wait()
		require.NoError(t, <-ended)
	})
}

func TestWorker__WakeReplacesThePreviousDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var runs atomic.Int32
		w, err := worker.New(worker.Config{Work: func(context.Context) time.Time {
			switch runs.Add(1) {
			case 1:
				return time.Now().Add(10 * time.Second)
			case 2:
				return time.Now().Add(20 * time.Second)
			default:
				return time.Time{}
			}
		}})
		require.NoError(t, err)
		ended := runWorker(ctx, w)
		synctest.Wait()
		time.Sleep(time.Second)
		w.Wake()
		synctest.Wait()
		time.Sleep(9 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 2, runs.Load(), "the old startup deadline must not survive a wake")
		time.Sleep(11 * time.Second)
		synctest.Wait()
		assert.EqualValues(t, 3, runs.Load())
		cancel()
		synctest.Wait()
		require.NoError(t, <-ended)
	})
}

func TestWorker__CancellationWaitsForTheCallbackAndSkipsPendingWakeAndTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		var runs, ticks int
		w, err := worker.New(worker.Config{
			Work: func(ctx context.Context) time.Time {
				runs++
				<-ctx.Done()
				<-release
				return time.Now()
			},
			Tick: func(context.Context) { ticks++ }, Interval: time.Second,
		})
		require.NoError(t, err)
		ended := runWorker(ctx, w)
		synctest.Wait()
		time.Sleep(2 * time.Second)
		w.Wake()
		cancel()
		synctest.Wait()
		assert.Equal(t, worker.Running, w.State(), "cancellation cannot claim a callback has stopped before it returns")
		select {
		case <-ended:
			t.Fatal("Run returned while the callback was still active")
		default:
		}
		close(release)
		synctest.Wait()
		require.NoError(t, <-ended)
		assert.Equal(t, 1, runs)
		assert.Zero(t, ticks, "ticks must neither overlap work nor start after cancellation")
		assert.Equal(t, worker.Stopped, w.State())
	})
}

func TestWorker__CancelledBeforeRunDoesNotInvokeCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w, err := worker.New(worker.Config{Work: func(context.Context) time.Time {
		t.Error("cancelled worker invoked business logic")
		return time.Time{}
	}})
	require.NoError(t, err)
	w.Wake()
	require.NoError(t, w.Run(ctx))
	assert.Equal(t, worker.Stopped, w.State())
}

func TestWorker__RejectsIncompleteSchedulingConfiguration(t *testing.T) {
	work := func(context.Context) time.Time { return time.Time{} }
	for _, tc := range []struct {
		name string
		cfg  worker.Config
	}{
		{"missing work", worker.Config{}},
		{"tick without interval", worker.Config{Work: work, Tick: func(context.Context) {}}},
		{"negative interval", worker.Config{Work: work, Tick: func(context.Context) {}, Interval: -time.Second}},
		{"interval without tick", worker.Config{Work: work, Interval: time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := worker.New(tc.cfg)
			require.Error(t, err)
		})
	}
}

func runWorker(ctx context.Context, w *worker.Worker) <-chan error {
	ended := make(chan error, 1)
	go func() { ended <- w.Run(ctx) }()
	return ended
}
