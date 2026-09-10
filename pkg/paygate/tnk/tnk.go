// Package tnk implements the TNK (Trac) payment rail.
// TNK is an ERC-20 token on Ethereum; this package monitors the Trac contract
// for Transfer events and credits deposits into a RailStore.
package tnk

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Config configures the TNK rail.
type Config struct {
	// Trac token contract address on Ethereum.
	TokenContract common.Address
	// Required block confirmations before crediting a deposit.
	DepositConfirmations uint64
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		// OriginTrail (TRAC) token on Ethereum mainnet.
		TokenContract:        common.HexToAddress("0xd44E771D3995D86e33fBD9b6bE14f20eF67946E8"),
		DepositConfirmations: 12,
	}
}

// TokenPriceFeed converts TNK (Trac token wei) to cents at the current price.
type TokenPriceFeed interface {
	TNKToCents(ctx context.Context, wei *big.Int) (int64, error)
}

// StaticTokenPriceFeed returns a fixed TNK/USD price for dev/test.
type StaticTokenPriceFeed struct{ PriceUSD float64 }

func (f *StaticTokenPriceFeed) TNKToCents(_ context.Context, wei *big.Int) (int64, error) {
	if wei == nil || wei.Sign() == 0 {
		return 0, nil
	}
	tnk := new(big.Float).SetInt(wei)
	tnk.Quo(tnk, new(big.Float).SetInt(big.NewInt(1e18)))
	cents := new(big.Float).Mul(tnk, big.NewFloat(f.PriceUSD*100))
	c, _ := cents.Int64()
	return c, nil
}

// DepositAddressManager maps derived deposit addresses to API keys.
type DepositAddressManager struct {
	mu    sync.RWMutex
	addrs map[common.Address]string
	keys  map[string]common.Address
}

func NewDepositAddressManager() *DepositAddressManager {
	return &DepositAddressManager{
		addrs: make(map[common.Address]string),
		keys:  make(map[string]common.Address),
	}
}

func (m *DepositAddressManager) Register(apiKey string, addr common.Address) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addrs[addr] = apiKey
	m.keys[apiKey] = addr
}

func (m *DepositAddressManager) Lookup(addr common.Address) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.addrs[addr]
}

func (m *DepositAddressManager) AddressFor(apiKey string) common.Address {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keys[apiKey]
}

// BalanceStore credits a deposit for an API key in cents.
type BalanceStore interface {
	AddCredits(apiKey string, cents int64)
}

// DepositTracker records processed deposit logs to avoid double-crediting.
type DepositTracker interface {
	IsProcessed(txhash string, logIndex uint) bool
	MarkProcessed(txhash string, logIndex uint)
}

// TNKRail monitors the Trac (TNK) ERC-20 contract for Transfer events.
type TNKRail struct {
	cfg     *Config
	client  *ethclient.Client
	balance BalanceStore
	deposit DepositTracker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewTNKRail creates a new TNK rail.
func NewTNKRail(cfg *Config, client *ethclient.Client, balance BalanceStore, deposit DepositTracker) (*TNKRail, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &TNKRail{
		cfg:     cfg,
		client:  client,
		balance: balance,
		deposit: deposit,
		stopCh:  make(chan struct{}),
	}, nil
}

// CreditDeposit directly credits a deposit (used by the log monitor).
func (r *TNKRail) CreditDeposit(apiKey string, amountCents int64) error {
	if r.balance == nil {
		return nil
	}
	r.balance.AddCredits(apiKey, amountCents)
	return nil
}

// creditCallback is called for each confirmed deposit.
type creditCallback func(to common.Address, amountWei int64) error

// Start begins monitoring the TNK contract for Transfer events to depositAddr.
// It is equivalent to StartMulti for a single address.
func (r *TNKRail) Start(ctx context.Context, depositAddr string, credit func(to common.Address, amountWei int64) error) error {
	return r.StartMulti(ctx, []string{depositAddr}, credit)
}

// StartMulti monitors multiple deposit addresses.
func (r *TNKRail) StartMulti(ctx context.Context, depositAddrs []string, credit func(to common.Address, amountWei int64) error) error {
	addrs := make([]common.Address, len(depositAddrs))
	for i, s := range depositAddrs {
		addrs[i] = common.HexToAddress(s)
	}

	header, err := r.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("getting block number: %w", err)
	}
	fromBlock := new(big.Int).Sub(header.Number, big.NewInt(int64(r.cfg.DepositConfirmations)))

	query := ethereum.FilterQuery{
		Addresses: []common.Address{r.cfg.TokenContract},
		FromBlock: fromBlock,
		ToBlock:   nil,
		Topics:    [][]common.Hash{{transferEventID}},
	}

	logs := make(chan types.Log)
	sub, err := r.client.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("subscribing to Transfer events: %w", err)
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer sub.Unsubscribe()
		for {
			select {
			case <-r.stopCh:
				return
			case <-ctx.Done():
				return
			case log := <-logs:
				r.handleLogMulti(ctx, &log, addrs, credit)
			}
		}
	}()
	return nil
}

