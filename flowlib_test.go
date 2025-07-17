package flowlib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

/* ---------- helper: capture stdout ---------- */
func capture(f func()) string {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = orig
	return string(out)
}

/* ---------- concrete nodes for tests ---------- */

// Echo prints a fixed message; Params not used.
type Echo struct {
	*NodeWithRetry
	msg string
	w   io.Writer
}

func NewEcho(msg string, w io.Writer) *Echo {
	e := &Echo{NodeWithRetry: NewNode(1, 0), msg: msg, w: w}
	e.execFn = e.Exec
	return e
}
func (e *Echo) Exec(any) (any, error) {
	if e.w == nil {
		e.w = os.Stdout
	}
	fmt.Fprintln(e.w, e.msg)
	return nil, nil
}

// Flaky fails N times, then succeeds.
type Flaky struct {
	*NodeWithRetry
	failCount int32
	maxFail   int32
	w         io.Writer
}

func NewFlaky(maxFail int32, w io.Writer) *Flaky {
	f := &Flaky{NodeWithRetry: NewNode(int(maxFail)+1, 0), maxFail: maxFail, w: w}
	f.execFn = f.Exec
	return f
}

func (f *Flaky) Exec(any) (any, error) {
	if atomic.AddInt32(&f.failCount, 1) <= f.maxFail {
		return nil, errors.New("boom")
	}
	if f.w == nil {
		f.w = os.Stdout
	}
	fmt.Fprintln(f.w, "success after retries")
	return nil, nil
}

// NumbersWP processes a batch with worker-pool concurrency.
type NumbersWP struct {
	*WorkerPoolBatchNode
	counter atomic.Int32
}

func NewNumbersWP(maxPar int) *NumbersWP {
	n := &NumbersWP{WorkerPoolBatchNode: NewWorkerPoolBatchNode(1, 0, maxPar)}
	n.WorkerPoolBatchNode.asyncNode.execAsyncFn = n.ExecAsync
	n.WorkerPoolBatchNode.asyncNode.NodeWithRetry.prepFn = n.Prep
	return n
}
func (n *NumbersWP) Prep(any) (any, error) {
	return []any{1, 2, 3, 4}, nil
}
func (n *NumbersWP) ExecAsync(ctx context.Context, v any) (any, error) {
	time.Sleep(10 * time.Millisecond)
	n.counter.Add(1)
	println("item", v.(int))
	return v, nil
}

/* ---------- TESTS ---------- */

func TestSyncFlow(t *testing.T) {
	var buf bytes.Buffer
	out := capture(func() {
		a, b := NewEcho("Hello", &buf), NewEcho("World!", &buf)
		a.Then(b)
		if _, err := NewFlow(a).Run(nil); err != nil {
			t.Fatalf("sync flow error: %v", err)
		}
	})
	if want := "Hello\nWorld!\n"; buf.String() != want {
		t.Fatalf("unexpected stdout: %q", out)
	}
}

func TestRetry(t *testing.T) {
	var buf bytes.Buffer
	out := capture(func() {
		flaky := NewFlaky(2, &buf) // fail twice
		if _, err := flaky.Run(nil); err != nil {
			t.Fatalf("retry logic failed: %v", err)
		}
	})
	if !bytes.Contains(buf.Bytes(), []byte("success after retries")) {
		t.Fatalf("fallback not reached: %q", out)
	}
}

func TestWorkerPoolBatch(t *testing.T) {
	wp := NewNumbersWP(2)
	ctx := context.Background()
	res := <-NewAsyncFlow(wp).RunAsync(ctx, nil)
	if res.Err != nil {
		t.Fatalf("worker pool flow error: %v", res.Err)
	}
	if got := wp.counter.Load(); got != 4 {
		t.Fatalf("expected 4 items processed, got %d", got)
	}
}

