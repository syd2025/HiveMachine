package provider

import (
	"context"
	"sync"
	"time"

	"github.com/hivemachine/internal/grpc"
)

// Status values returned by Rust core.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
)

// ProviderState holds runtime state for a provider, tracked by the Go gateway.
type ProviderState struct {
	ID            string
	Endpoint      string
	Status        string // "healthy" | "degraded" | "unhealthy"
	Reputation    float64
	Models        []string
	LastHeartbeat time.Time

	// MinAskCPM is the provider's minimum acceptable price in cents per 1M tokens
	// for each model. A value of 0 means no minimum (provider participates at any price).
	// Set via SetMinAsk or when provider registers market data.
	MinAskCPM map[string]int64 // model → min price (cents per 1M tokens)

	// ConcurrencyLimit is the max simultaneous requests the provider accepts (0 = unlimited).
	ConcurrencyLimit int32
	// ActiveRequests is the current count of in-flight requests.
	ActiveRequests int32
	// DailyBudgetCents is the max spend per day for this provider (0 = unlimited).
	DailyBudgetCents int64
	// TodaySpentCents tracks today's spend toward DailyBudgetCents.
	TodaySpentCents int64
	// AcceptRateLimit is the min acceptable accept rate (0-100); below this provider excluded.
	AcceptRateLimit float64
	// Internal scoring fields (updated by reputation.go).
	uptimeScore    float64
	latencyScore   float64
	latencyMs      int64
	consecutiveOk  int64
	totalRequests  int64
	healthyReqs    int64
}

// Registry tracks provider state and syncs with Rust core.
type Registry struct {
	mu         sync.RWMutex
	providers  map[string]*ProviderState
	grpcClient *grpc.Client
}

// NewRegistry creates a provider registry.
func NewRegistry(client *grpc.Client) *Registry {
	return &Registry{
		providers:  make(map[string]*ProviderState),
		grpcClient: client,
	}
}

// Refresh queries the Rust core for current providers and updates the registry.
func (r *Registry) Refresh(ctx context.Context) error {
	resp, err := r.grpcClient.ListProviders(ctx)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	liveIDs := make(map[string]bool)
	for _, p := range resp.Providers {
		liveIDs[p.GetId()] = true

		state, ok := r.providers[p.GetId()]
		if !ok {
			state = &ProviderState{LastHeartbeat: time.Now()}
			r.providers[p.GetId()] = state
		}

		state.ID = p.GetId()
		state.Endpoint = p.GetEndpoint()
		state.Status = p.GetStatus()
		state.Reputation = p.GetReputation()
		state.Models = p.GetModels()

		if state.Status == StatusUnhealthy {
			state.LastHeartbeat = time.Time{}
		}
	}

	for id := range r.providers {
		if !liveIDs[id] {
			delete(r.providers, id)
		}
	}

	return nil
}

// Get returns a snapshot of all providers. Returns nil if not yet loaded.
func (r *Registry) Get() map[string]*ProviderState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.providers) == 0 {
		return nil
	}

	snap := make(map[string]*ProviderState, len(r.providers))
	for k, v := range r.providers {
		snap[k] = v
	}
	return snap
}

// GetByID returns a single provider's state.
func (r *Registry) GetByID(id string) *ProviderState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[id]
}

// UpdateHeartbeat records a successful ping for a provider.
func (r *Registry) UpdateHeartbeat(id string, latencyMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.providers[id]
	if !ok {
		state = &ProviderState{ID: id, Status: StatusHealthy}
		r.providers[id] = state
	}

	state.LastHeartbeat = time.Now()
	state.latencyMs = latencyMs
	state.consecutiveOk++
	state.totalRequests++
	state.healthyReqs++

	if state.Status != StatusHealthy && state.consecutiveOk >= 3 {
		state.Status = StatusHealthy
	}
}

// UpdateFailure records a failed ping for a provider.
func (r *Registry) UpdateFailure(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.providers[id]
	if !ok {
		state = &ProviderState{ID: id, Status: StatusUnhealthy}
		r.providers[id] = state
	}

	state.consecutiveOk = 0
	state.totalRequests++

	if state.Status == StatusHealthy {
		state.Status = StatusUnhealthy
		state.LastHeartbeat = time.Now().Add(-61 * time.Second)
	}
}

// SetLatencyScore updates the latency scoring component.
func (r *Registry) SetLatencyScore(id string, score float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state, ok := r.providers[id]; ok {
		state.latencyScore = score
	}
}

// SetUptimeScore updates the uptime scoring component.
func (r *Registry) SetUptimeScore(id string, score float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state, ok := r.providers[id]; ok {
		state.uptimeScore = score
	}
}

// Count returns the number of tracked providers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}

// SetMinAsk sets the minimum ask price for a provider's model.
// A min-ask of 0 clears the minimum (provider will accept any price).
func (r *Registry) SetMinAsk(providerID, model string, minAskCPM int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	if state.MinAskCPM == nil {
		state.MinAskCPM = make(map[string]int64)
	}
	if minAskCPM == 0 {
		delete(state.MinAskCPM, model)
	} else {
		state.MinAskCPM[model] = minAskCPM
	}
}

// SetConcurrencyLimit sets the maximum concurrent requests for a provider.
// A value of 0 means unlimited.
func (r *Registry) SetConcurrencyLimit(providerID string, limit int32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	state.ConcurrencyLimit = limit
}

// SetDailyBudget sets the daily spending budget for a provider.
// A value of 0 means unlimited.
func (r *Registry) SetDailyBudget(providerID string, cents int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	state.DailyBudgetCents = cents
}

// SetAcceptRateLimit sets the minimum accept rate (0-100) for a provider.
func (r *Registry) SetAcceptRateLimit(providerID string, rate float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	state.AcceptRateLimit = rate
}

// CanAcceptRequest returns true if the provider can accept another request.
// Checks concurrency limit and daily budget.
func (r *Registry) CanAcceptRequest(providerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.providers[providerID]
	if !ok {
		return false
	}
	// Check concurrency limit.
	if state.ConcurrencyLimit > 0 && state.ActiveRequests >= state.ConcurrencyLimit {
		return false
	}
	// Check daily budget (if set).
	if state.DailyBudgetCents > 0 && state.TodaySpentCents >= state.DailyBudgetCents {
		return false
	}
	return true
}

// IncrementActiveRequests increments the active request counter.
func (r *Registry) IncrementActiveRequests(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	state.ActiveRequests++
}

// DecrementActiveRequests decrements the active request counter (never below 0).
func (r *Registry) DecrementActiveRequests(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.providers[providerID]
	if !ok {
		return
	}
	if state.ActiveRequests > 0 {
		state.ActiveRequests--
	}
}
