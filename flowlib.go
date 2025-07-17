// Package flowlib is a minimal, single-file workflow/agent runtime.
//
//   - BaseNode / NodeWithRetry
//   - BatchNode (serial map)
//   - AsyncBatchNode (serial async)
//   - AsyncParallelBatchNode (fan-out all items)
//   - WorkerPoolBatchNode (bounded goroutine pool)
//   - Flow / AsyncFlow orchestrators with branching
//
// No external dependencies: only the Go standard library.
package flowlib

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

/* ---------- shared helpers ---------- */

type Action = string

const DefaultAction Action = "default"

func warn(msg string, a ...any) { fmt.Printf("⚠️  "+msg+"\n", a...) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

/* ---------- Node interface & base ---------- */

type Node interface {
	SetParams(map[string]any)
	Params() map[string]any
	Next(action Action, n Node) Node
	Successors() map[Action]Node
	Run(shared any) (Action, error)
}

type baseNode struct {
	params     map[string]any
	successors map[Action]Node
	prepFn     func(any) (any, error)
	postFn     func(shared, p, e any) (Action, error)
}

func newBaseNode() baseNode {
	return baseNode{
		params:     map[string]any{},
		successors: map[Action]Node{},
		prepFn:     func(any) (any, error) { return nil, nil },
		postFn:     func(shared, p, e any) (Action, error) { return DefaultAction, nil },
	}
}

func (b *baseNode) SetParams(p map[string]any)    { b.params = p }
func (b *baseNode) Params() map[string]any        { return b.params }
func (b *baseNode) Successors() map[Action]Node   { return b.successors }
func (b *baseNode) Next(a Action, n Node) Node    { b.successors[a] = n; return n }
func (b *baseNode) Then(n Node) Node              { return b.Next(DefaultAction, n) }
func (b *baseNode) prep(shared any) (any, error)  { return b.prepFn(shared) }
func (b *baseNode) exec(prepRes any) (any, error) { return nil, nil }
func (b *baseNode) post(shared, p, e any) (Action, error) {
	return b.postFn(shared, p, e)
}
func (b *baseNode) Run(shared any) (Action, error) {
	return DefaultAction, nil // overridden by NodeWithRetry
}

/* ---------- NodeWithRetry ---------- */

type NodeWithRetry struct {
	baseNode
	MaxRetries int
	Wait       time.Duration
	execFn     func(any) (any, error)
}

func NewNode(maxRetries int, wait time.Duration) *NodeWithRetry {
	return &NodeWithRetry{
		baseNode:   newBaseNode(),
		MaxRetries: maxRetries,
		Wait:       wait,
		execFn:     func(any) (any, error) { return nil, errors.New("not implemented") },
	}
}

func (n *NodeWithRetry) ExecFallback(prepRes any, execErr error) (any, error) {
	return nil, execErr
}

// Run executes Prep, then Exec with retry/backoff, then Post.
func (n *NodeWithRetry) Run(shared any) (Action, error) {
	// 1) Prep
	p, err := n.prep(shared)
	if err != nil {
		return "", err
	}
	// 2) Exec w/ retry
	var e any
	for i := 0; i < max(1, n.MaxRetries); i++ {
		e, err = n.execFn(p)
		if err == nil {
			break
		}
		if i == n.MaxRetries-1 {
			e, err = n.ExecFallback(p, err)
			break
		}
		if n.Wait > 0 {
			time.Sleep(n.Wait)
		}
	}
	if err != nil {
		return "", err
	}
	// 3) Post
	return n.post(shared, p, e)
}

/* ---------- BatchNode (serial) ---------- */

type BatchNode struct{ *NodeWithRetry }

func NewBatchNode(r int, w time.Duration) *BatchNode { return &BatchNode{NewNode(r, w)} }

func (bn *BatchNode) Run(shared any) (Action, error) {
	// 1) Prep
	p, err := bn.prep(shared)
	if err != nil {
		return "", err
	}
	// 2) Exec w/ retry
	var e any
	for i := 0; i < max(1, bn.MaxRetries); i++ {
		e, err = bn.exec(p)
		if err == nil {
			break
		}
		if i == bn.MaxRetries-1 {
			e, err = bn.ExecFallback(p, err)
			break
		}
		if bn.Wait > 0 {
			time.Sleep(bn.Wait)
		}
	}
	if err != nil {
		return "", err
	}
	// 3) Post
	return bn.post(shared, p, e)
}

func (bn *BatchNode) exec(items any) (any, error) {
	slice, ok := items.([]any)
	if !ok {
		return nil, fmt.Errorf("BatchNode expects []any, got %T", items)
	}
	out := make([]any, 0, len(slice))
	for _, v := range slice {
		res, err := bn.execFn(v)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

/* ---------- Flow orchestrator (sync) ---------- */

type Flow struct {
	start Node
}

func NewFlow(start Node) *Flow {
	return &Flow{start: start}
}

func (f *Flow) getNext(curr Node, act Action) Node {
	if act == "" {
		act = DefaultAction
	}
	nxt := curr.Successors()[act]
	if nxt == nil && len(curr.Successors()) > 0 {
		warn("Flow ends: action '%s' not found (%v)", act, curr.Successors())
	}
	return nxt
}

func (f *Flow) Run(shared any) (Action, error) {
	curr := f.start
	var last Action
	var err error
	for curr != nil {
		last, err = curr.Run(shared)
		if err != nil {
			return last, err
		}
		curr = f.getNext(curr, last)
	}
	return last, nil
}

/* ---------- Async primitives ---------- */

type Result struct {
	Act    Action
	Output any
	Err    error
}

type AsyncNode interface {
	Node
	RunAsync(ctx context.Context, shared any) <-chan Result
}

type asyncNode struct {
	*NodeWithRetry
	execAsyncFn func(context.Context, any) (any, error)
}

func NewAsyncNode(r int, w time.Duration) *asyncNode {
	return &asyncNode{
		NodeWithRetry: NewNode(r, w),
		execAsyncFn:   func(ctx context.Context, v any) (any, error) { return nil, nil },
	}
}

func (an *asyncNode) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		defer close(ch)
		// Prep
		p, err := an.prep(shared)
		if err != nil {
			ch <- Result{"", nil, err}
			return
		}
		// Exec w/ retry
		var e any
		for i := 0; i < max(1, an.MaxRetries); i++ {
			e, err = an.execAsyncFn(ctx, p)
			if err == nil {
				break
			}
			if i == an.MaxRetries-1 {
				e, err = an.ExecFallbackAsync(ctx, p, err)
				break
			}
			select {
			case <-ctx.Done():
				ch <- Result{"", nil, ctx.Err()}
				return
			case <-time.After(an.Wait):
			}
		}
		if err != nil {
			ch <- Result{"", nil, err}
			return
		}
		// Post
		act, _ := an.PostAsync(ctx, shared, p, e)
		ch <- Result{act, e, nil}
	}()
	return ch
}

func (an *asyncNode) PrepAsync(ctx context.Context, shared any) (any, error) {
	return an.prep(shared)
}
func (an *asyncNode) ExecAsync(ctx context.Context, p any) (any, error) {
	return an.execFn(p)
}
func (an *asyncNode) PostAsync(ctx context.Context, shared, p, e any) (Action, error) {
	return DefaultAction, nil
}
func (an *asyncNode) ExecFallbackAsync(ctx context.Context, p any, err error) (any, error) {
	return nil, err
}

/* ---------- Batch & ParallelBatch ---------- */

type AsyncBatchNode struct{ *asyncNode }

func NewAsyncBatchNode(r int, w time.Duration) *AsyncBatchNode {
	return &AsyncBatchNode{NewAsyncNode(r, w)}
}

// Serial
func (ab *AsyncBatchNode) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		defer close(ch)
		// 1) Prep step
		p, err := ab.prep(shared)
		if err != nil {
			ch <- Result{"", nil, err}
			return
		}
		// 2) Expect a slice
		list, ok := p.([]any)
		if !ok {
			ch <- Result{"", nil, fmt.Errorf("AsyncBatchNode expects []any, got %T", p)}
			return
		}
		// 3) Execute each item, in sequence
		for _, v := range list {
			if _, err := ab.execAsyncFn(ctx, v); err != nil {
				ch <- Result{"", nil, err}
				return
			}
		}
		// 4) All done
		ch <- Result{DefaultAction, nil, nil}
	}()
	return ch
}

