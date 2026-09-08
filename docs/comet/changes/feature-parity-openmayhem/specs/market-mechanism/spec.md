# Market Mechanism Specification

## Overview

Implement a dynamic market pricing system based on supply-demand equilibrium with epoch-based settlement. The market adjusts prices based on utilization, with provider min-ask and user max-bid controls.

## Market Design

### Core Principles

1. **Price Discovery**: Market price emerges from supply/demand interaction
2. **Price Locking**: Session price is locked at open, never repriced mid-session
3. **Epoch Settlement**: Prices and settlements are finalized at epoch boundaries (1 hour)
4. **Utilization-Based**: Price adjusts based on capacity utilization

### Price Adjustment Algorithm

```
Target: ~85% utilization

If utilization > 85%:
    price += min(price * 0.10, max_step)
    // Price steps up, max 10% per epoch
    
If utilization < 85%:
    price -= min(price * 0.10, max_step)
    // Price steps down, max 10% per epoch
    
If utilization == 85%:
    price unchanged
```

### Utilization Calculation

```
utilization = settled_work / total_capacity

- settled_work: Receipt-verified completed sessions
- total_capacity: Sum of provider capacities
- Only verified work counts (no phantom supply)
```

## Data Structures

### Market State

```go
// Market represents a model×tier market
type Market struct {
    ID            MarketID
    ModelID       string
    Tier          AttestationTier
    CurrentPrice  PriceInfo
    Epoch         EpochInfo
    State         MarketState
    Providers     []MarketProvider
    MinAsk        int64  // Provider's minimum ask in au
    MaxBid        int64  // User's maximum bid in au
}

// PriceInfo contains current pricing
type PriceInfo struct {
    InputPriceAU  int64   // atto-USD per input token
    OutputPriceAU int64   // atto-USD per output token
    Epoch         int64   // Epoch number
    SeedPriceAU   int64   // Initial seed price
    Utilization   float64 // Current utilization 0.0-1.0
}

// EpochInfo tracks epoch state
type EpochInfo struct {
    Number       int64     // Epoch number
    StartTime    time.Time // Epoch start
    EndTime      time.Time // Expected end
    Settlements  []SettlementReceipt
    State        EpochState
}

// MarketState enum
type MarketState int

const (
    MarketStateSeed MarketState = iota  // Initial state, price at seed
    MarketStateActive                   // Active trading
    MarketStateSettled                  // Epoch completed
)
```

### Session Voucher (Price Lock)

```go
// SessionVoucher locks price for a session
type SessionVoucher struct {
    ID              SessionID
    MarketID        MarketID
    ProviderID      string
    InputPriceAU    int64   // Locked at session open
    OutputPriceAU   int64   // Locked at session open
    InputTokens     int64
    OutputTokens    int64
    TotalAU         int64   // Calculated: input*tokens*input_price + ...
    IssuedAt        time.Time
    Epoch           int64
    Status          VoucherStatus
}

// VoucherStatus tracks settlement state
type VoucherStatus int

const (
    VoucherStatusPending VoucherStatus = iota
    VoucherStatusFulfilled
    VoucherStatusDisputed
    VoucherStatusExpired
)
```

### Provider Controls

```go
// ProviderMarketConfig for provider's market participation
type ProviderMarketConfig struct {
    ProviderID   string
    Markets      []MarketParticipation
    
    // Controls
    MinAskAU     map[MarketID]int64  // Per-market minimum ask
    MaxDailyAU   int64               // Daily budget cap
    AcceptRate   RateLimit           // Max accepts per minute
    MaxConcurrent int                // Concurrent session limit
}

// MarketParticipation tracks per-market state
type MarketParticipation struct {
    MarketID      MarketID
    MinAskAU      int64
    IsActive      bool
    LastBidEpoch  int64
    EarningsAU    int64
}
```

### Receipt and Settlement

```go
// Receipt is signed proof of completed work
type Receipt struct {
    ID              ReceiptID
    SessionID       SessionID
    VoucherID       SessionID  // Reference to locked price
    ProviderID      string
    ModelID         string
    InputTokens     int64
    OutputTokens    int64
    InputPriceAU    int64      // From voucher, not current market
    OutputPriceAU   int64      // From voucher
    TotalAU         int64
    IssuedAt        time.Time
    ProviderSig     []byte     // Provider signature
    Status          ReceiptStatus
}

// ReceiptStatus
type ReceiptStatus int

const (
    ReceiptStatusIssued ReceiptStatus = iota
    ReceiptStatusVerified
    ReceiptStatusSettled
    ReceiptStatusDisputed
)
```

## State Machines

### Epoch State Machine

```
Epoch Start ──► Active ──► Settling ──► Complete ──► Next Epoch
                  │            │
                  └────────────┘ (if no receipts)
```

### Price Adjustment State

