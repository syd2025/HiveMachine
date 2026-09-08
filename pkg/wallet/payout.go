package wallet

import (
	"errors"
	"sync"
)

// PayoutBinding represents a verified payout destination bound to a wallet.
type PayoutBinding struct {
	WalletID   string     // Wallet this binding belongs to
	Chain      ChainType  // Target chain
	Address    string     // Blockchain address
	Verified   bool       // True if the binding has been verified on-chain
	VerifiedAt string     // RFC3339 timestamp of verification
	Label      string     // Optional label (e.g., "Stripe Connect", "Personal ETH")
}

// ErrBindingNotFound is returned when a payout binding is not found.
var ErrBindingNotFound = errors.New("payout binding not found")

// PayoutBindingStore manages payout bindings.
type PayoutBindingStore interface {
	// Set saves a payout binding for a wallet.
	Set(walletID string, b *PayoutBinding) error

	// ByWalletID returns all payout bindings for a wallet.
	ByWalletID(walletID string) ([]*PayoutBinding, error)

	// ByWalletAndChain returns the binding for a specific wallet and chain.
	ByWalletAndChain(walletID string, chain ChainType) (*PayoutBinding, error)

	// Verify marks a binding as verified.
	// In production, this would verify the address on-chain (e.g., sign a message proving control).
	Verify(walletID string, chain ChainType) error

	// Delete removes a binding.
	Delete(walletID string, chain ChainType) error
}

// InMemoryPayoutBindingStore holds bindings in process memory.
type InMemoryPayoutBindingStore struct {
	mu       sync.RWMutex
	bindings map[string]map[ChainType]*PayoutBinding // walletID → (chain → binding)
}

// NewInMemoryPayoutBindingStore creates an empty in-memory payout binding store.
func NewInMemoryPayoutBindingStore() *InMemoryPayoutBindingStore {
	return &InMemoryPayoutBindingStore{
		bindings: make(map[string]map[ChainType]*PayoutBinding),
	}
}

// Set saves a payout binding.
func (s *InMemoryPayoutBindingStore) Set(walletID string, b *PayoutBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindings[walletID] == nil {
		s.bindings[walletID] = make(map[ChainType]*PayoutBinding)
	}
	b.WalletID = walletID
	s.bindings[walletID][b.Chain] = b
	return nil
}

// ByWalletID returns all bindings for a wallet.
func (s *InMemoryPayoutBindingStore) ByWalletID(walletID string) ([]*PayoutBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chains, ok := s.bindings[walletID]
	if !ok {
		return nil, nil
	}
	out := make([]*PayoutBinding, 0, len(chains))
	for _, b := range chains {
		out = append(out, b)
	}
	return out, nil
}

// ByWalletAndChain returns the binding for a specific wallet and chain.
func (s *InMemoryPayoutBindingStore) ByWalletAndChain(walletID string, chain ChainType) (*PayoutBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if chains, ok := s.bindings[walletID]; ok {
		if b, ok := chains[chain]; ok {
			return b, nil
		}
	}
	return nil, ErrBindingNotFound
}

// Verify marks a binding as verified.
// In production, this would verify control via on-chain message signing.
func (s *InMemoryPayoutBindingStore) Verify(walletID string, chain ChainType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chains, ok := s.bindings[walletID]; ok {
		if b, ok := chains[chain]; ok {
			b.Verified = true
			b.VerifiedAt = "2026-09-08T00:00:00Z" // Would be time.Now().Format(time.RFC3339) in production
			return nil
		}
	}
	return ErrBindingNotFound
}

// Delete removes a binding.
func (s *InMemoryPayoutBindingStore) Delete(walletID string, chain ChainType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chains, ok := s.bindings[walletID]; ok {
		delete(chains, chain)
	}
	return nil
}
