# Phase 6: Failover & Calibration — Implementation Plan

## Current State

- `pkg/gateway/proxy/` is empty — no proxy code exists yet
- `Registry` tracks provider health via `HeartbeatLoop`; `BestProviders()` already filters by `Status != "healthy"` and `MinScore`
- `grpc.Client.pick()` is round-robin with no failover — first failure = total failure
- `ScoreConfig.MinScore` threshold already exists but is only used in `BestProviders`, not in `grpc.Client`
- No per-model latency tracking (calibration matrix)

---

## Task 19: Multi-Backend Failover

### Problem
`grpc.Client` calls go to one endpoint. If that endpoint is unhealthy or slow, the call fails immediately. There's no automatic retry against a backup provider.

### Design

**New file: `pkg/gateway/proxy/router.go`**

```go
package proxy

// Router selects providers for inference requests.
// It wraps Registry.BestProviders and adds per-request health tracking.
type Router struct {
    registry  *provider.Registry
    cfg      *provider.ScoreConfig
    attempts int // number of failover attempts per request
}

// NewRouter creates a Router.
func NewRouter(registry *provider.Registry, cfg *provider.ScoreConfig, attempts int) *Router

// Route returns the best available provider for the given model.
// Excludes providers that are unhealthy, stale, or below MinScore.
func (r *Router) Route(ctx context.Context, model string) (*provider.RoutingScore, error)

// RouteAll returns all healthy providers for a model, in score order.
// Used by callers that want to try multiple providers.
func (r *Router) RouteAll(ctx context.Context, model string) ([]provider.RoutingScore, error)
```

**Changes to `pkg/gateway/api/server.go`**

Instead of directly calling `s.client.ChatCompletions(...)`, the API server delegates to a new `proxy.Proxy`:

```go
type Proxy struct {
    router *proxy.Router
    client *grpc.Client
}

func (p *Proxy) ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
    // Try best provider first, then fallbacks
    providers, _ := p.router.RouteAll(ctx, req.GetModel())
    var lastErr error
    for _, p := range providers {
        resp, err := p.client.ChatCompletions(ctx, req) // TODO: route to specific endpoint
        if err == nil {
            return resp, nil
        }
        lastErr = err
        // Log failure to registry
    }
    return nil, lastErr
}
```

**Key decision**: The `grpc.Client` currently routes via round-robin pool. For Phase 6, we extend it to accept a `providerID` hint — if provided, only that specific provider is tried. If absent, round-robin across the pool.

### Verification
- Start gateway with one provider marked unhealthy
- Request routed to second healthy provider
- `curl` returns 200 even when primary is down

---

## Task 20: Model Calibration Matrix

### Problem
Router picks providers by composite score (uptime + latency + reputation). But latency varies per model — a provider fast for `gpt-4` might be slow for `claude-3`. We need per-model, per-provider latency tracking.

### Design

**New file: `pkg/gateway/proxy/calibration.go`**

```go
package proxy

// ModelProviderLatency tracks per-(model, provider) latency in seconds.
type ModelProviderLatency struct {
    ModelProvider string  // "gpt-4:provider-1"
    Model         string
    ProviderID    string
    P50           float64 // median latency
    P95           float64
    P99           float64
    SampleCount   int
    LastUpdated   time.Time
}

// CalibrationMatrix stores latency profiles for all (model, provider) pairs.
type CalibrationMatrix struct {
    mu sync.RWMutex
    m  map[string]*ModelProviderLatency // key: "modelID:providerID"
}

// RecordLatency adds a latency observation for a (model, provider) pair.
func (m *CalibrationMatrix) RecordLatency(model, providerID string, latencySeconds float64)

// Get returns the calibration record for a (model, provider) pair.
func (m *CalibrationMatrix) Get(model, providerID string) (*ModelProviderLatency, bool)

// BestForModel returns providers sorted by P50 latency for a given model.
func (m *CalibrationMatrix) BestForModel(model string) []string // providerIDs, sorted by P50
```

**Integration point**: After each successful `ChatCompletions` or `Completions` call, the proxy records the round-trip latency tagged with `model + providerID`. This is used in `Router.RouteAll` to weight providers by their per-model latency rather than generic score.

### Verification
- Issue 10 requests for `gpt-4` and 10 for `claude-3` to two providers
- Calibration matrix shows distinct P50 per model per provider
- Router selects lower-latency provider for each model

---

## Task 21: Endpoint Health Scoring

### Problem
`BestProviders` excludes providers below `MinScore`, but this is static. Real health should incorporate recent failure rate, consecutive failures, and time-since-last-success.

### Design

**New file: `pkg/gateway/proxy/health.go`**

```go
package proxy

// HealthScore captures why a provider got its score.
type HealthScore struct {
    ProviderID       string
    CompositeScore   float64 // [0..1]
    ConsecutiveFails int     // resets on success
    FailureRate     float64 // failures / total_requests in window
    LastFailure     time.Time
    LastSuccess     time.Time
    IsExcluded      bool    // true if below MinScore
}

// Tracker records success/failure outcomes and computes real-time health.
type Tracker struct {
    mu       sync.RWMutex
    failures map[string]int    // providerID → consecutive failures
    total    map[string]int    // providerID → total requests
    success  map[string]int    // providerID → successes
    window   time.Duration     // rolling window for failure rate
}

// RecordSuccess notes a successful request to a provider.
func (t *Tracker) RecordSuccess(providerID string)

// RecordFailure notes a failed request to a provider.
func (t *Tracker) RecordFailure(providerID string)

// Health returns the current health for a provider.
func (t *Tracker) Health(providerID string) HealthScore
```

**Integration**: `HealthTracker` is updated on every inference call (success or failure). `ProviderState.Status` in the registry is updated based on `HealthScore.IsExcluded`. The `Router` uses this instead of the static `MinScore` filter.

### Verification
- Simulate 3 consecutive failures for provider A
- Provider A's `Status` changes to `"unhealthy"` in registry
- `BestProviders` excludes provider A
- Subsequent success resets counter, provider returns to healthy

---

## Implementation Order

1. **`proxy/router.go`** — `Router`, `Route()`, `RouteAll()` pulling from existing `Registry.BestProviders()`
2. **`proxy/health.go`** — `Tracker` with success/failure recording, integrated into `Registry`
3. **`proxy/calibration.go`** — `CalibrationMatrix` with per-model latency tracking
4. **Wire into `server.go`** — replace direct `s.client` calls with `proxy.Proxy` that uses router
5. **Wire into `grpc.Client`** — add `FailoverCall(ctx, req, providers)` that tries providers in order
6. **Add tests** for each package
