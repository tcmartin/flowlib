## Product Requirements Document – **Flowlib Core (Testable MVP)**

**Revision:** 1.0  **Date:** July 16 2025  **Owner:** Trevor Martin (tcmartin)

---

### 1 — Purpose

Deliver a **minimal, single‑file Go library** that provides the *essential execution engine* for agent / workflow applications:

* Nodes with `Prep → Exec → Post` lifecycle
* Retry logic + fallback
* Batch helpers (serial & concurrent)
* Flow orchestrator with branching by *action*
* Async execution with `context` + cancellation
* **Zero external dependencies**

The library must compile with **Go 1.18+** and be import‑able as `github.com/tcmartin/flowlib`.

---

### 2 — In‑scope functionality (MVP)

| Feature                    | Goal / Behaviour                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------------ |
| **BaseNode**               | Holds `params`, successor map, and template hooks.                                         |
| **NodeWithRetry**          | Retries `Exec` up to *N* times, sleeps `Wait` between attempts, then calls `ExecFallback`. |
| **BatchNode**              | Serially maps over `[]any`.                                                                |
| **AsyncNode**              | Goroutine‑based `RunAsync`, respects `ctx.Done()`.                                         |
| **AsyncParallelBatchNode** | Spawns one goroutine per item (fan‑out).                                                   |
| **WorkerPoolBatchNode**    | Same as above but with bounded pool (`MaxParallel`).                                       |
| **Flow**                   | Orchestrates nodes; branch by returned action; warns if missing successor.                 |
| **AsyncFlow**              | Same traversal but awaits `RunAsync` when node implements `AsyncNode`.                     |

---

### 3 — Out of scope (MVP)

* YAML loader, HTTP server, credential vault → deferred to **flowrunner**.
* GUI / graphical DAG builder.
* Persistence, metrics, logging interceptors.

---

### 4 — Quality goals & Test coverage

| Area                   | Accept / Reject criteria                                                      | Unit tests (current)                     |
| ---------------------- | ----------------------------------------------------------------------------- | ---------------------------------------- |
| **Sync traversal**     | Nodes execute in declared order; `Then` wires default edge.                   | `TestSyncFlow` prints `Hello World`.     |
| **Retry logic**        | `Exec` retried until success; fallback called on final failure.               | `TestRetry` fails twice then succeeds.   |
| **Worker‑pool batch**  | Processes every element exactly once; never exceeds `MaxParallel` goroutines. | `TestWorkerPoolBatch` counts 4 items.    |
| **Async cancellation** | Flow aborts with `ctx.Err()` when context deadline hits.                      | `TestCancelAsync` expects non‑nil error. |

> **Note:** Tests deliberately avoid `params` mutation to isolate engine behaviour.

---

### 5 — Milestone checklist

* [x] Single‑file library compiles (Go ≥ 1.18).
* [x] Public API: `Node`, `AsyncNode`, `Flow`, `AsyncFlow`.
* [x] Unit tests green via `go test ./...`.
* [ ] Tag `v0.1.0` after tests pass in CI.

---

### 6 — Next steps (post‑MVP)

1. **Stabilise** API, add godoc comments.
2. **flowrunner**: YAML loader + CLI harness.
3. Introduce logging interceptor & metrics hooks.
4. Optional: Parallel DAG execution (`ParallelFlow`).

---

> *Flowlib aims to stay tiny. Bigger features live in higher‑level projects.*

