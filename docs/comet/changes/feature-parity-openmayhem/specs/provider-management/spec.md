# Provider Management Specification

## Overview

Implement advanced provider management features including concurrency limits, daily budget caps, accept-rate limits, multi-model packing, and parts inventory management.

## Provider Lifecycle

```
Provider Onboarding
        │
        ▼
┌───────────────────┐
│ Registration      │ ────► KYB verification (optional)
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Hardware Probe    │ ────► Model selection
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Model Download    │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Attestation (T2+) │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Heartbeat Loop    │ ◄───► Route Advertisement
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Serving           │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│ Graceful Drain    │ ────► Earnings Settlement
└───────────────────┘
```

## Data Structures

### Provider Entry

```go
// ProviderKey uniquely identifies a provider+enclave+room
type ProviderKey struct {
    Provider  string  // Provider public key hash
    EnclaveID string  // Enclave ID
    RoomID    string  // Market/room ID
}

// ProviderEntry in the provider table
type ProviderEntry struct {
    Key              ProviderKey
    Contract         ProviderSnapshot  // From contract
    Heartbeat        *ProviderHeartbeat
    HeartbeatAgeMs   int64
    Observed         ProviderMetrics   // EWMA metrics
    AttestationHead  []byte            // Latest attestation hash
}

// ProviderSnapshot from contract
type ProviderSnapshot struct {
    Provider       string
    EnclaveID      string
    ModelID        string
    RoomID         string
    ConsentVersion int
    Reputation     float64
    PriceVersion   int
    RateMap        RateMap
    MinSessionAU   int64
    Capabilities   ProviderCapabilities
    AttestationHead []byte
}

// ProviderCapabilities
type ProviderCapabilities struct {
    MaxContext      int64
    Quantizations   []string  // "fp16", "int4", "nvfp4", etc.
    Modalities      []string  // "text", "vision", "audio"
    ExecutionModes  []string  // "baseline", "prefix-cache", "parallel-dispatch"
}
```

### Provider Metrics (EWMA)

```go
// ProviderMetrics for performance tracking
type ProviderMetrics struct {
    EWMA_TTFT_MS    float64  // Exponential weighted moving average TTFT
    EWMA_TokPerSec  float64  // Throughput
    EWMA_ErrorRate  float64  // Error rate
    
    // Circuit breaking
    ConsecutiveFailures     int
    UnderdeliveryStreak     int
    CapacityMismatchStreak  int
    CircuitOpenUntilMs      int64
    
    // Samples
    SampleCount  int
    LastUpdated  time.Time
}

// Circuit breaker thresholds
const (
    MaxConsecutiveFailures = 3
    MaxFailureRate = 0.5  // 50%
    CircuitOpenDuration = 30 * time.Second
)
```

### Rate Limiting

```go
// RateMapEntry for pricing rate limiting
type RateMapEntry struct {
    Unit         RateUnit    // Billing unit
    PerUnitAU    int64       // Price per unit in au
    Granularity  int         // Billing granularity
}

// RateUnit
type RateUnit string

const (
    RateUnitInputToken         RateUnit = "INPUT_TOKEN_UNIT"
    RateUnitOutputToken        RateUnit = "OUTPUT_TOKEN_UNIT"
    RateUnitCachedInputToken   RateUnit = "CACHED_INPUT_TOKEN_UNIT"
    RateUnitUsageStep          RateUnit = "USAGE_STEP"
    RateUnitUsageFrame         RateUnit = "USAGE_FRAME"  // Video frames
)

// Rate limits for provider participation
type ProviderRateLimits struct {
    MaxConcurrent   int           // Max concurrent sessions
    AcceptRate      RateLimit     // Sessions per minute
    DailyBudgetAU   int64         // Max daily spend in au
    DailySpendAU    int64         // Current daily spend
    DailyBudgetReset time.Time    // Next reset
}

// RateLimit definition
type RateLimit struct {
    Count   int
    Window  time.Duration
}

// Check if provider can accept
func (p *ProviderRateLimits) CanAccept() bool {
    if p.DailySpendAU >= p.DailyBudgetAU {
        return false
    }
    // Check accept rate limit
    return true
}
```

### Provider Selection

