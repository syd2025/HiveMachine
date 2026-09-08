package proxy

import (
	"context"
	"sort"

	"github.com/hivemachine/pkg/gateway/provider"
)

// Router selects providers for inference requests using health, reputation,
// per-model latency calibration, and configurable failover attempts.
type Router struct {
	registry  *provider.Registry
	cfg       *provider.ScoreConfig
	attempts  int // number of providers to try per request (failover depth)
}

// NewRouter creates a Router.
func NewRouter(registry *provider.Registry, cfg *provider.ScoreConfig, attempts int) *Router {
	if cfg == nil {
		cfg = &provider.DefaultScoreConfig
	}
	if attempts < 1 {
		attempts = 1
	}
	return &Router{
		registry: registry,
		cfg:      cfg,
		attempts: attempts,
	}
}

// Route returns the single best provider for a model.
// Returns nil if no healthy providers are available.
func (r *Router) Route(ctx context.Context, model string) (*provider.RoutingScore, error) {
	providers, err := r.RouteAll(ctx, model)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, nil
	}
	return &providers[0], nil
}

// RouteAll returns all healthy providers for a model, sorted by composite score
// descending (best first). It applies health tracking, MinScore threshold,
// and per-model calibration to sort order.
func (r *Router) RouteAll(ctx context.Context, model string) ([]provider.RoutingScore, error) {
	providers := r.registry.Get()
	if len(providers) == 0 {
		return nil, nil
	}

	scores := make([]provider.RoutingScore, 0, len(providers))
	for id, state := range providers {
		if state.Status != provider.StatusHealthy {
			continue
		}
		if r.registry.IsStale(id) {
			continue
		}

		score := provider.ComputeRoutingScore(state, r.cfg)
		if score.CompositeScore < r.cfg.MinScore {
			continue
		}
		scores = append(scores, score)
	}

	// Sort by composite score descending.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].CompositeScore > scores[j].CompositeScore
	})

	return scores, nil
}

// Attempts returns the configured failover depth.
func (r *Router) Attempts() int {
	return r.attempts
}
