# Payment Rails Specification

## Overview

Implement a multi-rail payment system supporting fiat (Stripe), TAP (ERC-20 on Ethereum), and TNK (Trac native token). Each rail operates independently with strict isolation.

## Payment Rail Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Payment Abstraction                       │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                  │
│  │  Stripe  │  │   TAP    │  │   TNK    │                  │
│  │  (Fiat)  │  │ (ERC-20) │  │  (Trac)  │                  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘                  │
│       │             │             │                         │
│       ▼             ▼             ▼                         │
│  ┌─────────────────────────────────────────┐                │
│  │           Payment Router                 │                │
│  │  - Validates rail selection              │                │
│  │  - Routes to appropriate rail            │                │
│  │  - Enforces cross-rail isolation         │                │
│  └─────────────────────────────────────────┘                │
│                        │                                     │
│                        ▼                                     │
│  ┌─────────────────────────────────────────┐                │
│  │           Settlement Engine              │                │
│  │  - Epoch-based settlements               │                │
│  │  - Multi-rail reconciliation             │                │
│  │  - Claim processing (TAP)                │                │
│  └─────────────────────────────────────────┘                │
└─────────────────────────────────────────────────────────────┘
```

## Rail Types

### Fiat Rail (Stripe)

| Aspect | Details |
|--------|---------|
| Currency | USD (display), local currency (collection) |
| Top-up | Stripe Checkout session |
| Payout | Stripe Connect (providers) |
| Settlement | Automatic after epoch |
| Gas | Sponsored by network |

### TAP Rail (Ethereum ERC-20)

| Aspect | Details |
|--------|---------|
| Token | ERC-20 on Ethereum |
| Top-up | Deposit to in-app address |
| Payout | Merkle claim after epoch |
| Settlement | Cumulative (not per-epoch) |
| Gas | Claim transaction (user pays) |

### TNK Rail (Trac Native)

| Aspect | Details |
|--------|---------|
| Token | Trac native token |
| Top-up | Direct transfer to in-app address |
| Payout | Automatic push after epoch |
| Settlement | Automatic via MSB |
| Gas | Sponsored by network |

## Data Structures

### Account and Balance

```go
// PaymentAccount holds balance for a rail
type PaymentAccount struct {
    AccountID   AccountID
    Rail        RailType
    BalanceAU   int64       // Balance in atto-USD
    PendingAU   int64       // Pending deposits
    HoldAU      int64       // Reserved for open sessions
    UpdatedAt   time.Time
}

// RailType enum
type RailType int

const (
    RailFiat RailType = iota  // Stripe
    RailTAP                   // Ethereum ERC-20
    RailTNK                   // Trac native
)

// Balance response
type BalanceResponse struct {
    Fiat    string  `json:"fiat"`    // "10.000000 USD"
    TAP     string  `json:"tap"`     // "0.000000 TAP"
    TNK     string  `json:"tnk"`     // "0.000000 TNK"
}
```

### Deposit Flow

```go
// DepositRequest for any rail
type DepositRequest struct {
    Rail     RailType
    Amount   int64       // In au (atto-USD)
    Asset    string      // For TAP: token address, for others: ignored
    Confirm  bool        // Execute or dry-run
}

// Stripe specific
type StripeDepositRequest struct {
    AmountCents int64
    Currency    string   // "usd", "eur", etc.
    SuccessURL  string
    CancelURL   string
}

// Deposit status
type DepositStatus struct {
    ID            DepositID
    Rail          RailType
    AmountAU      int64
    Status        DepositState
    RailTXHash    string      // Chain transaction hash
    CreatedAt     time.Time
    ConfirmedAt   *time.Time
}

// Deposit states
type DepositState int

const (
    DepositStatePending DepositState = iota
    DepositStateConfirming
    DepositStateConfirmed
    DepositStateFailed
)
```

### Settlement

```go
// SettlementBatch for epoch settlement
type SettlementBatch struct {
    Epoch          int64
    Rail           RailType
    Transfers      []Transfer
    TotalAmountAU  int64
    MSBOperationID string      // MSB (Main Settlement Bus) operation
    State          SettlementState
    SignedAt       time.Time
}

