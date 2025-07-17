package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tcmartin/flowlib"
)

// Message represents a chat message
type Message struct {
	Role    string
	Content string
}

// LLMClient interface for any LLM service
type LLMClient interface {
	Complete(messages []Message) (string, error)
}

// ChatNode implements the flowlib.Node interface directly
type ChatNode struct {
	params     map[string]any
	successors map[flowlib.Action]flowlib.Node
	client     LLMClient
}

func NewChatNode(client LLMClient) *ChatNode {
	return &ChatNode{
		params:     make(map[string]any),
		successors: make(map[flowlib.Action]flowlib.Node),
		client:     client,
	}
}

// Implement Node interface methods
func (c *ChatNode) SetParams(p map[string]any)                  { c.params = p }
func (c *ChatNode) Params() map[string]any                      { return c.params }
func (c *ChatNode) Successors() map[flowlib.Action]flowlib.Node { return c.successors }
func (c *ChatNode) Next(a flowlib.Action, n flowlib.Node) flowlib.Node {
	c.successors[a] = n
	return n
}

// Run implements the core chat logic
func (c *ChatNode) Run(shared any) (flowlib.Action, error) {
	// If this is the first run, initialize messages
	if _, exists := c.params["messages"]; !exists {
		c.params["messages"] = []Message{}
		fmt.Println("Welcome to the chat! Type 'exit' to end the conversation.")
	}

	// Get user input
	fmt.Print("\nYou: ")
	reader := bufio.NewReader(os.Stdin)
	userInput, _ := reader.ReadString('\n')
	userInput = strings.TrimSpace(userInput)

	// Check if user wants to exit
	if strings.ToLower(userInput) == "exit" {
		fmt.Println("\nGoodbye!")
		return "", nil // End the conversation
	}

	// Add user message to history
	messages, _ := c.params["messages"].([]Message)
	messages = append(messages, Message{Role: "user", Content: userInput})
	c.params["messages"] = messages

	// Call LLM with the conversation history
	response, err := c.client.Complete(messages)
	if err != nil {
		return "", err
	}

	// Print the assistant's response
	fmt.Printf("\nAssistant: %s\n", response)

	// Add assistant message to history
	messages = append(messages, Message{Role: "assistant", Content: response})
	c.params["messages"] = messages

	// Return the action to continue the conversation
	return "continue", nil
}

// MockLLMClient provides realistic-looking responses
type MockLLMClient struct{}

func (m *MockLLMClient) Complete(messages []Message) (string, error) {
	// Get the last user message
	lastMessage := messages[len(messages)-1].Content

	// Simulate thinking time
	time.Sleep(500 * time.Millisecond)

	// Generate a response based on the user's input
	lowercaseInput := strings.ToLower(lastMessage)

	if strings.Contains(lowercaseInput, "hello") || strings.Contains(lowercaseInput, "hi") {
		return "Hello there! How can I help you today?", nil
	} else if strings.Contains(lowercaseInput, "how are you") {
		return "I'm just a simple AI assistant, but I'm functioning well! How are you doing?", nil
	} else if strings.Contains(lowercaseInput, "name") {
		return "I'm a chatbot built with flowlib, a lightweight workflow engine for Go.", nil
	} else if strings.Contains(lowercaseInput, "weather") {
		return "I don't have access to real-time weather data, but I hope it's nice where you are!", nil
	} else if strings.Contains(lowercaseInput, "joke") {
		return "Why don't scientists trust atoms? Because they make up everything!", nil
	} else if strings.Contains(lowercaseInput, "thank") {
		return "You're welcome! Is there anything else I can help with?", nil
	} else if strings.Contains(lowercaseInput, "bye") {
		return "Goodbye! Have a great day!", nil
	} else if strings.Contains(lowercaseInput, "flowlib") {
		return "Flowlib is a single-file, dependency-free workflow/agent engine for Go. It's lightweight, expressive, and perfect for building systems like me!", nil
	} else if strings.Contains(lowercaseInput, "help") {
		return "I can chat about various topics, tell jokes, or explain what flowlib is. What would you like to know?", nil
	}

	// Default response for anything else
	return fmt.Sprintf("You said: \"%s\". That's interesting! Can you tell me more?", lastMessage), nil
}

func main() {
	// Create the chat node
	chatNode := NewChatNode(&MockLLMClient{})

	// Create a self-loop for conversation continuation
	chatNode.Next("continue", chatNode)

	// Create and run the flow
	flow := flowlib.NewFlow(chatNode)
	flow.Run(nil)
}
