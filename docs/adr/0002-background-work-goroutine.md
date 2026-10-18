# 0002. Background work: one goroutine over rows in PostgreSQL

Status: accepted (inherited from the template `tung-dnt/lean-go`; proven in production by the zip ingest of meme-app, its ADR 0010)

- **Context:** Some work cannot finish inside a request (a long import, a large file to process). A task queue (backlite, River, asynq) brings a second store or a broker, its own dashboard, and an enqueue that can fail after the domain row is written, which then needs a compensating delete. The domain row usually records the work's status and attempts already.
- **Decision:**
  - The domain rows are the queue. The handler writes the row; that is all it takes to queue the work.
  - One goroutine, started and stopped by `server.Run` with its context, runs the pending rows oldest first, one at a time. The handler wakes it through a one-slot channel; it reads what to run from the database, never from the signal. `Run` waits for it before the database pool closes.
  - Its first pass after a start is the recovery: a row left half done by a process that died is run again. A bound on attempts stops a row that keeps killing the process.
  - A run is idempotent: its result and the settling of the row are written in one transaction that locks the row first, and a settled row is skipped.
  - A run that fails for a fault (a database or a third-party service that is down) waits a configured backoff before it is run again; the wait is kept in memory per row, so other rows are run meanwhile.
  - Tested end to end through the real server, restart included: `servertest.KeepSchema`, `Server.Stop`, `servertest.Sharing`.
- **Assumption:** one running instance. Two that overlap during a deploy may run the same row twice; the row lock and the settled check make that one result, at the cost of work done twice. If the app ever runs several instances for good, replace the first-pass/wake loop with a worker that claims rows with `FOR UPDATE SKIP LOCKED` and a `claimed_at` column.
- **Rejected:** a bare `go work(id)` per request (lost on every deploy, no retry, no bound on concurrency); a `SKIP LOCKED` polling worker from the start (a migration and polling for a multi-instance safety a single instance does not need); a task queue library (a second store, a non-atomic enqueue, a dashboard).
- **Consequences:** a retry's wait is lost on restart, so such a row is run at once after a start. The outcome of each piece of work is on its row, for the page that shows it; there is no separate list of failed jobs.