// Transfer represents a single settlement transfer
type Transfer struct {
    OperationID   string
    Network       string          // "ethereum", "trac"
    From          string          // MSB address
    To            string          // Recipient address
    AmountE18     int64           // Amount in wei/smallest unit
    TXHash        string          // Transaction hash
    Payload       json.RawMessage // Rail-specific data
}

// Settlement journal entry for recovery
type JournalEntry struct {
    SchemaVersion  int
    Status         string      // "prepared", "confirmed"
    OperationID    string
    Network        string
    From           string
    To             string
    AmountE18      int64
    TXHash         string
    Payload        json.RawMessage
    BeforeBalance  int64
    ConfirmedAt    *time.Time
}
```

### Provider Payout

```go
// ProviderPayoutBinding for receiving payments
type ProviderPayoutBinding struct {
    ProviderID   string
    Rail         RailType
    Destination  string          // Wallet address or Stripe account
    Status       BindingState
    ActivatesAt  int64           // Epoch number when active
    CreatedAt    time.Time
    ConfirmedAt  *time.Time
}

// BindingState
type BindingState int

const (
    BindingStatePending BindingState = iota
    BindingStateVerified
    BindingStateActive
    BindingStateRotated
)

// Provider earnings report
type EarningsReport struct {
    ProviderID    string
    Rail          RailType
    GrossAU       int64       // Total earned
    NetworkFeeAU  int64       // 15% network fee
    NetAU         int64       // Gross - fee
    HoldbackAU    int64       // Reserved for disputes
    ClaimableAU   int64       // Available for claim/payout
    LastClaimedAt *time.Time
}
```

## Wallet Management

```go
// Wallet for cryptographic operations
type Wallet struct {
    Address     string          // Derived address
    Source      WalletSource    // How key was created
    CreatedAt   time.Time
    Encrypted   bool            // Whether key is encrypted
}

// WalletSource
type WalletSource int

const (
    WalletSourceGenerated WalletSource = iota  // New mnemonic
    WalletSourceImported                       // Imported key
    WalletSourceHardware                       // Hardware wallet (future)
)

// Wallet operations
type WalletService interface {
    CreateWallet(password string) (*Wallet, error)
    RestoreWallet(mnemonic string, password string) (*Wallet, error)
    BackupWallet() (string, error)  // Returns mnemonic
    GetAddress(rail RailType) (string, error)
    Sign(tx []byte) ([]byte, error)
}
```

## API Interface

### Balance and Deposits

```go
type PaymentService interface {
    // Balance
    GetBalance(rail RailType) (*PaymentAccount, error)
    GetAllBalances() (*BalanceResponse, error)
    
    // Deposits
    CreateStripeCheckout(ctx context.Context, amountCents int64) (*CheckoutSession, error)
    InitiateTAPDeposit(ctx context.Context, amountTAP float64) (*DepositRequest, error)
    InitiateTNKDeposit(ctx context.Context, amountTNK float64) (*DepositRequest, error)
    WaitForDeposit(ctx context.Context, depositID DepositID, timeout time.Duration) (*DepositStatus, error)
    
    // Withdrawals (for providers)
    SetPayoutBinding(ctx context.Context, rail RailType, destination string) (*ProviderPayoutBinding, error)
    RotatePayoutBinding(ctx context.Context, rail RailType, newDestination string) error
    GetPayoutBinding(rail RailType) (*ProviderPayoutBinding, error)
    
    // Earnings
    GetEarnings(rail RailType) (*EarningsReport, error)
    ClaimTAPEarnings(ctx context.Context) (*ClaimResult, error)
}
```

### Checkout Session (Stripe)

```go
type CheckoutSession struct {
    ID        string
    URL       string
    Amount    int64
    Currency  string
    Status    string
}
```

### Claim Result (TAP)

```go
type ClaimResult struct {
    OperationID  string
    TXHash       string
    Amount       int64
    GasUsed      int64
    Status       string
}
```

## Cross-Rail Isolation

### Invariants

1. **No mixing**: A session on rail X can only pay from rail X balance
2. **No conversion**: Rails do not convert between each other
3. **Separate settlement**: Each rail settles independently
4. **Independent binding**: Payout bindings are per-rail

### Implementation

```go
// ValidateRailIsolation ensures proper rail usage
func (s *PaymentService) ValidateRailIsolation(session *Session) error {
    if session.Rail != session.Voucher.Rail {
        return fmt.Errorf("rail mismatch: session=%v, voucher=%v", 
            session.Rail, session.Voucher.Rail)
    }
    
    account, err := s.GetBalance(session.Rail)
    if err != nil {
        return err
    }
    
    if account.BalanceAU < session.Voucher.TotalAU {
        return fmt.Errorf("insufficient balance on rail %v", session.Rail)
    }
    
    return nil
}
```

## Settlement Flow

### Epoch Settlement

```
1. Epoch ends
2. Collect all receipts for the epoch per rail
3. Calculate provider earnings per rail
4. For each rail:
   a. Fiat: Initiate Stripe transfers
   b. TAP: Generate Merkle root, no auto-transfer
   c. TNK: Sign MSB transfer messages
