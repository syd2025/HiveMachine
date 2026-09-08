package provider

import (
	"sort"
)

// RoutingScore is the composite health score used for provider selection.
type RoutingScore struct {
	ID            string
	Endpoint      string
	CompositeScore float64 // [0..1]
	Reputation    float64
	UptimeScore   float64
	LatencyScore  float64
	Status        string
}

// ScoreConfig controls how the routing score is computed.
type ScoreConfig struct {
	UptimeWeight   float64
	LatencyWeight  float64
	ReputationWeight float64
	MinScore       float64 // threshold; providers below this are excluded
}

// DefaultScoreConfig is the production routing configuration.
var DefaultScoreConfig = ScoreConfig{
	UptimeWeight:     0.40,
	LatencyWeight:    0.30,
	ReputationWeight: 0.30,
	MinScore:         0.20,
}

// ComputeRoutingScore returns a composite routing score for a provider.
// It blends the registry's Reputation (which already encodes uptime+latency)
// with a fresh latency reading for the current request.
func ComputeRoutingScore(state *ProviderState, cfg *ScoreConfig) RoutingScore {
	if cfg == nil {
		cfg = &DefaultScoreConfig
	}

	uptimeScore := computeUptimeScore(state)
	latencyScore := state.latencyScore // already an EMA in [0..1]

	composite := cfg.UptimeWeight*uptimeScore +
		cfg.LatencyWeight*latencyScore +
		cfg.ReputationWeight*state.Reputation

	return RoutingScore{
		ID:            state.ID,
		Endpoint:      state.Endpoint,
		CompositeScore: composite,
		Reputation:    state.Reputation,
		UptimeScore:   uptimeScore,
		LatencyScore:  latencyScore,
		Status:        state.Status,
	}
}

// BestProviders returns providers sorted by composite score descending.
// It excludes providers with Status != "healthy" and scores below cfg.MinScore.
func BestProviders(registry *Registry, cfg *ScoreConfig) []RoutingScore {
	providers := registry.Get()
	if len(providers) == 0 {
		return nil
	}

	if cfg == nil {
		cfg = &DefaultScoreConfig
	}

	scores := make([]RoutingScore, 0, len(providers))
	for id, state := range providers {
		// Exclude unhealthy or stale providers.
		if state.Status != StatusHealthy {
			continue
		}
		if registry.IsStale(id) {
			continue
		}

		score := ComputeRoutingScore(state, cfg)
		if score.CompositeScore < cfg.MinScore {
			continue
		}
		scores = append(scores, score)
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].CompositeScore > scores[j].CompositeScore
	})

	return scores
}
