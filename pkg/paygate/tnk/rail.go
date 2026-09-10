package tnk

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hivemachine/pkg/paygate/balance"
)

// Rail implements the balance.Rail interface for TNK (Trac token on Ethereum).
// It wraps a TNKRail log listener and credits confirmed deposits into a RailStore.
type Rail struct {
	tnkRail   *TNKRail
	store     *balance.RailStore
	addrs     *DepositAddressManager
	priceFeed TokenPriceFeed
	hotAddr   common.Address
}

// NewRail creates a TNK Rail. store receives credited deposits in cents.
// addrs maps derived deposit addresses to API keys so the log watcher can
// credit the correct account.
func NewRail(tnkRail *TNKRail, store *balance.RailStore, addrs *DepositAddressManager, priceFeed TokenPriceFeed, hotAddr common.Address) *Rail {
	return &Rail{
		tnkRail:   tnkRail,
		store:     store,
		addrs:     addrs,
		priceFeed: priceFeed,
		hotAddr:   hotAddr,
	}
}

// Name returns "tnk".
func (r *Rail) Name() string { return "tnk" }

// Deposit returns the user's derived deposit address (or the hot wallet fallback).
func (r *Rail) Deposit(ctx context.Context, apiKey string, _ int64, currency string) (*balance.DepositResult, error) {
	addr := r.addrs.AddressFor(apiKey)
	if addr == (common.Address{}) {
		addr = r.hotAddr
	}
	return &balance.DepositResult{
		DepositID: addr.Hex(),
		URL:      fmt.Sprintf("ethereum:%s", addr.Hex()),
		Currency: currency,
	}, nil
}

// Balance returns the TNK balance for apiKey in cents.
func (r *Rail) Balance(ctx context.Context, apiKey string) (int64, error) {
	return r.store.Get(apiKey, balance.RailTNK), nil
}

// Withdraw is not yet implemented.
func (r *Rail) Withdraw(ctx context.Context, apiKey string, amountCents int64, destination string) (*balance.WithdrawResult, error) {
	return nil, fmt.Errorf("tnk: withdraw not implemented")
}

// Start begins monitoring the Ethereum chain for TNK deposits to all registered addresses.
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

	strAddrs := make([]string, len(watch))
	for i, a := range watch {
		strAddrs[i] = strings.ToLower(a.Hex())
	}

	settle := func(to common.Address, amountWei int64) error {
		apiKey := r.addrs.Lookup(to)
		if apiKey == "" {
			apiKey = "__hot__"
		}
		cents, err := r.priceFeed.TNKToCents(ctx, big.NewInt(amountWei))
		if err != nil {
			return fmt.Errorf("tnk price conversion: %v", err)
		}
		r.store.AddCredits(apiKey, balance.RailTNK, cents)
		fmt.Printf("[tnk] credited %d cents to %s (%s)\n", cents, apiKey, to.Hex()[:12])
		return nil
	}

	return r.tnkRail.StartMulti(ctx, strAddrs, settle)
}

// Stop halts the log monitor.
func (r *Rail) Stop() { r.tnkRail.Stop() }
