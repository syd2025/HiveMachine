package wallet

import (
	"context"
	"fmt"
	"sync"

	"github.com/hivemachine/pkg/paygate/balance"
)

// Service provides wallet-related operations in the gateway context.
// It maps API keys to wallet addresses for deposit flows.
type Service struct {
	mu      sync.RWMutex
	enabled bool
	// apiKey -> chain -> address
	addresses map[string]map[balance.RailType]string
	// rail -> address prefix hint (e.g., for generating deposit addresses)
	railMeta map[balance.RailType]*railMeta
}

type railMeta struct {
	// DepositAddressPrefix is the address that receives deposits for this rail.
	// For TAP: gateway's hot wallet Ethereum address.
	// For TNK: gateway's hot wallet Trac address.
	DepositAddress string
}

// NewService creates a wallet service for the gateway.
// When enabled, it tracks per-API-key deposit addresses for each rail.
func NewService() *Service {
	return &Service{
		enabled:   false,
		addresses: make(map[string]map[balance.RailType]string),
		railMeta:  make(map[balance.RailType]*railMeta),
	}
}

// Enable activates the wallet service.
func (s *Service) Enable() {
	s.mu.Lock()
	s.enabled = true
	s.mu.Unlock()
}

// IsEnabled reports whether the wallet service is active.
func (s *Service) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// RegisterRail records the gateway's deposit address for a rail.
func (s *Service) RegisterRail(rail balance.RailType, depositAddr string) {
	s.mu.Lock()
	s.railMeta[rail] = &railMeta{DepositAddress: depositAddr}
	s.mu.Unlock()
}

// RegisterAddress records a user's deposit address for a specific rail.
// The API key identifies the user.
func (s *Service) RegisterAddress(apiKey string, rail balance.RailType, address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.addresses[apiKey] == nil {
		s.addresses[apiKey] = make(map[balance.RailType]string)
	}
	s.addresses[apiKey][rail] = address
}

// DepositAddress returns the gateway's deposit address for the given rail.
// This is the address users send funds to when depositing via TAP or TNK.
func (s *Service) DepositAddress(ctx context.Context, apiKey string, rail balance.RailType) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// First check if we have a per-user override.
	if userAddr, ok := s.addresses[apiKey]; ok {
		if addr, ok := userAddr[rail]; ok && addr != "" {
			return addr, nil
		}
	}

	// Fall back to the gateway's hot wallet address for this rail.
	meta, ok := s.railMeta[rail]
	if !ok || meta.DepositAddress == "" {
		return "", fmt.Errorf("no deposit address registered for rail %q", rail)
	}
	return meta.DepositAddress, nil
}

// AllDepositAddresses returns all registered deposit addresses for a user.
func (s *Service) AllDepositAddresses(apiKey string) map[balance.RailType]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if addrs, ok := s.addresses[apiKey]; ok {
		result := make(map[balance.RailType]string)
		for k, v := range addrs {
			result[k] = v
		}
		return result
	}
	return nil
}
