# Multi-Payment Rails Specification

## Overview

HiveMachine supports three independent payment rails: Stripe (fiat), TAP (ERC-20 on Ethereum), and TNK (Trac native token). Each rail has isolated accounting — balances, receipts, and settlement are rail-specific and never mixed.

## Rail Definitions

| Rail | Currency | Settlement | Balance Type |
|------|----------|------------|--------------|
| Stripe | USD | Immediate (webhook) | fiat balance |
| TAP | ETH/USDC | ~12 min (1 Ethereum block finality) | crypto balance |
| TNK | TNK | ~5s (Trac Network) | native balance |

**This spec covers the abstract rail interface and Stripe integration (TAP/TNK are future phases).**

## Core Interface

All rails implement the `Rail` interface:

```go
// Rail is the interface for a payment rail.
type Rail interface {
    // Name returns the rail identifier: "stripe", "tap", "tnk".
    Name() string

    // Deposit initiates a deposit and returns a reference (checkout URL, tx hash, etc.).
    Deposit(ctx context.Context, apiKey string, amountCents int64, currency string) (*DepositResult, error)

    // Withdraw initiates a withdrawal from user balance to an external address.
    Withdraw(ctx context.Context, apiKey string, amountCents int64, destination string) (*WithdrawResult, error)

    // Balance returns the current rail-specific balance for the given API key.
    Balance(ctx context.Context, apiKey string) (int64, error)

    // Settle processes an incoming webhook or callback to credit the balance.
    Settle(ctx context.Context, payload []byte, signature string) (*Settlement, error)

    // CancelDeposit releases or refunds a pending deposit.
    CancelDeposit(ctx context.Context, depositID string) error
}
```

### Types

```go
// DepositResult is returned after initiating a deposit.
type DepositResult struct {
    DepositID   string // Rail-specific deposit identifier
    URL         string // Checkout URL (Stripe) or payment address (TAP/TNK)
    AmountCents int64  // Confirmed deposit amount
    Currency    string // ISO 4217 currency code
    ExpiresAt   *time.Time // Deposit expiry (for checkout flows)
}

// WithdrawResult is returned after initiating a withdrawal.
type WithdrawResult struct {
    WithdrawID string // Rail-specific withdrawal identifier
    TxHash     string // Transaction hash (for on-chain rails)
    Status     string // "pending", "confirmed", "failed"
}

// Settlement describes a settled deposit.
type Settlement struct {
    DepositID   string
    ApiKey      string
    AmountCents int64
    Currency    string
    SettledAt   time.Time
    Rail        string
}
```

## Cross-Rail Isolation

Balances are strictly isolated per rail:

```
api_key_abc123:
  stripe_balance: 5000  (cents)
  tap_balance:    0     (cents, ETH valued at settlement price)
  tnk_balance:    0     (cents, TNK valued at settlement price)
```

**Transfer between rails is not supported.** A user with Stripe balance cannot convert to TAP balance without an external exchange.

### Isolation Rules
1. Each rail has its own `BalanceStore` instance (no shared state)
2. Receipts are rail-specific (a Stripe receipt cannot be redeemed on TAP)
3. Deduction is rail-specific (spending deducts from the rail selected at request time)
4. A request can specify which rail to charge: `X-Payment-Rail: stripe|tap|tnk` (default: stripe)

## Stripe Rail Implementation

### Current Implementation (`pkg/paygate/stripe/paygate.go`)

```go
type Paygate struct {
    secretKey       string
    endpointSecret  string
    balanceStore    BalanceStore
}

func NewPaygate(secretKey, endpointSecret string, balanceStore BalanceStore) *Paygate
func (p *Paygate) CreateCheckoutSession(ctx context.Context, req CreateCheckoutSessionRequest) (*CreateCheckoutSessionResponse, error)
func (p *Paygate) ProcessWebhook(payload []byte, sig string) error
```

### Supported Events
- `checkout.session.completed` → credit `amount_cents` to `api_key` in metadata
- `payment_intent.succeeded` → credit `amount_cents` to `api_key` in metadata (alternative path)

