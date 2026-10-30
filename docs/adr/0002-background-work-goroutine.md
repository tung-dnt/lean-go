# 0002. Background work: callbacks over durable domain rows

Status: accepted

- **Context:** Long-running work must survive request cancellation and process restart. A domain row
  can record the intent and progress without a separate broker or a non-atomic enqueue.
- **Decision:** Persist work in domain-owned PostgreSQL rows. Use `pkg/worker` for process-owned
  scheduling and `pkg/workflow` when the operation has a durable multi-step lifecycle. The template
  supplies both runtimes but does not start a worker for its synchronous note CRUD.
  - The handler commits durable intent before calling `Wake`. Wake is a coalescing hint, not job data.
  - `Work` scans durable rows at startup and after wake/deadline. Its returned absolute deadline
    schedules the next pass; zero disarms that deadline. Optional periodic `Tick` does not change it.
  - The process owner starts the runtime with its context, cancels and joins it before closing
    dependencies. One instance has one lifecycle; callbacks are serial and cancellation is cooperative.
  - The domain owns queries, ordering, retry/backoff policy, attempt limits and restart recovery.
    The worker does not prescribe an in-memory or persisted retry schedule.
  - Workflow Do callbacks are replay-safe. The domain store atomically checks the expected state,
    applies Commit writes and advances status in one transaction. External effects need idempotency
    or durable reconciliation; a database lock alone cannot make an external call exactly once.
- **Limits:** Worker serialization is local to one process, not a distributed claim. Overlapping
  instances need domain-owned claim/fencing and replay safety appropriate to the effects involved.
- **Don't:** Launch a request-owned goroutine, copy another scheduling loop, treat a wake as durable
  storage, or assume a transaction rolls back an external side effect.
- **Verification:** Package tests cover generic scheduling and transition contracts. Applications
  add real PostgreSQL tests for their store and restart tests proving durable work is recovered.

Implementation recipes: [Background work](../conventions/background-work.md) and
[Workflows](../conventions/workflow.md).