```go
// SelectionCandidate for route selection
type SelectionCandidate struct {
    Entry           *ProviderEntry
    EstimatedPriceAU int64   // Total estimated price
    EffectiveTTFTMs float64  // Estimated TTFT
    LatencyFactor   float64  // Normalized latency
    PriceNorm       float64  // Normalized price
    ThroughputRatio float64  // Relative throughput
    FreeSlots       int      // Available slots
    EngineBacklog   int      // Pending requests
}

// SelectionWeights for P2C algorithm
type SelectionWeights struct {
    ReputationAlpha float64  // Weight for reputation (default: 1.0)
    SaturationBeta  float64  // Weight for saturation (default: 0.5)
    PriceGamma      float64  // Weight for price (default: 0.3)
}

// P2C (Power of Two Choices) Selection
func SelectProvider(candidates []SelectionCandidate, weights SelectionWeights) *SelectionCandidate {
    if len(candidates) == 0 {
        return nil
    }
    if len(candidates) == 1 {
        return &candidates[0]
    }
    
    // Pick two random candidates
    a := candidates[rand.Intn(len(candidates))]
    b := candidates[rand.Intn(len(candidates))]
    
    // Score each
    scoreA := scoreCandidate(a, weights)
    scoreB := scoreCandidate(b, weights)
    
    if scoreA > scoreB {
        return &a
    }
    return &b
}

func scoreCandidate(c SelectionCandidate, w SelectionWeights) float64 {
    return w.ReputationAlpha*c.Entry.Contract.Reputation +
           w.SaturationBeta*(1-c.LatencyFactor) +
           w.PriceGamma*(1-c.PriceNorm)
}
```

## Limit Controls

### Provider Limits Configuration

```go
// ProviderLimits for a provider or specific enclave
type ProviderLimits struct {
    ProviderID     string
    
    // Global limits
    MaxConcurrent  int
    AcceptRate     RateLimit  // e.g., 30 per minute
    DailyBudgetAU  int64      // in au
    
    // Per-enclave limits (optional)
    EnclaveLimits  map[string]EnclaveLimits
}

// EnclaveLimits for specific model/enclave
type EnclaveLimits struct {
    EnclaveID      string
    MaxConcurrent  int
    DailyBudgetAU  int64
}

// CLI commands
/*
mayhem provider limits set --max-concurrent 4 --accept-rate 30/min --budget 5000000000000/day
mayhem provider limits set --enclave <model> --max-concurrent 1 --budget 1000000000000/day
*/
```

### Enforcement

```go
// LimitEnforcer checks and enforces provider limits
type LimitEnforcer struct {
    limits       *ProviderLimits
    acceptCount  int           // Current minute accepts
    acceptWindow time.Time     // Window start
    
    mu           sync.Mutex
}

func (e *LimitEnforcer) TryAccept() error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    // Check daily budget
    if e.limits.DailyBudgetAU > 0 && e.limits.DailySpendAU >= e.limits.DailyBudgetAU {
        return ErrDailyBudgetExceeded
    }
    
    // Check concurrent limit
    if e.limits.MaxConcurrent > 0 && e.currentConcurrent >= e.limits.MaxConcurrent {
        return ErrConcurrentLimitExceeded
    }
    
    // Check accept rate
    if e.shouldRateLimit() {
        return ErrAcceptRateExceeded
    }
    
    e.acceptCount++
    return nil
}

func (e *LimitEnforcer) RecordSpend(amountAU int64) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.limits.DailySpendAU += amountAU
}
```

## Multi-Model Packing

### Memory-Based Packing

```go
// ModelRequirements for packing calculation
type ModelRequirements struct {
    ModelID        string
    ContextSize    int64     // Max context in tokens
    MemoryMB       int64     // VRAM required
    MinMemoryMB    int64     // Minimum for partial loading
    Quantization   string    // "q4_k_m", "q8_0", etc.
}

// PackingPlan for multi-model serving
type PackingPlan struct {
    Models   []PackedModel
    TotalMemoryMB int64
    Fits       bool
}

// PackedModel in a plan
type PackedModel struct {
    ModelID       string
    Quantization  string
    MemoryMB      int64
    ContextSize   int64
    SlotIndex     int
}

// Calculate packing for available memory
func CalculatePacking(availableMB int64, models []ModelRequirements) *PackingPlan {
    // Sort by memory efficiency
    sort.Slice(models, func(i, j int) bool {
        return models[i].MemoryMB < models[j].MemoryMB
    })
    
    plan := &PackingPlan{
        Models: make([]PackedModel, 0),
    }
    
    for _, model := range models {
        if plan.TotalMemoryMB+model.MemoryMB <= availableMB {
            plan.Models = append(plan.Models, PackedModel{
                ModelID:      model.ModelID,
                MemoryMB:     model.MemoryMB,
                ContextSize:  model.ContextSize,
            })
            plan.TotalMemoryMB += model.MemoryMB
        }
    }
    
    plan.Fits = len(plan.Models) > 0
    return plan
}

/*
mayhem provider serve plan
mayhem provider serve add <enclave-id>
mayhem provider serve remove <enclave-id>
*/
```

