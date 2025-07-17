# **Flowlib – 1-file Go workflow & agent runtime**

License: MIT   |   [Docs (→ WIP)](#documentation)

---

**Flowlib** is a **single-file, dependency-free workflow/agent engine for Go**.

| 💡 Lightweight    | **≈ 350 lines** of pure Go. Drop the file in or `go get`. No third-party deps, no vendor lock-in.                              |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| 🎯 Expressive     | Nodes, conditional branches, retries, async, batch, worker-pools, *agent* & *RAG* patterns – all in the core.                  |
| ⚡ Fast            | Runs **10 k+** concurrent goroutines on a 4-core box without breaking a sweat.                                                 |
| 🧩 Composable     | Build CLI tools, servers, or embed in your own service. Layer YAML loaders, HTTP APIs, JS sandboxes – Flowlib stays unchanged. |
| 🛠️ Agentic-Ready | Pair it with your favourite LLM client: let AI *write* the YAML or Go that wires new agents together.                          |

---

## ✨ Why Flowlib?

Current workflow / agent frameworks in the Go ecosystem range from "feature-rich but heavy" to "hard-wired to one vendor".
**You rarely need 100 000+ lines to run a DAG.** Flowlib keeps the *essential abstraction* – **Graph of Nodes** – and nothing else.

| Framework   | Core Abstraction | Vendor-specific shims   | LOC (≈) | Binary size (≈) |
| ----------- | ---------------- | ----------------------- | ------- | --------------- |
| *Popular X* | Engine, Plugins  | Many (AWS, GCP, etc.)   | 55 k    | 60 MB           |
| *Popular Y* | DAG, Agents      | Many (OpenAI, Pinecone) | 12 k    | 45 MB           |
| **Flowlib** | Graph            | **None**                | **350** | **< 400 kB**    |

---

## 🚀 Quick start

```bash
# OPTION 1 – Go modules
go get github.com/tcmartin/flowlib@latest

# OPTION 2 – Copy‑paste
curl -L https://raw.githubusercontent.com/tcmartin/flowlib/main/flowlib.go \
     -o flowlib.go
```

```go
package main

import (
    "context"
    "fmt"

    "github.com/tcmartin/flowlib"
)

/*------- a tiny concrete node -------*/
type Echo struct{ *flowlib.NodeWithRetry }

func NewEcho(msg string) *Echo {
    e := &Echo{flowlib.NewNode(1, 0)}
    e.SetParams(map[string]any{"msg": msg})
    e.execFn = e.Exec
    return e
}
func (e *Echo) Exec(any) (any, error) {
    fmt.Println(e.Params()["msg"])
    return nil, nil
}

func main() {
    a, b := NewEcho("Hello"), NewEcho("World!")
    a.Then(b)                          // default edge
    flow := flowlib.NewFlow(a)         // build graph
    flow.Run(nil)                      // run synchronously

    ctx := context.Background()
    wp := flowlib.NewWorkerPoolBatchNode(3, 0, 32) // bounded‑parallel batch
    wp.asyncNode.prepFn = func(any) (any, error) {
        return []any{1, 2, 3, 4}, nil
    }
    wp.asyncNode.execAsyncFn = func(ctx context.Context, v any) (any, error) {
        fmt.Println("item", v)
        return v, nil
    }
    async := flowlib.NewAsyncFlow(wp)
    <-async.RunAsync(ctx, nil)         // await
}
```

---

## 🛠️ Core concepts (in one file)

| Concept                        | Lines | Description                                    |
| ------------------------------ | ----- | ---------------------------------------------- |
| **BaseNode**                   | <40   | `Prep → Exec → Post` template, successor map.  |
| **NodeWithRetry**              | +35   | Automatic retries, back‑off, fallback hook.    |
| **BatchNode**                  | +15   | Serial map over `[]any`.                       |
| **AsyncNode**                  | +90   | Goroutine/`context`‑based async, cancel‑safe.  |
| **AsyncParallelBatchNode**     | +25   | Full fan‑out (`goroutine per item`).           |
| **WorkerPoolBatchNode**        | +60   | Bounded pool for huge batches.                 |
| **Flow / AsyncFlow**           | +80   | Orchestrator, conditional branching by action. |
| *Helpers* (`Result`, `max`, …) | +10   | Misc utilities.                                |

---

## 🧪 Test Coverage

All core functionality is covered by comprehensive tests:

| Feature                    | Test                             | Status |
| -------------------------- | -------------------------------- | ------ |
| **Sync traversal**         | `TestSyncFlow`                   | ✅     |
| **Retry logic**            | `TestRetry`                      | ✅     |
| **BatchNode (serial)**     | `TestBatchNodeSerial`            | ✅     |
| **AsyncBatchNode**         | `TestAsyncBatchNodeSerial`       | ✅     |
| **AsyncParallelBatchNode** | `TestAsyncParallelBatchNodeFanOut` | ✅   |
| **Worker‑pool batch**      | `TestWorkerPoolBatch`            | ✅     |
| **Async cancellation**     | `TestCancelAsync`                | ✅     |
| **Conditional branching**  | `TestConditionalBranching`       | ✅     |
| **Missing successor warning** | `TestMissingSuccessorWarning` | ✅     |
| **Param propagation**      | `TestParamPropagationAndIsolation` | ✅   |

---

## 🏗️ Design patterns built with Flowlib

| Pattern      | Difficulty | Sketch                                                  |
| ------------ | ---------- | ------------------------------------------------------- |
| Simple Chat  | ☆☆☆        | Self‑loop node, `shared["history"]`.                    |
| Workflow     | ☆☆☆        | Linear chain of nodes → outline → write → style.        |
| RAG          | ☆☆☆        | `Store` node + `Retrieve` node in loop before LLM.      |
| Multi‑Agent  | ★☆☆        | Two async flows communicating via channel in `shared`.  |
| Map‑Reduce   | ★☆☆        | `WorkerPoolBatchNode` (map) → reducer node.             |
| Supervisor   | ★★☆        | Parent node calls `subFlow.Run` and branches on result. |
| Parallel DAG | ★★☆        | Split after node A, run branches concurrently, join.    |

> **Tip:** Flowlib never dictates storage, logging, or LLM vendor.
> Compose nodes with your favourite DB client, OpenAI SDK, etc.

---

## 📚 Documentation

* **API docs** – godoc (coming soon).
* **Examples** – see `examples/` folder (TBA).
* **flowrunner** – a separate project that layers YAML, HTTP, multi‑tenant creds, JS snippets, and webhooks atop Flowlib. Flowlib itself stays vanilla Go.

---

## 🤝 Community

* Open an **issue** for bugs or feature requests.
* Join the upcoming **Discord** to chat with other builders.
* PRs welcome – keep the single‑file purity for core; new ideas can live in extensions.

---

## ⭐ Acknowledgements

Flowlib's philosophy is inspired by the [Pocket Flow](https://github.com/pocket-flow/pocket-flow) 100‑line Python framework: keep the core tiny, let creativity bloom on top.

Made with ☕ and Gopher spirit.