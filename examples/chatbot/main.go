package main

import (
	"bufio"
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
