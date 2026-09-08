package proxy

import (
	"sync"
	"time"

	"github.com/hivemachine/pkg/gateway/provider"
)

// HealthScore captures the real-time health state of a provider.
type HealthScore struct {
	ProviderID       string
	CompositeScore   float64 // [0..1]
	FailureRate     float64 // failures / total in rolling window
	ConsecutiveFails int
	LastFailure     time.Time
	LastSuccess     time.Time
	IsExcluded      bool // true if excluded from routing
}

// Tracker records success/failure outcomes and computes real-time health per provider.
// It drives the exclusion decisions in Router.RouteAll.
type Tracker struct {
	mu      sync.RWMutex
	failures map[string]int // providerID → consecutive failures
	total    map[string]int // providerID → total requests in window
	success  map[string]int // providerID → successful requests in window
	lastSucc map[string]time.Time
	lastFail map[string]time.Time
	window   time.Duration
}

// NewTracker creates a Tracker with the given rolling window duration.
func NewTracker(window time.Duration) *Tracker {
	return &Tracker{
		failures: make(map[string]int),
		total:    make(map[string]int),
		success:  make(map[string]int),
		lastSucc: make(map[string]time.Time),
		lastFail: make(map[string]time.Time),
		window:   window,
	}
}

// RecordSuccess notes a successful request to a provider.
func (t *Tracker) RecordSuccess(providerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.success[providerID]++
	t.total[providerID]++
	t.failures[providerID] = 0
	t.lastSucc[providerID] = time.Now()
}

// RecordFailure notes a failed request to a provider.
// Returns the provider's health score after recording.
func (t *Tracker) RecordFailure(providerID string) HealthScore {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total[providerID]++
	t.failures[providerID]++
	t.lastFail[providerID] = time.Now()

	failRate := 0.0
	if t.total[providerID] > 0 {
		failRate = float64(t.failures[providerID]) / float64(t.total[providerID])
	}
	score := t.computeScoreLocked(providerID, failRate)
	return HealthScore{
		ProviderID:       providerID,
		FailureRate:     failRate,
		ConsecutiveFails: t.failures[providerID],
		LastFailure:     t.lastFail[providerID],
		LastSuccess:     t.lastSucc[providerID],
		IsExcluded:      score < provider.DefaultScoreConfig.MinScore,
	}
}

// Health returns the current health score for a provider.
// Returns zero value if the provider has no recorded requests.
func (t *Tracker) Health(providerID string) HealthScore {
	t.mu.RLock()
	defer t.mu.RUnlock()
	failRate := 0.0
	if t.total[providerID] > 0 {
		failRate = float64(t.failures[providerID]) / float64(t.total[providerID])
	}
	score := t.computeScoreLocked(providerID, failRate)
	return HealthScore{
		ProviderID:       providerID,
		FailureRate:     failRate,
		ConsecutiveFails: t.failures[providerID],
		LastFailure:     t.lastFail[providerID],
		LastSuccess:     t.lastSucc[providerID],
		IsExcluded:      score < provider.DefaultScoreConfig.MinScore,
	}
}

// computeScoreLocked computes a health score from failure rate.
// Caller must hold t.mu.
func (t *Tracker) computeScoreLocked(providerID string, failRate float64) float64 {
	// Health score in [0..1]: 1 - failure_rate
	score := 1.0 - failRate
	return score
}

// IsExcluded returns true if the provider should be excluded from routing
// based on consecutive failure count or failure rate.
func (t *Tracker) IsExcluded(providerID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.failures[providerID] >= 3 {
		return true
	}
	failRate := 0.0
	if t.total[providerID] > 0 {
		failRate = float64(t.failures[providerID]) / float64(t.total[providerID])
	}
	return failRate >= 0.5 // excluded if ≥50% failure rate
}

// ConsecutiveFailures returns the number of consecutive failures for a provider.
func (t *Tracker) ConsecutiveFailures(providerID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.failures[providerID]
}
