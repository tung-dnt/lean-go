# Durable workflows

Use `pkg/workflow` (package `workflow`) when a domain operation has named stages and needs durable progress across retries or process restarts, waits for an external decision, or coordinates multiple effects. The package is a generic runner; it does not own domain state or persistence. Keep definitions, callbacks, and the concrete store in the owning flat `internal/<domain>` package.

## Choose the right tool

**Do:** use a workflow for a persisted lifecycle with explicit states and transitions. Use ordinary functions for pure calculation, and a handler/query or one database transaction for a single atomic mutation. A CRUD request does not become a workflow merely because it has several helper calls.

**Don't:** add an in-memory queue/checkpoint, duplicate status-switch logic, or build a sample workflow just to demonstrate the package. Model a real domain use case only when one exists.

Before implementing a qualifying workflow, identify its owner and stable key; initial, idle, failed and final states; all normal and error transitions; idempotency and retry policy; transaction and external-effect boundaries; and how crashes or unknown commit acknowledgements are recovered.

## Define the graph in the domain

Keep the definition and callbacks beside the domain that owns the entity, for example `internal/<domain>/<concern>_workflow_definition.go` and `*_workflow_<step>.go`. The graph is declarative: one initial edge (`From == ""`), named states, optional guards, Do and Commit callbacks, retry policy, and an error-decision function. It is not an event registry or handler map.

Illustrative recipe only—not an implemented domain workflow:

```go
func definition(ops *operations) workflow.Definition[*Record, pgx.Tx] {
    return workflow.Definition[*Record, pgx.Tx]{
        Name: "record lifecycle",
        Idles: []workflow.State{"awaiting-decision"},
        Finals: []workflow.State{"complete"},
        Failed: "failed",
        Transitions: []workflow.Transition[*Record, pgx.Tx]{
            {To: "ready"}, // the sole initial edge
            {From: "ready", To: "awaiting-decision", Do: ops.prepare, Commit: ops.savePrepared},
            {From: "awaiting-decision", To: "complete", Do: ops.applyDecision, Commit: ops.saveDecision},
            {From: "ready", To: "failed", When: workflow.Never[*Record], Commit: ops.recordFailure},
            {From: "awaiting-decision", To: "failed", When: workflow.Never[*Record], Commit: ops.recordFailure},
        },
    }
}
```

This recipe uses `github.com/jackc/pgx/v5`, matching the template's database stack; the domain chooses its actual transaction type. Exactly one initial edge is required and its target cannot be final. Failed and final states are terminal; idle states have outgoing edges used only by `Advance`. Every target must be a declared state. Guards for transitions from the same state must select at most one edge at runtime; ambiguity is an error. Declare error-only Fail/Move edges and guard them with `workflow.Never` so normal routing cannot take them.

## Persistence is the correctness boundary

Implement the workflow `Entity` contract with real durable storage before constructing the orchestrator. `Load` reads persisted state; `Insert` idempotently inserts in the initial state; `Claim` fences/counts an attempt; and `Transition` performs a compare-and-set against the expected state (and attempt fence where used). `Insert` and `Transition` must run the supplied Commit callback and the state write in the same transaction. A callback error rolls back all callback writes and the status update. Return a conflict when a peer changed the entity first. The runner cannot supply atomicity that the store does not implement.

A transition's `Do` runs outside the transaction and can be repeated after a failed commit, conflict, or lost acknowledgement. **Do:** make it replay-safe: use stable external idempotency keys, repeatable reads, and deterministic preparation. Put database writes in `Commit(ctx, tx, entity)` and perform them only through that transaction. Queue external side effects durably in the same commit when they cannot be included in the database transaction; a separate worker can deliver them.

**Don't:** perform non-idempotent external effects in `Do`, open a second transaction or use pool-backed queries in `Commit`, or return success from a fake/no-op store. Do not write workflow status directly from a handler or `OnError`; transition through the runner and store contract.

`Start(ctx, key, entity)` uses a stable idempotency key equal to the entity's stored ID. It inserts once; a duplicate reports `Existing` and leaves the stored entity untouched. Start commits the initial edge but does not execute its `Do` or subsequent transitions. `Run(ctx, id)` drives automatic transitions until idle/final, no eligible transition, an error decision, or its transition limit. `Advance(ctx, id, target, payload)` is for an authorized external decision: it requires an idle entity, takes exactly one matching edge, passes payload to that edge's `Do`, and does not continue running.

```go
result, err := flow.Start(ctx, record.ID, record)
if err != nil {
    return fmt.Errorf("start record workflow: %w", err)
}
if result.Outcome == workflow.Advanced {
    worker.Wake() // only after Start's durable transaction committed
}
```

This is a callsite pattern, not a sample service or fake store. At the HTTP boundary, authenticate and authorize the actor before calling `Advance`; the runner enforces graph state/edge validity, not user permissions. Validate the decision payload in the owning domain.

## Error decisions and results

`OnError` chooses a decision; it does not persist status itself. The default policy fails unretriable errors or exhausted attempts and otherwise returns Retry advice. Retry/Yield preserve the current state; Fail takes the declared edge to Failed; Move takes a declared edge and lets `Run` continue; Drop returns `NoTransition` without an error. A Move during `Advance` is invalid because Advance never continues into another transition. Fail/Move persistence uses the runner's bounded detached context; a conflict with another run yields rather than overwriting it.

Configure retry on the transition, not the definition: `Retry: workflow.RetryPolicy{MaxAttempts: 3, Backoff: time.Second}` and `CountsAttempt: true` when an attempt should be counted. The store's `Claim` persists that count before Do. A zero `MaxAttempts` fails immediately under the default policy; the illustrative graph above therefore does not retry. Counted attempts come from the entity, so a multi-stage workflow must explicitly decide whether and when its store resets or retains them.

Always inspect both `Result` and `error`. In particular, a successfully recorded Fail returns `Outcome: Final` **and the original cause error**. Retry returns `Waiting` plus the cause error and `RetryAfter` advice; Yield wraps `ErrYield`; Drop is `NoTransition` with nil error, so nil error alone does not prove success. `Start` can return `Advanced` or `Existing`; `Advance` returns `Advanced` on a successful edge. `Run` can stop at `Idle`, `Final`, `NoTransition`, or `Waiting`. Do not swallow invalid-definition, ambiguous-transition, conflict, or transition-limit errors as completion. The runner advises retry timing but does not sleep or schedule retries; the domain and its worker own scheduling.

## Test the contract

Test the actual domain store and workflow outcomes: duplicate Start, initial commit, idle Run, one-edge Advance and payload, replay after Do/Commit failure, atomic rollback of Commit writes with status, stale compare-and-set conflicts, retry exhaustion, cancellation, and recovery after process restart or an unknown commit acknowledgement. Use the repository's end-to-end PostgreSQL testing convention for persistence behavior; callback-only mocks cannot prove atomicity or recovery.
