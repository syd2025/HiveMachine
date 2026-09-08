package pricing

import (
	"sync"
	"time"
)

// QuotaLimit defines the token budget for an API key per quota period.
type QuotaLimit struct {
	APIKey        string
	Limit         int        // max tokens per period
	Period        time.Duration // reset period (e.g. 24h)
	Remaining     int        // tokens left in current period
	PeriodEnd     time.Time  // when the current period expires
}

// InMemoryQuotaStore holds quota state in memory.
// For production, replace with Redis backed store.
type InMemoryQuotaStore struct {
	mu     sync.RWMutex
	quotas map[string]QuotaLimit
}

// NewInMemoryQuotaStore creates an empty quota store.
func NewInMemoryQuotaStore() *InMemoryQuotaStore {
	return &InMemoryQuotaStore{quotas: make(map[string]QuotaLimit)}
}

// Set sets a quota limit for an API key.
func (s *InMemoryQuotaStore) Set(apiKey string, limit int, period time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.quotas[apiKey] = QuotaLimit{
		APIKey:    apiKey,
		Limit:     limit,
		Period:    period,
		Remaining: limit,
		PeriodEnd: now.Add(period),
	}
}

// Get returns the current quota for an API key.
// Returns zero quota if not set.
func (s *InMemoryQuotaStore) Get(apiKey string) QuotaLimit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quotas[apiKey]
}

// Check returns true if at least maxTokens can be deducted without exceeding the quota.
// It also returns the estimated remaining tokens after deducting.
func (s *InMemoryQuotaStore) Check(apiKey string, maxTokens int) (allowed bool, remaining int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, ok := s.quotas[apiKey]
	if !ok {
		return true, 0 // no quota configured → allow
	}

	// Reset period if expired.
	if time.Now().After(q.PeriodEnd) {
		q.Remaining = q.Limit
		q.PeriodEnd = time.Now().Add(q.Period)
	}

	if q.Remaining < maxTokens {
		return false, q.Remaining
	}
	return true, q.Remaining - maxTokens
}

// Deduct records actual token usage, decreasing remaining quota.
// If period has expired, resets first.
func (s *InMemoryQuotaStore) Deduct(apiKey string, tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, ok := s.quotas[apiKey]
	if !ok {
		return
	}

	now := time.Now()
	if now.After(q.PeriodEnd) {
		q.Remaining = q.Limit
		q.PeriodEnd = now.Add(q.Period)
	}

	q.Remaining -= tokens
	if q.Remaining < 0 {
		q.Remaining = 0
	}
	s.quotas[apiKey] = q
}

// Remaining returns the tokens left for an API key, resetting if period expired.
func (s *InMemoryQuotaStore) Remaining(apiKey string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, ok := s.quotas[apiKey]
	if !ok {
		return 0
	}

	now := time.Now()
	if now.After(q.PeriodEnd) {
		return q.Limit
	}
	return q.Remaining
}
