# Background work

Use `pkg/worker` (package `worker`) for process-local scheduling and lifecycle around work backed by durable state. The worker is not a task queue: the domain callback scans and updates durable records; PostgreSQL remains the source of truth. Do not wire a dormant worker into server startup when no real durable-work use case exists.

## Do / Don't

**Do:** persist work before waking the process worker. Make each callback safe to replay, scan durable work at startup, and return the next due time as an absolute deadline. The initial Work pass recovers work left by a stopped process. Call `Wake` only after the transaction that made new work durable commits.

**Don't:** treat Wake as a queue or job count, rely on a signal for recovery, or put business state in `worker.State`. Ready/Running/Waiting/Stopped describe only this worker instance. Durable status, attempts, per-record retry policy and idempotency belong to the owning domain and its store.

`Work` runs immediately when `Run` starts, on Wake, and at the deadline it returned on its previous pass. Every Work return replaces that deadline; a zero `time.Time` disarms it until another wake. A past deadline makes the next pass due immediately. Optional `Tick` runs at its positive configured interval and does not change the Work deadline. Work and Tick are serial and never overlap; a slow callback can delay/coalesce ticks. Callback errors and retry decisions are domain policy, not worker API results.

Give the worker the domain's actual durable scan, not a placeholder callback. Construct one worker per process lifecycle, not one per request. When work is a staged/recoverable lifecycle, persist that domain state with `pkg/workflow`; keep the definition, callbacks, store, and retry policy in the owning flat domain package.

## Own shutdown in the process composition root

The process owner starts `Run` with a child context and must cancel **and join** it before closing the database pool or any callback dependency. Cancellation is cooperative: `Run` returns nil after cancellation only when the active callback returns, so callbacks and their database/network calls must honor the context. A second or repeated `Run` on one worker instance returns `ErrAlreadyStarted`; create a new instance for a new lifecycle. Callback panics are not recovered.

Use this ownership shape when a real worker exists. The named `work` callback must be the completed domain scan/processor, and `serve` is the process's serving function; this is a callsite recipe, not a no-op worker implementation:

```go
func runProcess(ctx context.Context, pool interface{ Close() }, work func(context.Context) time.Time, serve func(context.Context) error) error {
    w, err := worker.New(worker.Config{Work: work})
    if err != nil {
        pool.Close()
        return fmt.Errorf("create worker: %w", err)
    }

    workerCtx, cancelWorker := context.WithCancel(ctx)
    workerDone := make(chan error, 1)
    go func() { workerDone <- w.Run(workerCtx) }()

    serveErr := serve(ctx)
    cancelWorker()
    workerErr := <-workerDone // join before pool.Close, including when serve failed
    pool.Close()
    if serveErr != nil {
        return serveErr
    }
    return workerErr
}
```

In the real composition root, close every callback dependency only after the same join. Ensure additional shutdown steps still cancel and wait on every return path. If callbacks ignore cancellation, Run cannot force them to stop and dependencies cannot safely be closed.

## Verify durable recovery

Test behavior through the real server and PostgreSQL conventions in [Testing](testing.md): persist work, prove a committed write wakes processing, and prove startup scan recovers work without a wake. Exercise retry deadlines and ensure one failing row does not make other durable rows disappear. Test cancellation while idle and during a callback, and ensure process shutdown joins before the pool closes. Do not assert only that Wake was called; it is deliberately lossy and coalescing.
