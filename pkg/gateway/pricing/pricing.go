package pricing

import (
	"math"
)

// ModelPrice holds per-token pricing for a model+provider combination.
type ModelPrice struct {
	Model       string
	ProviderID  string
	PromptPricePer1K  float64 // cost per 1,000 prompt tokens
	CompletionPricePer1K float64 // cost per 1,000 completion tokens
}

// DefaultPrices returns a map of model ID to default pricing.
// Real deployments should load this from a database.
var DefaultPrices = map[string]ModelPrice{
	"gpt-4o": {
		Model:            "gpt-4o",
		PromptPricePer1K:   0.005,
		CompletionPricePer1K: 0.015,
	},
	"gpt-4o-mini": {
		Model:            "gpt-4o-mini",
		PromptPricePer1K:   0.00015,
		CompletionPricePer1K: 0.0006,
	},
	"claude-3-5-sonnet": {
		Model:            "claude-3-5-sonnet",
		PromptPricePer1K:   0.003,
		CompletionPricePer1K: 0.015,
	},
	"llama-3": {
		Model:            "llama-3",
		PromptPricePer1K:   0.0002,
		CompletionPricePer1K: 0.0002,
	},
	"mayhem/default": {
		Model:            "mayhem/default",
		PromptPricePer1K:   0.001,
		CompletionPricePer1K: 0.001,
	},
}

// ComputeCost calculates the cost in dollars for a given token usage and model.
func ComputeCost(promptTokens, completionTokens int, modelID string) float64 {
	price, ok := DefaultPrices[modelID]
	if !ok {
		// Fallback to mayhem/default pricing.
		price = DefaultPrices["mayhem/default"]
	}

	promptCost := float64(promptTokens) / 1000.0 * price.PromptPricePer1K
	completionCost := float64(completionTokens) / 1000.0 * price.CompletionPricePer1K
	return math.Round((promptCost+completionCost)*1e8) / 1e8 // round to 8 decimal places
}

// ComputeCostWithPrice computes cost using explicit price (for custom/synthetic models).
func ComputeCostWithPrice(promptTokens, completionTokens int, price ModelPrice) float64 {
	promptCost := float64(promptTokens) / 1000.0 * price.PromptPricePer1K
	completionCost := float64(completionTokens) / 1000.0 * price.CompletionPricePer1K
	return math.Round((promptCost+completionCost)*1e8) / 1e8
}

// InMemoryStore holds model prices in memory.
type InMemoryStore struct {
	prices map[string]ModelPrice
}

// NewInMemoryStore creates a store pre-populated with DefaultPrices.
func NewInMemoryStore() *InMemoryStore {
	m := make(map[string]ModelPrice, len(DefaultPrices))
	for k, v := range DefaultPrices {
		m[k] = v
	}
	return &InMemoryStore{prices: m}
}

// Get returns the price for a model, or false if not found.
func (s *InMemoryStore) Get(modelID string) (ModelPrice, bool) {
	p, ok := s.prices[modelID]
	return p, ok
}

// Set updates or adds a model price.
func (s *InMemoryStore) Set(modelID string, p ModelPrice) {
	s.prices[modelID] = p
}
