package balance

import (
	"errors"
	"sync"
)

// ErrInsufficientBalance is returned when an account has insufficient funds.
var ErrInsufficientBalance = errors.New("insufficient balance")

// Account holds a user's balance state.
type Account struct {
	APIKey   string
	BalanceCents int64 // balance in cents (USD)
}

// InMemoryStore holds account balances in memory.
type InMemoryStore struct {
	mu        sync.RWMutex
	accounts  map[string]Account
	minBalance int64 // minimum balance in cents; deductions below this are blocked
}

// NewInMemoryStore creates an empty balance store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		accounts:  make(map[string]Account),
		minBalance: 0,
	}
}

// Get returns the account for an API key.
func (s *InMemoryStore) Get(apiKey string) Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.accounts[apiKey]
}

// SetBalance sets the initial or current balance for an API key.
func (s *InMemoryStore) SetBalance(apiKey string, balanceCents int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts[apiKey] = Account{APIKey: apiKey, BalanceCents: balanceCents}
}

// Deduct subtracts costCents from the account balance.
// Returns error if balance would go below minBalance (0 for in-memory).
// Returns the new balance or error.
func (s *InMemoryStore) Deduct(apiKey string, costCents int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[apiKey]
	if !ok {
		return 0, ErrInsufficientBalance
	}

	if acc.BalanceCents-costCents < s.minBalance {
		return acc.BalanceCents, ErrInsufficientBalance
	}

	acc.BalanceCents -= costCents
	s.accounts[apiKey] = acc
	return acc.BalanceCents, nil
}

// AddCredits adds credits to an account.
func (s *InMemoryStore) AddCredits(apiKey string, cents int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[apiKey]
	if !ok {
		acc = Account{APIKey: apiKey}
	}
	acc.BalanceCents += cents
	s.accounts[apiKey] = acc
}

// HasSufficientBalance returns true if the account can afford costCents.
func (s *InMemoryStore) HasSufficientBalance(apiKey string, costCents int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	acc, ok := s.accounts[apiKey]
	if !ok {
		return false
	}
	return acc.BalanceCents-costCents >= s.minBalance
}