```
                    utilization > 85%
                        │
                        ▼
              ┌───────────────────┐
              │   Price Up        │ ◄────────────┐
              │   (max 10% step)  │              │
              └───────────────────┘              │
                        │                        │
                        │ utilization < 85%      │
                        ▼                        │
              ┌───────────────────┐              │
              │   Price Down      │──────────────┘
              │   (max 10% step)  │
              └───────────────────┘
                        │
                        │ utilization ≈ 85%
                        ▼
              ┌───────────────────┐
              │   Price Stable    │
              └───────────────────┘
```

### Settlement Flow

```
1. Epoch ends
2. Collect all receipts for epoch
3. Calculate provider earnings
4. Sign settlement batch
5. Execute transfers (Stripe/TAP/TNK)
6. Publish epoch summary
7. Start new epoch
```

## API Interface

```go
// MarketService handles market operations
type MarketService interface {
    // Market queries
    GetMarket(modelID string, tier AttestationTier) (*Market, error)
    ListMarkets() ([]*Market, error)
    GetPrice(modelID string, tier AttestationTier) (*PriceInfo, error)
    
    // Provider controls
    SetMinAsk(ctx context.Context, marketID MarketID, minAskAU int64) error
    GetMinAsk(providerID string, marketID MarketID) (int64, error)
    
    // Session pricing
    CreateVoucher(ctx context.Context, session *SessionRequest) (*SessionVoucher, error)
    LockPrice(voucher *SessionVoucher) error  // Called at session open
    
    // Receipts and settlement
    IssueReceipt(ctx context.Context, session *CompletedSession) (*Receipt, error)
    VerifyReceipt(ctx context.Context, receipt *Receipt) error
    
    // Epoch operations
    GetCurrentEpoch() (*EpochInfo, error)
    SettleEpoch(ctx context.Context, epoch int64) (*SettlementResult, error)
}

// SessionRequest with pricing
type SessionRequest struct {
    ModelID       string
    Tier          AttestationTier
    MaxBidAU      int64        // User's max price (from header or config)
    MinContext    int64
    Quantization  string
    InputTokens   int64        // Estimated
}
```

## Pricing Units

```go
// All prices are in atto-USD (au), 10^-18 dollars
// 1 dollar = 1,000,000,000,000,000,000 au

const (
    AUPerDollar = 1e18
    // $0.50 per 1M tokens = 500,000,000,000 au per token
    // $0.001 per 1M tokens = 1,000,000,000 au per token
)

// Price conversion helpers
func TokensToAU(tokens int64, priceAU int64) int64 {
    return tokens * priceAU
}

func AUToDollars(au int64) float64 {
    return float64(au) / AUPerDollar
}

func DollarsToAU(dollars float64) int64 {
    return int64(dollars * AUPerDollar)
}
```

## Price Calculation Examples

### Text Generation

```go
// Input tokens × input_price + output tokens × output_price
inputTokens := int64(1000)
outputTokens := int64(500)
inputPrice := int64(500_000_000_000)  // $0.50 per 1M = $0.0000005 per token
outputPrice := int64(2_000_000_000_000)  // $2.00 per 1M

totalAU := inputTokens*inputPrice + outputTokens*outputPrice
// = 1000*500_000_000_000 + 500*2_000_000_000_000
// = 500_000_000_000_000 + 1_000_000_000_000_000
// = 1_500_000_000_000_000 au = $0.0015
```

### Image Generation

```go
// Per-image with resolution multiplier
basePrice := int64(100_000_000_000_000)  // $0.10 base
resolutionMultiplier := 4.0  // 2048x2048 vs 512x512
totalAU := int64(float64(basePrice) * resolutionMultiplier)
```

## Error Codes

| Error | Description |
|-------|-------------|
| `market.no_provider` | No provider at user's price |
| `market.price_locked` | Cannot change price mid-session |
| `market.min_ask_exceeded` | Market price below provider's min-ask |
| `market.max_bid_exceeded` | User's max-bid below market price |
| `market.no_capacity` | No provider has capacity |
| `market.epoch_not_active` | Epoch not accepting sessions |
| `market.settlement_failed` | Settlement execution failed |

## Acceptance Criteria

- **A6.1**: Price adjusts up when utilization > 85%
- **A6.2**: Price adjusts down when utilization < 85%
- **A6.3**: Price step is capped at 10% per epoch
- **A6.4**: Session price is locked at open and never repriced
- **A6.5**: Provider min-ask controls market participation
- **A6.6**: Market with <2 providers stays at seed price
- **A6.7**: Epoch settlement executes automatically
- **A6.8**: Receipts are signed and verifiable
- **A6.9**: Utilization calculation uses only verified work

## Non-Functional Requirements

- **Latency**: Price lookup < 10ms
- **Throughput**: Support 1000+ concurrent sessions per market
- **Consistency**: Price lock is atomic with session creation
- **Durability**: All receipts persisted before acknowledgment
