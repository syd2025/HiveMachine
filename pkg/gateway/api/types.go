package api

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ChatCompletionRequest mirrors OpenAI ChatCompletionRequest
type ChatCompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Tools       []map[string]any  `json:"tools,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatCompletionResponse mirrors OpenAI ChatCompletionResponse
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage,omitempty"`
}

// Choice represents a completion choice
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// CompletionRequest mirrors OpenAI CompletionRequest
type CompletionRequest struct {
	Model       string  `json:"model"`
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

// CompletionResponse mirrors OpenAI CompletionResponse
type CompletionResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []TextChoice `json:"choices"`
	Usage   Usage       `json:"usage,omitempty"`
}

// TextChoice represents a text completion choice
type TextChoice struct {
	Index        int    `json:"index"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// randomID generates a random ID string
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// nowUnix returns current Unix timestamp
func nowUnix() int64 {
	return time.Now().Unix()
}