// Transfer event signature (same as standard ERC-20).
var transferEventID = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

func parseTransferLog(log *types.Log) (to common.Address, value *big.Int, err error) {
	if len(log.Topics) < 3 {
		return common.Address{}, nil, fmt.Errorf("insufficient topics in log")
	}
	to = common.HexToAddress(log.Topics[2].Hex())
	if len(log.Data) < 32 {
		return to, big.NewInt(0), nil
	}
	value = new(big.Int).SetBytes(log.Data[:32])
	return to, value, nil
}

func (r *TNKRail) handleLogMulti(ctx context.Context, log *types.Log, depositAddrs []common.Address, credit func(to common.Address, amountWei int64) error) {
	to, value, err := parseTransferLog(log)
	if err != nil {
		fmt.Printf("[tnk] parse error: %v\n", err)
		return
	}
	isWatched := false
	for _, addr := range depositAddrs {
		if to == addr {
			isWatched = true
			break
		}
	}
	if !isWatched {
		return
	}
	header, err := r.client.HeaderByNumber(ctx, nil)
	if err != nil {
		fmt.Printf("[tnk] header error: %v\n", err)
		return
	}
	confirmations := header.Number.Uint64() - log.BlockNumber
	if confirmations < r.cfg.DepositConfirmations {
		return
	}
	if r.deposit.IsProcessed(log.TxHash.Hex(), log.Index) {
		return
	}
	if err := credit(to, value.Int64()); err != nil {
		fmt.Printf("[tnk] credit error: %v\n", err)
		return
	}
	r.deposit.MarkProcessed(log.TxHash.Hex(), log.Index)
}

// Stop halts the log monitor.
func (r *TNKRail) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

// CheckDeposit verifies a specific TNK deposit transaction on-chain.
func CheckDeposit(ctx context.Context, client *ethclient.Client, cfg *Config, txHash string, logIndex uint, priceFeed TokenPriceFeed) (bool, int64, error) {
	hash := common.HexToHash(txHash)
	receipt, err := client.TransactionReceipt(ctx, hash)
	if err != nil {
		return false, 0, fmt.Errorf("transaction receipt: %w", err)
	}
	for _, log := range receipt.Logs {
		if int(log.Index) != int(logIndex) {
			continue
		}
		if log.Address != cfg.TokenContract {
			continue
		}
		if len(log.Topics) < 3 {
			continue
		}
		to, value, err := parseTransferLog(log)
		if err != nil {
			continue
		}
		header, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			return false, 0, err
		}
		confirmations := header.Number.Uint64() - log.BlockNumber
		if confirmations < cfg.DepositConfirmations {
			return false, 0, nil
		}
		cents, err := priceFeed.TNKToCents(ctx, value)
		if err != nil {
			return false, 0, err
		}
		_ = to
		return true, cents, nil
	}
	return false, 0, fmt.Errorf("log not found at index %d", logIndex)
}

// ERC20ABI is exported for external use.
var ERC20ABI abi.ABI

func init() {
	const erc20ABI = `[{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"type":"event"}]`
	var err error
	ERC20ABI, err = abi.JSON(strings.NewReader(erc20ABI))
	if err != nil {
		panic("failed to parse ERC20 ABI: " + err.Error())
	}
}

// InMemoryDepositTracker records processed deposits in process memory.
type InMemoryDepositTracker struct {
	processed map[string]bool
}

func NewInMemoryDepositTracker() *InMemoryDepositTracker {
	return &InMemoryDepositTracker{processed: make(map[string]bool)}
}

func (t *InMemoryDepositTracker) key(txhash string, logIndex uint) string {
	return txhash + ":" + fmt.Sprint(logIndex)
}

func (t *InMemoryDepositTracker) IsProcessed(txhash string, logIndex uint) bool {
	return t.processed[t.key(txhash, logIndex)]
}

func (t *InMemoryDepositTracker) MarkProcessed(txhash string, logIndex uint) {
	t.processed[t.key(txhash, logIndex)] = true
}