type AsyncParallelBatchNode struct{ *asyncNode }

func NewAsyncParallelBatchNode(r int, w time.Duration) *AsyncParallelBatchNode {
	return &AsyncParallelBatchNode{NewAsyncNode(r, w)}
}

func (ap *AsyncParallelBatchNode) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		defer close(ch)
		// Prep
		p, err := ap.prep(shared)
		if err != nil {
			ch <- Result{"", nil, err}
			return
		}
		// Expect p to be []any
		list, ok := p.([]any)
		if !ok {
			ch <- Result{"", nil, fmt.Errorf("expected []any")}
			return
		}
		out := make([]any, len(list))
		var wg sync.WaitGroup
		errCh := make(chan error, 1)
		for i, v := range list {
			i, v := i, v
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, e := ap.execAsyncFn(ctx, v)
				if e != nil {
					select {
					case errCh <- e:
					default:
					}
					return
				}
				out[i] = res
			}()
		}
		wg.Wait()
		select {
		case e := <-errCh:
			ch <- Result{"", nil, e}
		default:
			ch <- Result{DefaultAction, out, nil}
		}
	}()
	return ch
}

/* ---------- WorkerPoolBatchNode (with cancellation) ---------- */

type WorkerPoolBatchNode struct {
	*asyncNode
	MaxParallel int
}

