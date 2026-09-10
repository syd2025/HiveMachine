package tap

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hivemachine/pkg/paygate/balance"
)

// PriceFeed converts wei to cents at the current ETH/USD price.
type PriceFeed interface {
	ETHToCents(ctx context.Context, wei *big.Int) (int64, error)
}

// StaticPriceFeed returns a fixed ETH/USD price for dev/test environments.
type StaticPriceFeed struct{ PriceUSD float64 }

func (f *StaticPriceFeed) ETHToCents(_ context.Context, wei *big.Int) (int64, error) {
	if wei == nil || wei.Sign() == 0 {
		return 0, nil
	}
	eth := new(big.Float).SetInt(wei)
	eth.Quo(eth, new(big.Float).SetInt(big.NewInt(1e18)))
	cents := new(big.Float).Mul(eth, big.NewFloat(f.PriceUSD*100))
	c, _ := cents.Int64()
	return c, nil
}

// DepositAddressManager maps derived deposit addresses to API keys.
type DepositAddressManager struct {
	mu    sync.RWMutex
	addrs map[common.Address]string // address → apiKey
	keys  map[string]common.Address // apiKey → address
}

func NewDepositAddressManager() *DepositAddressManager {
	return &DepositAddressManager{
		addrs: make(map[common.Address]string),
		keys:  make(map[string]common.Address),
	}
}

// Register records that addr belongs to apiKey. addr should be a derived
// HD address unique to this apiKey.
func (m *DepositAddressManager) Register(apiKey string, addr common.Address) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addrs[addr] = apiKey
	m.keys[apiKey] = addr
}

// Lookup returns the apiKey that owns addr, or "" if unknown.
func (m *DepositAddressManager) Lookup(addr common.Address) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.addrs[addr]
}

// AddressFor returns the deposit address for apiKey, or zero address if not registered.
func (m *DepositAddressManager) AddressFor(apiKey string) common.Address {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keys[apiKey]
}

// Rail implements the balance.Rail interface for TAP (ERC-20 on Ethereum).
// It wraps a TAPRail log listener and credits confirmed deposits into a RailStore.
type Rail struct {
	tapRail   *TAPRail
	store     *balance.RailStore
	addrs     *DepositAddressManager
	priceFeed PriceFeed
	hotAddr   common.Address
}

// NewRail creates a TAP Rail. store receives credited deposits in cents.
// addrs maps derived deposit addresses to API keys so the log watcher can
// credit the correct account.
func NewRail(tapRail *TAPRail, store *balance.RailStore, addrs *DepositAddressManager, priceFeed PriceFeed, hotAddr common.Address) *Rail {
	return &Rail{
		tapRail:   tapRail,
		store:     store,
		addrs:     addrs,
		priceFeed: priceFeed,
		hotAddr:   hotAddr,
	}
}

// Name returns "tap".
func (r *Rail) Name() string { return "tap" }

// Deposit returns the user's derived deposit address (or the hot wallet fallback).
func (r *Rail) Deposit(ctx context.Context, apiKey string, _ int64, currency string) (*balance.DepositResult, error) {
	addr := r.addrs.AddressFor(apiKey)
	if addr == (common.Address{}) {
		addr = r.hotAddr // fallback to hot wallet
	}
	return &balance.DepositResult{
		DepositID: addr.Hex(),
		URL:      fmt.Sprintf("ethereum:%s", addr.Hex()),
		Currency: currency,
	}, nil
}

// Balance returns the TAP balance for apiKey in cents.
func (r *Rail) Balance(ctx context.Context, apiKey string) (int64, error) {
	return r.store.Get(apiKey, balance.RailTAP), nil
}

// Withdraw is not yet implemented.
func (r *Rail) Withdraw(ctx context.Context, apiKey string, amountCents int64, destination string) (*balance.WithdrawResult, error) {
	return nil, fmt.Errorf("tap: withdraw not implemented")
}

// Settle manually credits a deposit for a known tx (used by the log watcher).
func (r *Rail) Settle(ctx context.Context, txHash string, apiKey string, amountWei int64) error {
	cents, err := r.priceFeed.ETHToCents(ctx, big.NewInt(amountWei))
	if err != nil {
		return fmt.Errorf("tap: price conversion: %w", err)
	}
	r.store.AddCredits(apiKey, balance.RailTAP, cents)
	return nil
}

// Start begins monitoring the Ethereum chain for deposits to all registered addresses.
// It credits confirmed transfers into the RailStore using the priceFeed.
func (r *Rail) Start(ctx context.Context) error {
	r.addrs.mu.RLock()
	watch := make([]common.Address, 0, len(r.addrs.addrs)+1)
	for a := range r.addrs.addrs {
		watch = append(watch, a)
	}
	if r.hotAddr != (common.Address{}) {
		watch = append(watch, r.hotAddr)
	}
	r.addrs.mu.RUnlock()

	// Convert to strings for the lower-level TAP client.
	strAddrs := make([]string, len(watch))
	for i, a := range watch {
		strAddrs[i] = strings.ToLower(a.Hex())
	}

	settle := func(to common.Address, amountWei int64) error {
		apiKey := r.addrs.Lookup(to)
		if apiKey == "" {
			apiKey = "__hot__" // hot wallet deposit — track separately
		}
		cents, err := r.priceFeed.ETHToCents(ctx, big.NewInt(amountWei))
		if err != nil {
			return fmt.Errorf("tap price conversion: %v", err)
		}
		r.store.AddCredits(apiKey, balance.RailTAP, cents)
		fmt.Printf("[tap] credited %d cents to %s (%s)\n", cents, apiKey, to.Hex()[:12])
		return nil
	}

	return r.tapRail.StartMulti(ctx, strAddrs, settle)
}

// Stop halts the log monitor.
func (r *Rail) Stop() { r.tapRail.Stop() }