## Parts Inventory

### Signed Parts Management

```go
// PartRecord from signed parts index
type PartRecord struct {
    PartID      string  // BLAKE3 hash
    FileName    string
    FileSize    int64
    SHA256      string  // For verification
    Source      string  // Hugging Face path
}

// PartsInventory for a provider
type PartsInventory struct {
    ProviderID     string
    PartsRoot      string  // Local parts directory
    Parts          map[string]*PartRecord
    InventoryRoot  string  // Merkle root of all parts
    LastSync       time.Time
}

// Sync parts from signed parts index
func (inv *PartsInventory) Sync(ctx context.Context) error {
    // Fetch latest parts index from ledger
    index, err := fetchPartsIndex(ctx)
    if err != nil {
        return err
    }
    
    // Download missing parts
    for _, part := range index.Parts {
        if _, ok := inv.Parts[part.PartID]; !ok {
            if err := inv.DownloadPart(ctx, part); err != nil {
                return err
            }
        }
    }
    
    // Verify all parts
    if err := inv.VerifyParts(); err != nil {
        return err
    }
    
    // Update inventory root
    inv.InventoryRoot = calculateMerkleRoot(inv.Parts)
    return nil
}

/*
mayhem provider parts pull     # Download all parts
mayhem provider parts add      # Add specific part
mayhem provider parts admit    # Admit parts for serving
*/
```

## Provider Commands

### CLI Reference

```bash
# Lifecycle
mayhem up --provider              # Start as provider
mayhem provider list              # List served enclaves
mayhem provider drain             # Graceful shutdown
mayhem provider drain --enclave <id>  # Drain specific enclave

# Configuration
mayhem provider limits set --max-concurrent 4 --accept-rate 30/min --budget 5000000000000/day
mayhem provider limits get
mayhem provider min-ask set <model:T1> 120000

# Hardware
mayhem provider serve plan       # Show packing plan
mayhem provider serve add <enclave-id>
mayhem provider serve remove <enclave-id>

# Parts
mayhem provider parts pull
mayhem provider parts list
mayhem provider parts admit --write

# Health
mayhem provider health           # Show route status
mayhem reputation                # Show provider reputation
```

## Acceptance Criteria

- **A17.1**: Provider can set concurrency/daily budget limits
- **A17.2**: Requests rejected when limits exceeded
- **A18.1**: Accept-rate limits enforced (30/min default)
- **A18.2**: Refusals from limits don't damage reputation
- **A19.1**: Multi-model packing respects memory budget
- **A19.2**: Packing plan shows all fitting models
- **A19.3**: Parts inventory syncs from signed index
- **A19.4**: Missing parts are downloaded on demand

## Route State Transitions

```
                    ┌─────────────────────┐
                    │                     │
                    ▼                     │
              ┌──────────┐                │
   Start ───► │  Live    │◄──────┐        │
              └──────────┘       │        │
                  │              │        │
        ┌─────────┼─────────┐    │        │
        ▼         ▼         ▼    │        │
   ┌────────┐ ┌────────┐ ┌────────────┐  │
   │Circuit │ │Draining│ │AtCapacity  │──┘
   │Open    │ │        │ │            │   (resolve)
   └────────┘ └────────┘ └────────────┘
        │         │         │
        └─────────┴─────────┘
              (resolve)
```

## P2C Selection Algorithm

```go
// Full P2C implementation
func SelectProviderP2C(
    candidates []SelectionCandidate,
    weights SelectionWeights,
) *SelectionCandidate {
    if len(candidates) == 0 {
        return nil
    }
    
    // Filter to only live routes
    live := filterLive(candidates)
    if len(live) == 0 {
        return nil
    }
    
    // Power of Two Choices
    i, j := twoRandomIndices(len(live))
    a := live[i]
    b := live[j]
    
    // Calculate scores
    scoreA := calculateScore(a, weights)
    scoreB := calculateScore(b, weights)
    
    if scoreA >= scoreB {
        return &a
    }
    return &b
}

func calculateScore(c SelectionCandidate, w SelectionWeights) float64 {
    // Normalize factors to 0-1 range
    latencyFactor := 1.0 - min(1.0, c.EffectiveTTFTMs/5000.0)  // 5s baseline
    priceNorm := min(1.0, float64(c.EstimatedPriceAU)/1000000000000.0)  // 1 au baseline
    throughputRatio := c.Entry.Observed.EWMA_TokPerSec / 1000.0  // 1000 tok/s baseline
    
    return w.ReputationAlpha*c.Entry.Contract.Reputation +
           w.SaturationBeta*latencyFactor*throughputRatio +
           w.PriceGamma*(1.0-priceNorm)
}
```
