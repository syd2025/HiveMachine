package store

import (
	"context"
	"time"
)

// quotaStoreAdapter wraps a *QuotaStore to implement the gateway's quotaStoreiface.
// It handles the conversion between the simple (apiKey, maxTokens) API and the
// persisted QuotaRow API.
type quotaStoreAdapter struct {
	store *QuotaStore
}

// NewQuotaStoreAdapter wraps a *QuotaStore to satisfy the gateway quotaStoreiface.
func NewQuotaStoreAdapter(store *QuotaStore) *quotaStoreAdapter {
	return &quotaStoreAdapter{store: store}
}

// Set creates or updates a quota limit for an API key.
func (a *quotaStoreAdapter) Set(apiKey string, limit int, periodSeconds int) {
	now := time.Now()
	q := &QuotaRow{
		APIKey:    apiKey,
		Limit:     limit,
		Remaining: limit,
		PeriodEnd: now.Add(time.Duration(periodSeconds) * time.Second),
	}
	a.store.Set(context.Background(), q)
}

// Check returns (allowed, remaining) by reading the current quota state.
// It does not modify any stored values.
func (a *quotaStoreAdapter) Check(apiKey string, maxTokens int) (bool, int) {
	q, err := a.store.Get(context.Background(), apiKey)
	if err != nil || q == nil {
		return true, -1 // no quota = unlimited
	}
	if time.Now().After(q.PeriodEnd) {
		return true, q.Limit
	}
	return q.Remaining >= maxTokens, q.Remaining
}

// Deduct records token usage, resetting the period if expired.
func (a *quotaStoreAdapter) Deduct(apiKey string, tokens int) {
	a.store.Deduct(context.Background(), apiKey, tokens)
}

// Remaining returns the tokens left in the current period.
func (a *quotaStoreAdapter) Remaining(apiKey string) int {
	return a.store.Remaining(context.Background(), apiKey)
}
