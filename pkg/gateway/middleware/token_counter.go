package middleware

import (
	"context"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

// Usage records token consumption for a single request.
type Usage struct {
	APIKey         string
	PromptTokens   int
	CompletionTokens int
	TotalTokens    int
}

// Handler is a Gin middleware that extracts the API key and tracks token usage.
type Handler struct {
	// UsageFunc is called after a successful inference response with usage data.
	// Middleware cannot call it directly since it only sees the upstream response.
	// Instead, the API server calls RecordUsage after extracting usage from the response.
	UsageFunc func(context.Context, Usage)
}

// New creates a token counter middleware.
func New() *Handler {
	return &Handler{}
}

// ExtractAPIKey reads the Bearer token from the Authorization header.
func ExtractAPIKey(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

// RecordUsage stores token usage for an API key.
// Safe to call concurrently.
func (h *Handler) RecordUsage(ctx context.Context, u Usage) {
	if h.UsageFunc != nil {
		h.UsageFunc(ctx, u)
	}
}

// InMemoryTracker tracks token usage per API key in memory.
type InMemoryTracker struct {
	mu     sync.RWMutex
	usage  map[string]int // apiKey → total tokens
	prompt map[string]int // apiKey → prompt tokens
	comp   map[string]int // apiKey → completion tokens
}

// NewInMemoryTracker creates a tracker with empty state.
func NewInMemoryTracker() *InMemoryTracker {
	return &InMemoryTracker{
		usage:  make(map[string]int),
		prompt: make(map[string]int),
		comp:   make(map[string]int),
	}
}

// Add records token usage for an API key.
func (t *InMemoryTracker) Add(apiKey string, prompt, completion int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage[apiKey] += prompt + completion
	t.prompt[apiKey] += prompt
	t.comp[apiKey] += completion
}

// Total returns total tokens used by an API key.
func (t *InMemoryTracker) Total(apiKey string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.usage[apiKey]
}

// Breakdown returns prompt and completion tokens separately.
func (t *InMemoryTracker) Breakdown(apiKey string) (prompt, completion int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.prompt[apiKey], t.comp[apiKey]
}

// UsageForAPIKey returns all usage for a specific key.
func (t *InMemoryTracker) UsageForAPIKey(apiKey string) int {
	return t.Total(apiKey)
}

// ExtractUsageFromResponse reads token counts from the upstream usage field.
// If the header is absent or malformed, returns 0.
func ExtractUsageFromResponse(c *gin.Context) (prompt, completion, total int) {
	h := c.GetHeader("X-Usage-Prompt-Tokens")
	if h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			prompt = v
		}
	}
	h = c.GetHeader("X-Usage-Completion-Tokens")
	if h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			completion = v
		}
	}
	h = c.GetHeader("X-Usage-Total-Tokens")
	if h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			total = v
		}
	}
	return prompt, completion, total
}
