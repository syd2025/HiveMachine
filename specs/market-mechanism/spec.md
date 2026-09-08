# Market Mechanism Specification

## Overview

The market mechanism sets dynamic prices per model+provider based on supply (available provider capacity) and demand (request volume). Prices are locked at session open and never change mid-session. The epoch duration is 1 hour (matches OpenMayhem default).

## Core Concepts

### Epoch

An epoch is a fixed time window (1 hour) during which market state is stable. At epoch boundaries, the market state is recomputed and published.

```
Epoch N:     [T0 -------- T1)    (1 hour)
Epoch N+1:   [T1 -------- T2)    (1 hour)
```

- `T0`: epoch start timestamp
- `T1`: epoch end = T0 + 3600 seconds
- Epoch number = `unix_seconds / 3600`

### Price Lock

When a user opens an inference session, the price is locked for the duration of that session (until streaming completes or timeout). The locked price is recorded in the receipt.

```
Session price = price(model, provider) at session open time
```

### Provider Min-Ask

Each provider sets a minimum price (in cents per 1M tokens) they're willing to accept for each model. The provider's routing weight is zero if the market price falls below their min-ask.

```go
type MinAsk struct {
    Model     string  // Model ID (e.g., "gpt-4o")
    MinAskCPM int64   // Minimum cents per 1M tokens
}
```

### Dynamic Pricing Formula

```
effective_demand = requests_in_epoch / capacity_in_epoch
utilization = effective_demand / total_capacity   // 0.0 to >1.0

base_price = model.base_price_cents_per_1m_tokens

# Utilization > 1.0 → premium pricing
if utilization > 1.0:
    multiplier = 1.0 + (utilization - 1.0) * price_elasticity  # elasticity = 0.5 default
else:
    multiplier = 1.0 - (1.0 - utilization) * discount_factor    # discount_factor = 0.2 default

market_price = round(base_price * multiplier, 2)  # cents per 1M tokens
```

### Market State Publication

At the end of each epoch, the gateway publishes a signed market state:

```go
type MarketState struct {
    Epoch         uint64             // Epoch number
    PublishedAt   time.Time          // Publication timestamp
    ModelPrices   map[string]int64   // model → price (cents per 1M tokens)
    ProviderCaps  map[string]int64   // providerID → available capacity for next epoch
    TotalDemand   int64              // Total requests in this epoch
    TotalCapacity int64              // Total capacity in this epoch
    Signature     []byte             // Gateway signature over the state
}
```

## Data Model

### MarketStateStore

```go
// MarketStateStore persists market states.
type MarketStateStore interface {
    Latest() (*MarketState, error)
    Save(s *MarketState) error
    ByEpoch(epoch uint64) (*MarketState, error)
}

// InMemoryMarketStateStore implements MarketStateStore.
type InMemoryMarketStateStore struct {
    mu    sync.RWMutex
    states map[uint64]*MarketState
}
```

### Provider Market Data

```go
// ProviderMarketData tracks per-provider market data.
type ProviderMarketData struct {
    ProviderID  string
    Model       string
    MinAskCPM   int64       // cents per 1M tokens (0 = no minimum)
    Capacity    int64       // requests per epoch (0 = not participating)
    ActiveSlots int64       // current in-flight requests
}
```

### Epoch Summary

```go
// EpochSummary aggregates demand/capacity for an epoch.
type EpochSummary struct {
    Epoch          uint64
    TotalRequests  int64
    TotalCapacity  int64
    AvgLatencyP50  time.Duration
    ProviderPrices map[string]int64 // providerID → avg price charged
}
```

## Epoch State Machine

```
State: IDLE
  │
  │ tick (every 1 hour on the hour)
  ▼
State: COMPUTING
  │  - Collect demand (request count per model)
  │  - Collect supply (provider capacity + min-ask)
  │  - Compute market_price per model
  │  - Generate MarketState
  │  - Sign MarketState
  ▼
State: PUBLISHED
  │  - Store MarketState
  │  - Publish to provider pub/sub channel
  ▼
State: IDLE
```

### State Transitions

```go
func (m *Market) transitionTo(state MarketState) error
func (m *Market) Start(epochInterval time.Duration)  // start tick goroutine
func (m *Market) Stop()                               // stop tick goroutine
```

## Price Lookup

```go
// PriceFor returns the market price for a model at the given time.
// If a session is active, returns the locked price.
func (m *Market) PriceFor(model string, sessionStart time.Time) (int64, error)

// LockPrice locks the current market price for a new session.
// Returns the locked price and records the lock in session metadata.
func (m *Market) LockPrice(sessionID, model string) (int64, error)
```

## Integration with Routing

The routing layer uses market prices:

```go
// routeRequest selects the best provider for a model given market prices.
func (r *Router) RouteRequest(ctx context.Context, model string, filter RouteFilter) (*Provider, error) {
    price := r.market.PriceFor(model, time.Now())

    providers := r.registry.ProvidersForModel(model)
    filtered := make([]*Provider, 0)
    for _, p := range providers {
        if p.MinAskCPM > 0 && p.MinAskCPM > price {
            continue  // below min-ask
        }
        filtered = append(filtered, p)
    }

    // Sort by reputation, return best
    sort.Slice(filtered, func(i, j int) bool {
        return filtered[i].Reputation > filtered[j].Reputation
    })
    return filtered[0], nil
}
```

## Receipt Integration

The locked price is recorded in the receipt:

```go
type Receipt struct {
    // ... existing fields ...
    LockedPrice int64   // Market price at session open (cents per 1M tokens)
    Epoch       uint64  // Epoch number at session open
}
```

## Open Questions

- **Q1**: Who sets the `base_price` for each model? (Config file, on-chain oracle, manual?)
- **Q2**: How is provider capacity measured? (In-flight requests? Token throughput? Both?)
- **Q3**: Should market state be published on-chain? (Adds censorship-resistance but latency)
- **Q4**: How to handle providers joining/leaving mid-epoch? (Immediately included/excluded in next epoch)
- **Q5**: Is there a price ceiling? (Max price per model to prevent gouging)

## Constraints

- Prices are locked at session open and never change mid-session
- Epoch tick fires on the wall-clock hour (configurable with `--epoch-interval`)
- Market state is signed by the gateway's receipt signing key
- Providers with `MinAskCPM == 0` are always willing to participate
- `price_elasticity` and `discount_factor` are configurable (default 0.5 and 0.2)
- Minimum market price is `base_price * 0.5` (50% discount floor)
- Maximum market price is `base_price * 3.0` (300% premium ceiling)
