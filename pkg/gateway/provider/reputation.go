package provider

import (
	"math"
	"time"
)

// ReputationConfig controls how the composite reputation score is computed.
type ReputationConfig struct {
	UptimeWeight  float64
	LatencyWeight float64
	LatencyP50    int64
	LatencyP95    int64
}

// DefaultReputationConfig is the production reputation tuning.
var DefaultReputationConfig = ReputationConfig{
	UptimeWeight:  0.6,
	LatencyWeight: 0.4,
	LatencyP50:    100,
	LatencyP95:    2000,
}

// ComputeReputation returns a composite score in [0..1]; higher is better.
// Returns 0 if no requests have been made yet.
func ComputeReputation(state *ProviderState, cfg *ReputationConfig) float64 {
	if cfg == nil {
		cfg = &DefaultReputationConfig
	}

	uptimeScore := computeUptimeScore(state)
	// No data at all → return 0 rather than blending zero-request signals.
	if uptimeScore == 0.0 && state.latencyMs == 0 {
		return 0.0
	}

	latencyScore := computeLatencyScore(state.latencyMs, cfg)
	score := cfg.UptimeWeight*uptimeScore + cfg.LatencyWeight*latencyScore
	return math.Round(score*10000) / 10000
}

func computeUptimeScore(state *ProviderState) float64 {
	if state.totalRequests == 0 {
		return 0.0
	}
	return float64(state.healthyReqs) / float64(state.totalRequests)
}

// computeLatencyScore returns 1 (at or below p50) down to 0 (at or above p95),
// with linear interpolation in between.
func computeLatencyScore(latencyMs int64, cfg *ReputationConfig) float64 {
	if latencyMs <= cfg.LatencyP50 {
		return 1.0
	}
	if latencyMs >= cfg.LatencyP95 {
		return 0.0
	}
	range_ := float64(cfg.LatencyP95 - cfg.LatencyP50)
	offset := float64(latencyMs - cfg.LatencyP50)
	return 1.0 - (offset / range_)
}

// RecordLatency updates the EMA latency score for a provider.
func (r *Registry) RecordLatency(id string, latencyMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.providers[id]
	if !ok {
		return
	}

	state.latencyMs = latencyMs

	raw := computeLatencyScore(latencyMs, &DefaultReputationConfig)
	if state.latencyScore == 0 {
		state.latencyScore = raw
	} else {
		state.latencyScore = 0.3*raw + 0.7*state.latencyScore
	}

	state.Reputation = ComputeReputation(state, nil)
}

// UpdateFromHeartbeat merges a successful heartbeat into a provider's scores.
func (r *Registry) UpdateFromHeartbeat(id string, latencyMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.providers[id]
	if !ok {
		return
	}

	state.latencyMs = latencyMs
	state.consecutiveOk++
	state.totalRequests++
	state.healthyReqs++
	state.LastHeartbeat = time.Now()

	rawLat := computeLatencyScore(latencyMs, &DefaultReputationConfig)
	if state.latencyScore == 0 {
		state.latencyScore = rawLat
	} else {
		state.latencyScore = 0.3*rawLat + 0.7*state.latencyScore
	}

	uptimeScore := computeUptimeScore(state)
	cfg := &DefaultReputationConfig
	state.Reputation = cfg.UptimeWeight*uptimeScore + cfg.LatencyWeight*state.latencyScore
}