func NewWorkerPoolBatchNode(r int, wait time.Duration, maxPar int) *WorkerPoolBatchNode {
	if maxPar <= 0 {
		maxPar = 64
	}
	return &WorkerPoolBatchNode{
		asyncNode:   NewAsyncNode(r, wait),
		MaxParallel: maxPar,
	}
}

func (wp *WorkerPoolBatchNode) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)

	go func() {
		defer close(ch)

		// 1) Prep
		p, err := wp.prep(shared)
		if err != nil {
			ch <- Result{"", nil, err}
			return
		}
		list, ok := p.([]any)
		if !ok {
			ch <- Result{"", nil, fmt.Errorf("WorkerPoolBatch expects []any, got %T", p)}
			return
		}

		// 2) Launch worker‐pool
		sem := make(chan struct{}, wp.MaxParallel)
		var wg sync.WaitGroup
		errCh := make(chan error, 1)
		out := make([]any, len(list))

		for i, v := range list {
			// check cancellation before scheduling
			select {
			case <-ctx.Done():
				ch <- Result{"", nil, ctx.Err()}
				return
			default:
			}

			wg.Add(1)
			sem <- struct{}{}

			go func(idx int, item any) {
				defer wg.Done()
				defer func() { <-sem }()

				// perform the work (execAsyncFn honors ctx internally if implemented)
				res, e := wp.execAsyncFn(ctx, item)
				if e != nil {
					select {
					case errCh <- e:
					default:
					}
					return
				}
				out[idx] = res
			}(i, v)
		}

		// 3) Wait for all workers or cancellation
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-ctx.Done():
			ch <- Result{"", nil, ctx.Err()}
			return
		case <-done:
		}

		// 4) Check for errors
		select {
		case e := <-errCh:
			ch <- Result{"", nil, e}
		default:
			ch <- Result{DefaultAction, out, nil}
		}
	}()

	return ch
}

/* ---------- AsyncFlow ---------- */

type AsyncFlow struct{ *Flow }

func NewAsyncFlow(start Node) *AsyncFlow { return &AsyncFlow{NewFlow(start)} }

func (af *AsyncFlow) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		defer close(ch)
		curr := af.start
		var last Action
		var err error
		for curr != nil {
			if asyncNode, ok := curr.(AsyncNode); ok {
				r := <-asyncNode.RunAsync(ctx, shared)
				last, err = r.Act, r.Err
			} else {
				last, err = curr.Run(shared)
			}
			if err != nil {
				ch <- Result{"", nil, err}
				return
			}
			curr = af.getNext(curr, last)
		}
		ch <- Result{last, nil, nil}
	}()
	return ch
}
