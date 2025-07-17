// Package flowlib is a minimal, single-file workflow/agent runtime.
//
//  • BaseNode / NodeWithRetry
//  • BatchNode (serial map)
//  • AsyncBatchNode (serial async)
//  • AsyncParallelBatchNode (fan-out all items)
//  • NEW: WorkerPoolBatchNode (bounded goroutine pool)
//  • Flow / AsyncFlow orchestrators with branching
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
}

func newBaseNode() baseNode {
	return baseNode{params: map[string]any{}, successors: map[Action]Node{}}
}

func (b *baseNode) SetParams(p map[string]any)         { b.params = p }
func (b *baseNode) Params() map[string]any             { return b.params }
func (b *baseNode) Successors() map[Action]Node        { return b.successors }
func (b *baseNode) Next(a Action, n Node) Node         { b.successors[a] = n; return n }
func (b *baseNode) Then(n Node) Node                   { return b.Next(DefaultAction, n) }
func (b *baseNode) Run(shared any) (Action, error)     { return DefaultAction, nil } // override
func (b *baseNode) prep(shared any) (any, error)       { return nil, nil }
func (b *baseNode) exec(prepRes any) (any, error)      { return nil, nil }
func (b *baseNode) post(shared, p, e any) (Action, error) { return DefaultAction, nil }
func (b *baseNode) _run(shared any) (Action, error) {
	p, err := b.prep(shared)
	if err != nil {
		return "", err
	}
	e, err := b.exec(p)
	if err != nil {
		return "", err
	}
	return b.post(shared, p, e)
}

/* ---------- NodeWithRetry ---------- */

type NodeWithRetry struct {
	baseNode
	MaxRetries int
	Wait       time.Duration
}

func NewNode(maxRetries int, wait time.Duration) *NodeWithRetry {
	return &NodeWithRetry{baseNode: newBaseNode(), MaxRetries: maxRetries, Wait: wait}
}

func (n *NodeWithRetry) ExecFallback(prepRes any, execErr error) (any, error) {
	return nil, execErr
}

func (n *NodeWithRetry) exec(prepRes any) (any, error) {
	var res any
	var err error
	for i := 0; i < max(1, n.MaxRetries); i++ {
		res, err = n.Exec(prepRes)
		if err == nil {
			return res, nil
		}
		if i == n.MaxRetries-1 {
			return n.ExecFallback(prepRes, err)
		}
		if n.Wait > 0 {
			time.Sleep(n.Wait)
		}
	}
	return res, err
}

// Exec should be overridden by concrete node.
func (n *NodeWithRetry) Exec(any) (any, error) { return nil, errors.New("not implemented") }

/* ---------- BatchNode (serial) ---------- */

type BatchNode struct{ *NodeWithRetry }

func NewBatchNode(r int, w time.Duration) *BatchNode { return &BatchNode{NewNode(r, w)} }

