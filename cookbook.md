# Flowlib Cookbook

This cookbook contains practical examples of how to use Flowlib to build various workflow patterns.

## Table of Contents

1. [Simple Chat Bot](#simple-chat-bot)
2. [Retry with Backoff](#retry-with-backoff)
3. [Conditional Branching](#conditional-branching)
4. [Parallel Processing](#parallel-processing)
5. [Worker Pool](#worker-pool)

## Simple Chat Bot

A basic chatbot that maintains conversation history and loops back to continue the conversation.

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "strings"

    "github.com/tcmartin/flowlib"
)

// ChatNode handles a conversation loop
type ChatNode struct {
    *flowlib.NodeWithRetry
    client LLMClient
}

// LLMClient interface for any LLM service
type LLMClient interface {
    Complete(messages []Message) (string, error)
}

// Message represents a chat message
type Message struct {
    Role    string
    Content string
}

func NewChatNode(client LLMClient) *ChatNode {
    c := &ChatNode{
        NodeWithRetry: flowlib.NewNode(1, 0),
        client:        client,
    }
    c.execFn = c.Exec
    c.baseNode.prepFn = c.Prep
    c.baseNode.postFn = c.Post
    return c
}

func (c *ChatNode) Prep(shared any) (any, error) {
    // Initialize shared state if needed
    sharedMap, ok := shared.(map[string]any)
    if !ok {
        return nil, fmt.Errorf("expected shared to be map[string]any, got %T", shared)
    }

    // Initialize messages if this is the first run
    if _, exists := sharedMap["messages"]; !exists {
        sharedMap["messages"] = []Message{}
        fmt.Println("Welcome to the chat! Type 'exit' to end the conversation.")
    }

    // Get user input
    fmt.Print("\nYou: ")
    reader := bufio.NewReader(os.Stdin)
    userInput, _ := reader.ReadString('\n')
    userInput = strings.TrimSpace(userInput)

    // Check if user wants to exit
    if strings.ToLower(userInput) == "exit" {
        return nil, nil // Signal to end conversation
    }

    // Add user message to history
    messages := sharedMap["messages"].([]Message)
    messages = append(messages, Message{Role: "user", Content: userInput})
    sharedMap["messages"] = messages

    return messages, nil
}

func (c *ChatNode) Exec(p any) (any, error) {
    messages, ok := p.([]Message)
    if !ok {
        return nil, nil // End conversation
    }

    // Call LLM with the conversation history
    response, err := c.client.Complete(messages)
    if err != nil {
        return nil, err
    }

    return response, nil
}

func (c *ChatNode) Post(shared, p, e any) (flowlib.Action, error) {
    if p == nil || e == nil {
        fmt.Println("\nGoodbye!")
        return "", nil // End the conversation
    }

    sharedMap := shared.(map[string]any)
    response := e.(string)

    // Print the assistant's response
    fmt.Printf("\nAssistant: %s\n", response)

    // Add assistant message to history
    messages := sharedMap["messages"].([]Message)
    messages = append(messages, Message{Role: "assistant", Content: response})
    sharedMap["messages"] = messages

    // Loop back to continue the conversation
    return "continue", nil
}

// Simple mock LLM client for demonstration
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(messages []Message) (string, error) {
    // In a real implementation, this would call an actual LLM API
    lastMessage := messages[len(messages)-1].Content
    return fmt.Sprintf("You said: %s. This is a mock response.", lastMessage), nil
}

func main() {
    // Create the chat node
    chatNode := NewChatNode(&MockLLMClient{})
    
    // Create a self-loop for conversation continuation
    chatNode.Next("continue", chatNode)
    
    // Create and run the flow
    flow := flowlib.NewFlow(chatNode)
    shared := make(map[string]any)
    flow.Run(shared)
}
```

## Retry with Backoff

Example of a node that retries operations with exponential backoff.

```go
package main

import (
    "errors"
    "fmt"
    "time"

    "github.com/tcmartin/flowlib"
)

// FlakyServiceNode simulates a service that occasionally fails
type FlakyServiceNode struct {
    *flowlib.NodeWithRetry
    failCount int
    maxFails  int
}

func NewFlakyServiceNode(maxRetries, maxFails int) *FlakyServiceNode {
    // Set up with retry count and exponential backoff
    n := &FlakyServiceNode{
        NodeWithRetry: flowlib.NewNode(maxRetries, 100*time.Millisecond),
        maxFails:      maxFails,
    }
    n.execFn = n.Exec
    return n
}

func (n *FlakyServiceNode) Exec(any) (any, error) {
    if n.failCount < n.maxFails {
        n.failCount++
        fmt.Printf("Attempt %d failed\n", n.failCount)
        return nil, errors.New("service temporarily unavailable")
    }
    
    fmt.Println("Service call succeeded!")
    return "success data", nil
}

func main() {
    // Create a node that will fail twice then succeed
    flakyNode := NewFlakyServiceNode(3, 2)
    
    // Create and run the flow
    flow := flowlib.NewFlow(flakyNode)
    result, err := flow.Run(nil)
    
    if err != nil {
        fmt.Printf("Flow failed: %v\n", err)
    } else {
        fmt.Printf("Flow succeeded with result: %v\n", result)
    }
}
```

## Conditional Branching

Example of a flow that takes different paths based on conditions.

```go
package main

import (
    "fmt"
    "math/rand"
    "time"

    "github.com/tcmartin/flowlib"
)

// DecisionNode randomly chooses a path
type DecisionNode struct {
    *flowlib.NodeWithRetry
}

func NewDecisionNode() *DecisionNode {
    n := &DecisionNode{NodeWithRetry: flowlib.NewNode(1, 0)}
    n.execFn = n.Exec
    n.baseNode.postFn = n.Post
    return n
}

func (n *DecisionNode) Exec(any) (any, error) {
    // Simulate some decision logic
    rand.Seed(time.Now().UnixNano())
    choice := rand.Intn(3)
    return choice, nil
}

func (n *DecisionNode) Post(shared, p, e any) (flowlib.Action, error) {
    choice := e.(int)
    switch choice {
    case 0:
        return "path_a", nil
    case 1:
        return "path_b", nil
    default:
        return "path_c", nil
    }
}

// ActionNode prints which path was taken
type ActionNode struct {
    *flowlib.NodeWithRetry
    name string
}

func NewActionNode(name string) *ActionNode {
    n := &ActionNode{NodeWithRetry: flowlib.NewNode(1, 0), name: name}
    n.execFn = n.Exec
    return n
}

func (n *ActionNode) Exec(any) (any, error) {
    fmt.Printf("Executing action for path: %s\n", n.name)
    return nil, nil
}

func main() {
    // Create the nodes
    decisionNode := NewDecisionNode()
    pathA := NewActionNode("A")
    pathB := NewActionNode("B")
    pathC := NewActionNode("C")
    
    // Connect with conditional branches
    decisionNode.Next("path_a", pathA)
    decisionNode.Next("path_b", pathB)
    decisionNode.Next("path_c", pathC)
    
    // Create and run the flow
    flow := flowlib.NewFlow(decisionNode)
    flow.Run(nil)
}
```

## Parallel Processing

Example of processing items in parallel using AsyncParallelBatchNode.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/tcmartin/flowlib"
)

func main() {
    // Create a parallel batch node
    parallelNode := flowlib.NewAsyncParallelBatchNode(1, 0)
    
    // Set up the prep function to generate items
    parallelNode.asyncNode.prepFn = func(any) (any, error) {
        return []any{1, 2, 3, 4, 5}, nil
    }
    
    // Set up the exec function to process each item
    parallelNode.asyncNode.execAsyncFn = func(ctx context.Context, item any) (any, error) {
        num := item.(int)
        fmt.Printf("Processing item %d\n", num)
        time.Sleep(time.Duration(num) * 100 * time.Millisecond)
        return num * 2, nil
    }
    
    // Create and run the flow
    ctx := context.Background()
    asyncFlow := flowlib.NewAsyncFlow(parallelNode)
    result := <-asyncFlow.RunAsync(ctx, nil)
    
    if result.Err != nil {
        fmt.Printf("Flow failed: %v\n", result.Err)
    } else {
        fmt.Println("Results:", result.Output)
    }
}
```

## Worker Pool

Example of processing a large batch of items with a bounded worker pool.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/tcmartin/flowlib"
)

func main() {
    // Create a worker pool with max 3 concurrent workers
    workerPool := flowlib.NewWorkerPoolBatchNode(1, 0, 3)
    
    // Set up the prep function to generate a large batch of items
    workerPool.asyncNode.prepFn = func(any) (any, error) {
        items := make([]any, 10)
        for i := range items {
            items[i] = i + 1
        }
        return items, nil
    }
    
    // Set up the exec function to process each item
    workerPool.asyncNode.execAsyncFn = func(ctx context.Context, item any) (any, error) {
        num := item.(int)
        fmt.Printf("Worker processing item %d\n", num)
        time.Sleep(500 * time.Millisecond)
        return num * 2, nil
    }
    
    // Create and run the flow
    ctx := context.Background()
    asyncFlow := flowlib.NewAsyncFlow(workerPool)
    result := <-asyncFlow.RunAsync(ctx, nil)
    
    if result.Err != nil {
        fmt.Printf("Flow failed: %v\n", result.Err)
    } else {
        fmt.Println("All items processed successfully")
        fmt.Println("Results:", result.Output)
    }
}
```