func TestCancelAsync(t *testing.T) {
	wp := NewNumbersWP(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := <-NewAsyncFlow(wp).RunAsync(ctx, nil)
	if res.Err == nil {
		t.Fatalf("expected context cancellation error")
	}
}
func TestBatchNodeSerial(t *testing.T) {
	input := []any{1, 2, 3}
	// create a BatchNode and inject a doubling execFn
	node := NewBatchNode(1, 0)
	node.execFn = func(x any) (any, error) {
		i, ok := x.(int)
		if !ok {
			t.Fatalf("expected int, got %T", x)
		}
		return i * 2, nil
	}

	// call the internal exec method directly
	result, err := node.exec(input)
	if err != nil {
		t.Fatal(err)
	}

	// verify the result is a []any with each element doubled
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	for i, v := range arr {
		want := (i + 1) * 2
		if v.(int) != want {
			t.Errorf("at index %d: got %v, want %v", i, v, want)
		}
	}
}
func TestAsyncBatchNodeSerial(t *testing.T) {
	// we'll count how many items get processed
	var processed int32
	node := NewAsyncBatchNode(1, 0)

	// prep returns a slice of 3 ints
	node.asyncNode.prepFn = func(_ any) (any, error) {
		return []any{1, 2, 3}, nil
	}
	// execAsyncFn increments the counter
	node.asyncNode.execAsyncFn = func(ctx context.Context, v any) (any, error) {
		atomic.AddInt32(&processed, 1)
		return nil, nil
	}

	// run the node directly as an AsyncNode
	res := <-node.RunAsync(context.Background(), nil)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if processed != 3 {
		t.Fatalf("expected 3 items processed, got %d", processed)
	}
}
func TestAsyncParallelBatchNodeFanOut(t *testing.T) {
	var maxSeen atomic.Int32
	var inFlight atomic.Int32

	node := NewAsyncParallelBatchNode(1, 0)
	// prep yields 5 items
	node.asyncNode.prepFn = func(_ any) (any, error) {
		return []any{1, 2, 3, 4, 5}, nil
	}
	// execAsyncFn tracks parallelism
	node.asyncNode.execAsyncFn = func(ctx context.Context, v any) (any, error) {
		cur := inFlight.Add(1)
		if cur > maxSeen.Load() {
			maxSeen.Store(cur)
		}
		// simulate work
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return v.(int) * 2, nil
	}

	res := <-node.RunAsync(context.Background(), nil)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	out, ok := res.Output.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", res.Output)
	}
	// check correctness
	for i, v := range out {
		if v.(int) != (i+1)*2 {
			t.Errorf("index %d: got %v, want %v", i, v, (i+1)*2)
		}
	}
	// ensure we saw true parallelism
	if maxSeen.Load() <= 1 {
		t.Errorf("parallelism not observed; max in-flight was %d", maxSeen.Load())
	}
}

func TestConditionalBranching(t *testing.T) {
	var buf bytes.Buffer

	// Create nodes that will be connected with conditional branches
	nodeA := NewEcho("Node A", &buf)
	nodeB := NewEcho("Node B", &buf)
	nodeC := NewEcho("Node C", &buf)

	// Create a custom node that can return different actions
	branchNode := NewNode(1, 0)
	branchNode.execFn = func(any) (any, error) {
		return "branch_to_c", nil // This will make the flow go to nodeC
	}
	// Override postFn to return a specific action
	branchNode.baseNode.postFn = func(shared, p, e any) (Action, error) {
		return e.(string), nil
	}

	// Connect nodes with different actions
	nodeA.Then(branchNode)
	branchNode.Next("branch_to_b", nodeB)
	branchNode.Next("branch_to_c", nodeC)

	// Run the flow
	_, err := NewFlow(nodeA).Run(nil)
	if err != nil {
		t.Fatalf("flow execution failed: %v", err)
	}

	// Verify the output shows nodeA and nodeC executed, but not nodeB
	expected := "Node A\nNode C\n"
	if buf.String() != expected {
		t.Fatalf("expected output %q, got %q", expected, buf.String())
	}
}
func TestMissingSuccessorWarning(t *testing.T) {
	var buf bytes.Buffer

	// Create a node that returns a non-default action
	node := NewNode(1, 0)
	node.execFn = func(any) (any, error) {
		return "missing_action", nil
	}
	node.baseNode.postFn = func(shared, p, e any) (Action, error) {
		return e.(string), nil
	}

	// Add a successor for a different action
	node.Next("some_other_action", NewEcho("This should not run", &buf))

	// Capture the warning output
	var warnOutput string
	origWarnFn := warn
	defer func() { warn = origWarnFn }()
	warn = func(msg string, a ...any) {
		warnOutput = fmt.Sprintf(msg, a...)
	}

	// Run the flow - it should end after the first node
	_, err := NewFlow(node).Run(nil)
	if err != nil {
		t.Fatalf("flow execution failed: %v", err)
	}

	// Verify that a warning was emitted
	if !strings.Contains(warnOutput, "missing_action") {
		t.Fatalf("expected warning about missing action, got: %q", warnOutput)
	}

	// Verify that the buffer is empty (successor was not executed)
	if buf.String() != "" {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}