func (bn *BatchNode) exec(items any) (any, error) {
	slice, ok := items.([]any)
	if !ok {
		return nil, fmt.Errorf("BatchNode expects []any, got %T", items)
	}
	out := make([]any, 0, len(slice))
	for _, v := range slice {
		res, err := bn.NodeWithRetry.exec(v)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

/* ---------- Flow orchestrator (sync) ---------- */

type Flow struct {
	baseNode
	start Node
}

func NewFlow(start Node) *Flow { return &Flow{baseNode: newBaseNode(), start: start} }

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

func (f *Flow) _orch(shared any, params map[string]any) (Action, error) {
	curr := f.start
	var last Action
	var err error
	for curr != nil {
		curr.SetParams(params)
		//last, err = curr.(*baseNode)._run(shared)
		last, err = curr.Run(shared)
		if err != nil {
			return last, err
		}
		curr = f.getNext(curr, last)
	}
	return last, err
}

func (f *Flow) Run(shared any) (Action, error) { return f._orch(shared, f.Params()) }

/* ---------- Async primitives ---------- */

type Result struct {
	Act Action
	Err error
}

type AsyncNode interface {
	Node
	RunAsync(ctx context.Context, shared any) <-chan Result
}

type asyncNode struct{ *NodeWithRetry }

func NewAsyncNode(r int, w time.Duration) *asyncNode { return &asyncNode{NewNode(r, w)} }

func (an *asyncNode) PrepAsync(ctx context.Context, s any) (any, error)  { return nil, nil }
func (an *asyncNode) ExecAsync(ctx context.Context, p any) (any, error)  { return nil, nil }
func (an *asyncNode) PostAsync(ctx context.Context, s, p, e any) (Action, error) {
	return DefaultAction, nil
}
func (an *asyncNode) ExecFallbackAsync(ctx context.Context, p any, err error) (any, error) {
	return nil, err
}

func (an *asyncNode) runAsyncInternal(ctx context.Context, p any) (any, error) {
	var res any
	var err error
	for i := 0; i < max(1, an.MaxRetries); i++ {
		res, err = an.ExecAsync(ctx, p)
		if err == nil {
			return res, nil
		}
		if i == an.MaxRetries-1 {
			return an.ExecFallbackAsync(ctx, p, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(an.Wait):
		}
	}
	return res, err
}

func (an *asyncNode) RunAsync(ctx context.Context, shared any) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		defer close(ch)
		p, err := an.PrepAsync(ctx, shared)
		if err != nil {
			ch <- Result{"", err}; return
		}
		e, err := an.runAsyncInternal(ctx, p)
		if err != nil {
			ch <- Result{"", err}; return
		}
		act, err := an.PostAsync(ctx, shared, p, e)
		ch <- Result{act, err}
	}()
	return ch
}
func (an *asyncNode) Run(any) (Action, error) { return "", errors.New("use RunAsync") }

/* ---------- AsyncBatch & AsyncParallelBatch ---------- */

type AsyncBatchNode struct{ *asyncNode }

func NewAsyncBatchNode(r int, w time.Duration) *AsyncBatchNode { return &AsyncBatchNode{NewAsyncNode(r, w)} }

func (ab *AsyncBatchNode) ExecAsync(ctx context.Context, items any) (any, error) {
	slice, ok := items.([]any)
	if !ok {
		return nil, fmt.Errorf("AsyncBatch expects []any, got %T", items)
	}
	out := make([]any, len(slice))
	for i, v := range slice {
		res, err := ab.asyncNode.runAsyncInternal(ctx, v)
		if err != nil {
			return out, err
		}
		out[i] = res
	}
	return out, nil
}

type AsyncParallelBatchNode struct{ *asyncNode }

func NewAsyncParallelBatchNode(r int, w time.Duration) *AsyncParallelBatchNode {
	return &AsyncParallelBatchNode{NewAsyncNode(r, w)}
}

func (ap *AsyncParallelBatchNode) ExecAsync(ctx context.Context, items any) (any, error) {
	slice, ok := items.([]any)
	if !ok {
		return nil, fmt.Errorf("AsyncParallelBatch expects []any, got %T", items)
	}
	out := make([]any, len(slice))
	wg := sync.WaitGroup{}
	wg.Add(len(slice))
	errCh := make(chan error, 1)
	for i, v := range slice {
		i, v := i, v
		go func() {
			defer wg.Done()
			res, err := ap.asyncNode.runAsyncInternal(ctx, v)
			if err != nil {
				select { case errCh <- err: default: }
				return
			}
			out[i] = res
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return out, err
	default:
		return out, nil
	}
}

/* ---------- NEW: WorkerPoolBatchNode ---------- */

// WorkerPoolBatchNode limits concurrency to MaxParallel goroutines,
// making it safe for huge or untrusted item lists.
type WorkerPoolBatchNode struct {
	*asyncNode
	MaxParallel int // e.g. 64
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

func (wp *WorkerPoolBatchNode) ExecAsync(ctx context.Context, items any) (any, error) {
	slice, ok := items.([]any)
	if !ok {
		return nil, fmt.Errorf("WorkerPoolBatch expects []any, got %T", items)
	}
	out := make([]any, len(slice))
	sem := make(chan struct{}, wp.MaxParallel)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for i, itm := range slice {
		i, itm := i, itm
		sem <- struct{}{} // acquire
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			res, err := wp.asyncNode.runAsyncInternal(ctx, itm)
			if err != nil {
				select { case errCh <- err: default: }
				return
			}
			out[i] = res
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return out, err
	default:
		return out, nil
	}
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
		for curr != nil {
			switch n := curr.(type) {
			case AsyncNode:
				r := <-n.RunAsync(ctx, shared)
				if r.Err != nil {
					ch <- r; return
				}
				last = r.Act
			default:
				var err error
				last, err = curr.Run(shared)
				if err != nil {
					ch <- Result{"", err}; return
				}
			}
			curr = af.getNext(curr, last)
		}
		ch <- Result{last, nil}
	}()
	return ch
}

