package trust

import (
	"crypto"
	"errors"
	"sync"
	"time"
)

// Store persists trust state per provider.
type Store interface {
	// Get returns the trust state for a provider.
	Get(providerID string) (*State, error)

	// SetTier sets the trust tier for a provider.
	SetTier(providerID string, tier TrustTier) error

	// SetEconomicTrust sets the economic trust state.
	SetEconomicTrust(providerID string, t *EconomicTrust) error

	// EnrollTPM enrolls a TPM AIK for a provider.
	EnrollTPM(providerID string, t *TPMTrust) error

	// SetKYB sets KYB approval state.
	SetKYB(providerID string, approved bool, approvedBy string) error

	// SlashEconomic marks the provider as slashed and returns the reason.
	SlashEconomic(providerID, reason string) error

	// GetVerifiedQuoteCache returns cached verified quotes for a provider (session-scoped).
	GetVerifiedQuoteCache(providerID, sessionID string) (bool, error)
	CacheVerifiedQuote(providerID, sessionID string) error
}

// InMemoryTrustStore holds trust state in process memory.
type InMemoryTrustStore struct {
	mu    sync.RWMutex
	state map[string]*State // providerID → State

	// quoteCache caches verified TPM quotes per (providerID, sessionID).
	quoteCache    map[string]map[string]time.Time // providerID → (sessionID → verifiedAt)
	quoteCacheTTL time.Duration
}

// NewInMemoryTrustStore creates an empty trust store.
func NewInMemoryTrustStore() *InMemoryTrustStore {
	return &InMemoryTrustStore{
		state:         make(map[string]*State),
		quoteCache:    make(map[string]map[string]time.Time),
		quoteCacheTTL: 10 * time.Minute, // Quote valid for session lifetime
	}
}

// State holds all trust state for a single provider.
type State struct {
	ProviderID   string
	Tier         TrustTier    // Current effective tier (minimum of enrolled tiers)
	Economic     *EconomicTrust
	TPM          *TPMTrust
	KYBApproved  bool
	KYBApprovedBy string

	// AttestedTier is the highest tier this provider has ever demonstrated.
	AttestedTier TrustTier
}

// ErrProviderNotFound is returned when a provider has no trust state.
var ErrProviderNotFound = errors.New("provider trust state not found")

func (s *InMemoryTrustStore) Get(providerID string) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state, ok := s.state[providerID]; ok {
		return state, nil
	}
	return nil, ErrProviderNotFound
}

func (s *InMemoryTrustStore) SetTier(providerID string, tier TrustTier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[providerID]
	if !ok {
		state = &State{ProviderID: providerID}
		s.state[providerID] = state
	}
	state.Tier = tier
	return nil
}

func (s *InMemoryTrustStore) SetEconomicTrust(providerID string, t *EconomicTrust) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[providerID]
	if !ok {
		state = &State{ProviderID: providerID}
		s.state[providerID] = state
	}
	state.Economic = t
	if t.Tier == 0 {
		t.Tier = TierEconomic
	}
	if t.Tier > state.AttestedTier {
		state.AttestedTier = t.Tier
	}
	if t.Tier < state.Tier || state.Tier == 0 {
		state.Tier = t.Tier
	}
	return nil
}

func (s *InMemoryTrustStore) EnrollTPM(providerID string, t *TPMTrust) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[providerID]
	if !ok {
		state = &State{ProviderID: providerID}
		s.state[providerID] = state
	}
	state.TPM = t
	if TierHardware > state.AttestedTier {
		state.AttestedTier = TierHardware
	}
	if TierHardware < state.Tier || state.Tier == 0 {
		state.Tier = TierHardware
	}
	return nil
}

func (s *InMemoryTrustStore) SetKYB(providerID string, approved bool, approvedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[providerID]
	if !ok {
		state = &State{ProviderID: providerID}
		s.state[providerID] = state
	}
	state.KYBApproved = approved
	state.KYBApprovedBy = approvedBy
	if approved && TierKYB > state.AttestedTier {
		state.AttestedTier = TierKYB
	}
	return nil
}

func (s *InMemoryTrustStore) SlashEconomic(providerID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.state[providerID]
	if !ok {
		return ErrProviderNotFound
	}
	if state.Economic == nil {
		state.Economic = &EconomicTrust{Tier: TierEconomic}
	}
	state.Economic.Slashed = true
	state.Economic.SlashReason = reason
	state.Economic.SlashedAt = time.Now()
	// Drop to anonymous tier when slashed
	state.Tier = TierAnonymous
	return nil
}

func (s *InMemoryTrustStore) GetVerifiedQuoteCache(providerID, sessionID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sessions, ok := s.quoteCache[providerID]; ok {
		if verifiedAt, ok := sessions[sessionID]; ok {
			if time.Since(verifiedAt) < s.quoteCacheTTL {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *InMemoryTrustStore) CacheVerifiedQuote(providerID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quoteCache[providerID] == nil {
		s.quoteCache[providerID] = make(map[string]time.Time)
	}
	s.quoteCache[providerID][sessionID] = time.Now()
	return nil
}

// CanRoute returns true if the provider meets the minimum tier for routing.
func (s *State) CanRoute(minTier TrustTier) bool {
	if s == nil {
		return false
	}
	// If slashed, cannot route
	if s.Economic != nil && s.Economic.Slashed {
		return false
	}
	return s.Tier >= minTier
}

// AIKPublicKey extracts the crypto.PublicKey from the stored AIK.
func (s *State) AIKPublicKey() crypto.PublicKey {
	// In production: parse DER-encoded AIK public key
	// Stub: return nil; real impl uses x509.ParsePKIXPublicKey or equivalent
	return nil
}