### Metadata Convention

All Stripe objects store:
- `api_key`: The HiveMachine API key to credit
- `amount_cents`: Integer deposit amount in cents

### Dev Mode

When `endpointSecret == ""`, `ProcessWebhook` skips signature verification. This allows local testing without Stripe CLI.

### Future: Stripe Payouts

When a provider earns balance, they can withdraw to their Stripe account via Stripe Connect. This requires:
- Provider onboards via Stripe Connect Express
- Gateway creates a Connect transfer to the provider's connected account
- Settlement confirmed via `transfer.created` webhook

## TAP Rail (Future Phase)

### Overview
TAP is an ERC-20 token on Ethereum. Deposits flow:
1. User sends ETH or USDC to a gateway-managed deposit address
2. Gateway monitors the Ethereum mempool for incoming transfers
3. On chain confirmation (12 minutes), gateway credits the user's TAP balance
4. Balance is denominated in cents at a configured ETH→USD price feed

### Interface Extension

```go
// EthereumDepositAddress returns the gateway's deposit address for TAP rails.
func (r *TAPRail) EthereumDepositAddress(apiKey string) (string, error)

// EstimatedGas returns the estimated gas cost for a withdrawal.
func (r *TAPRail) EstimatedGas(ctx context.Context, amountCents int64, destination string) (int64, error)
```

### Implementation Notes
- Use `ethereum/exec` for on-chain interaction
- Monitor deposits via `ethclient.Client.FilterLogs` polling or WebSocket subscription
- Price feed: Chainlink ETH/USD oracle (or configurable fallback)
- Gas estimation includes gas cost for withdrawal transaction + 20% buffer

## TNK Rail (Future Phase)

### Overview
TNK is the Trac Network's native token. Deposits use Trac's SDK:
1. User sends TNK to a gateway-managed Trac address
2. Gateway monitors the Trac chain via SDK
3. On finality (~5s), gateway credits TNK balance
4. Balance is denominated in cents at on-chain TNK→USD price

### Interface Extension

```go
// TracDepositAddress returns the gateway's deposit address for TNK rails.
func (r *TNKRail) TracDepositAddress(apiKey string) (string, error)
```

### Implementation Notes
- Use Trac Network SDK (`github.com/trac-network/sdk-go`)
- Monitor deposits via SDK subscription to incoming transfers
- Price feed: Trac's built-in oracle or Chainlink TNK/USD

## Settlement Flow

```
User                    Gateway                  Rail Provider
  |                         |                         |
  |-- deposit request ----->|                         |
  |                         |-- initiate deposit ---->|
  |                         |<--- deposit ref --------|  (checkout URL / tx address)
  |<-- deposit ref ---------|                         |
  |                         |                         |
  |--- complete payment --->|                         |
  |                         |                         |
  |                    [rail monitors]                |
  |                         |<--- settlement event ---|
  |                         |                         |
  |                         |-- credit balance ------->|
  |                         |  (BalanceStore.Add)     |
```

## Open Questions

- **Q1**: Should deposits expire? (Stripe checkout sessions expire in 24h; what about TAP/TNK?)
- **Q2**: How to handle partial settlements? (Ethereum mempool can have multiple transfers; deduplicate by tx hash)
- **Q3**: Should there be a minimum deposit? (Yes — configurable, e.g., $1 for Stripe, $5 for ETH)
- **Q4**: How are exchange rates locked? (Deposit at rate when initiated, not when settled?)
- **Q5**: What is the refund policy for failed deposits? (Stripe: automatic; TAP/TNK: manual or via governance)

## Constraints

- Each rail is independently testable without affecting others
- Rail selection is per-request, not per-connection
- Gateway never holds private keys for TAP/TNK — uses hot wallet with threshold signing or MPC
- All rail operations are idempotent (deposit deduplication by rail-specific deposit ID)
- Minimum deposit amounts are enforced at the rail level
