package balance

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RailType identifies which payment rail a balance belongs to.
type RailType string

const (
	RailStripe RailType = "stripe" // Fiat via Stripe
	RailTAP    RailType = "tap"    // ERC-20 on Ethereum
	RailTNK    RailType = "tnk"    // Trac native token
)

// IsValid returns true if the rail type is recognized.
func (r RailType) IsValid() bool {
	switch r {
	case RailStripe, RailTAP, RailTNK:
		return true
	default:
		return false
	}
}

// String returns the string representation of the rail type.
func (r RailType) String() string {
	return string(r)
}

// ErrRailMismatch is returned when a cross-rail operation is attempted.
var ErrRailMismatch = errors.New("balance: operation spans multiple payment rails")

// RailAccount holds the balance for a specific API key + rail combination.
type RailAccount struct {
	APIKey      string
	Rail        RailType
	BalanceCents int64
}

// RailStore manages balances per (apiKey, rail).
// Cross-rail isolation: each rail has independent accounting.
type RailStore struct {
	mu       sync.RWMutex
	accounts map[string]map[RailType]int64 // apiKey → (rail → cents)
}

// NewRailStore creates an empty rail-aware balance store.
func NewRailStore() *RailStore {
	return &RailStore{
		accounts: make(map[string]map[RailType]int64),
	}
}

// key returns the composite key for an account.
func key(apiKey string, rail RailType) string {
	return apiKey + "|" + string(rail)
}

// Get returns the balance for an API key on a specific rail.
func (s *RailStore) Get(apiKey string, rail RailType) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rails, ok := s.accounts[apiKey]; ok {
		return rails[rail]
	}
	return 0
}

// SetBalance sets the balance for an API key on a specific rail.
func (s *RailStore) SetBalance(apiKey string, rail RailType, cents int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts[apiKey] == nil {
		s.accounts[apiKey] = make(map[RailType]int64)
	}
	s.accounts[apiKey][rail] = cents
}

// AddCredits adds credits to an API key on a specific rail.
func (s *RailStore) AddCredits(apiKey string, rail RailType, cents int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts[apiKey] == nil {
		s.accounts[apiKey] = make(map[RailType]int64)
	}
	s.accounts[apiKey][rail] += cents
}

// Deduct deducts cents from an API key on a specific rail.
// Returns the new balance and an error if insufficient.
func (s *RailStore) Deduct(apiKey string, rail RailType, cents int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts[apiKey] == nil {
		return 0, ErrInsufficientBalance
	}
	current := s.accounts[apiKey][rail]
	if current < cents {
		return current, ErrInsufficientBalance
	}
	s.accounts[apiKey][rail] -= cents
	return s.accounts[apiKey][rail], nil
}

// HasSufficientBalance returns true if the API key has enough on the given rail.
func (s *RailStore) HasSufficientBalance(apiKey string, rail RailType, cents int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rails, ok := s.accounts[apiKey]; ok {
		return rails[rail] >= cents
	}
	return false
}

// ValidateRailIsolation checks that a list of operations are all on the same rail.
// Returns ErrRailMismatch if operations span multiple rails.
func ValidateRailIsolation(ops []RailOperation) error {
	if len(ops) == 0 {
		return nil
	}
	first := ops[0].Rail
	for _, op := range ops[1:] {
		if op.Rail != first {
			return ErrRailMismatch
		}
	}
	return nil
}

// RailOperation describes a single balance-modifying operation.
type RailOperation struct {
	APIKey string
	Rail   RailType
	Cents  int64
}

// DepositResult is returned after initiating a deposit.
type DepositResult struct {
	DepositID   string     `json:"deposit_id"`
	URL         string     `json:"url"`          // Checkout URL (Stripe) or chain address (TAP/TNK)
	AmountCents int64      `json:"amount_cents"`
	Currency    string     `json:"currency"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// WithdrawResult is returned after initiating a withdrawal.
type WithdrawResult struct {
	WithdrawID string `json:"withdraw_id"`
	TxHash     string `json:"tx_hash"`
	Status     string `json:"status"` // "pending", "confirmed", "failed"
}

// Settlement describes a settled deposit.
type Settlement struct {
	DepositID   string    `json:"deposit_id"`
	ApiKey      string    `json:"api_key"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	SettledAt   time.Time `json:"settled_at"`
	Rail        RailType  `json:"rail"`
}

// Rail is the interface for a payment rail.
type Rail interface {
	Name() string

	// Deposit initiates a deposit and returns the destination (address or URL).
	Deposit(ctx context.Context, apiKey string, amountCents int64, currency string) (*DepositResult, error)

	// Withdraw initiates a withdrawal.
	Withdraw(ctx context.Context, apiKey string, amountCents int64, destination string) (*WithdrawResult, error)

	// Balance returns the rail-specific balance in cents.
	Balance(ctx context.Context, apiKey string) (int64, error)
}

// BalanceResponse is the API response for a multi-rail balance query.
type BalanceResponse struct {
	APIKey     string           `json:"api_key"`
	Balances   map[RailType]int64 `json:"balances"` // rail → cents
	TotalCents int64            `json:"total_cents"`
}