5. Execute transfers (async for TAP/TNK)
6. Update balances and earnings
7. Publish settlement summary
```

### TAP Claim Process

```
1. User has accumulated unclaimed TAP earnings
2. User calls ClaimTAPEarnings()
3. System:
   a. Verify accumulated amount > 0
   b. Build Merkle proof
   c. Sign claim transaction
   d. Submit to Ethereum
   e. Wait for confirmation
   f. Update claimed amount
4. User receives tokens
```

## Configuration

```go
type PaymentConfig struct {
    // Rails
    EnabledRails []RailType
    
    // Stripe
    StripeSecretKey     string
    StripeWebhookSecret string
    StripeConnectSecret string  // For provider payouts
    
    // Ethereum (TAP)
    EthereumRPC         string  // Ethereum RPC URL
    TAPTokenAddress     string  // TAP ERC-20 address
    GasSponsorAddress   string  // Gas fee sponsor
    
    // Trac (TNK)
    TracMSBAddress      string  // Main Settlement Bus address
    TracNodeRPC         string  // Trac node RPC
    
    // Fees
    NetworkFeePercent   float64 // 0.15 = 15%
    MinClaimAmountAU    int64   // Minimum TAP claim
    
    // Settlement
    SettlementInterval  time.Duration  // Default: 1 hour
    SettlementTimeout   time.Duration  // Max wait for settlement
}
```

## Error Codes

| Error | Description |
|-------|-------------|
| `payment.insufficient_balance` | Not enough balance for operation |
| `payment.rail_not_supported` | Rail not enabled |
| `payment.rail_mismatch` | Session/rail mismatch |
| `payment.deposit_failed` | Deposit transaction failed |
| `payment.confirmation_timeout` | Wait for confirmation timed out |
| `payment.claim_failed` | TAP claim transaction failed |
| `payment.settlement_failed` | Settlement execution failed |
| `payment.binding_not_verified` | Payout binding not verified |

## Acceptance Criteria

- **A9.1**: Stripe checkout creates valid payment session
- **A9.2**: Balance updates after Stripe webhook confirmation
- **A10.1**: TAP deposit address accepts ERC-20 transfers
- **A10.2**: TAP balance updates after deposit confirmation
- **A11.1**: TNK balance updates after Trac transfer
- **A11.2**: TNK settlement auto-transfers to providers
- **A12.1**: Sessions on rail A cannot use rail B balance
- **A12.2**: Cross-rail transfers are rejected

## Dependencies

```go
// Required Go modules
github.com/stripe/stripe-go/v76        // Stripe SDK
github.com/ethereum/go-ethereum        // Ethereum JSON-RPC
github.com/trac-network/trac-go        // Trac SDK (if available)
github.com/awni/magnesium              // BIP39 mnemonic
github.com/tyler-smith/go-bip39        // Mnemonic generation
github.com/ethereum/go-ethereum/accounts HD  // HD wallet derivation
```
