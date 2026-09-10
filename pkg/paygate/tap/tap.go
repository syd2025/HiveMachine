package tap

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Config configures the TAP rail.
type Config struct {
	RPCURL                string
	TAPContract           common.Address
	DepositConfirmations  uint64
	PollInterval          time.Duration
}

func DefaultConfig() *Config {
	return &Config{
		DepositConfirmations: 12,
		PollInterval:        15 * time.Second,
	}
}

type TAPRail struct {
	cfg     *Config
	client  *ethclient.Client
	balance BalanceStore
	deposit DepositTracker
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type BalanceStore interface {
	Add(ctx context.Context, apiKey string, amountWei int64) error
}

type DepositTracker interface {
	IsProcessed(txhash string, logIndex uint) bool
	MarkProcessed(txhash string, logIndex uint)
}

func NewTAPRail(cfg *Config, client *ethclient.Client, balance BalanceStore, deposit DepositTracker) (*TAPRail, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &TAPRail{
		cfg:     cfg,
		client:  client,
		balance: balance,
		deposit: deposit,
		stopCh:  make(chan struct{}),
	}, nil
}

var transferEventID = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

func parseTransferLog(log *types.Log) (to common.Address, value *big.Int, err error) {
	if len(log.Topics) < 3 {
		return common.Address{}, nil, fmt.Errorf("Transfer event has fewer than 3 topics")
	}
	to = common.HexToAddress(log.Topics[2].Hex())
	if len(log.Data) < 32 {
		return common.Address{}, nil, fmt.Errorf("Transfer event data too short")
	}
	value = new(big.Int).SetBytes(log.Data[:32])
	return to, value, nil
}

type InMemoryDepositTracker struct {
	mu        sync.RWMutex
	processed map[string]bool
}

func NewInMemoryDepositTracker() *InMemoryDepositTracker {
	return &InMemoryDepositTracker{processed: make(map[string]bool)}
}

func (t *InMemoryDepositTracker) key(txhash string, logIndex uint) string {
	return txhash + ":" + fmt.Sprint(logIndex)
}

func (t *InMemoryDepositTracker) IsProcessed(txhash string, logIndex uint) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.processed[t.key(txhash, logIndex)]
}

func (t *InMemoryDepositTracker) MarkProcessed(txhash string, logIndex uint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.processed[t.key(txhash, logIndex)] = true
}

// creditCallback is called for each confirmed deposit.
// to is the recipient address parsed from the Transfer event.
type creditCallback func(to common.Address, amountWei int64) error

// Start begins monitoring the TAP contract for Transfer events to depositAddr.
// It is equivalent to StartMulti(addrs, credit) when called with a single address.
func (r *TAPRail) Start(ctx context.Context, depositAddr string, credit func(to common.Address, amountWei int64) error) error {
	return r.StartMulti(ctx, []string{depositAddr}, credit)
}

// StartMulti monitors multiple deposit addresses and calls credit for each confirmed deposit.
func (r *TAPRail) StartMulti(ctx context.Context, depositAddrs []string, credit func(to common.Address, amountWei int64) error) error {
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
		Addresses: []common.Address{r.cfg.TAPContract},
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

func (r *TAPRail) handleLogMulti(ctx context.Context, log *types.Log, depositAddrs []common.Address, credit func(to common.Address, amountWei int64) error) {
	to, value, err := parseTransferLog(log)
	if err != nil {
		fmt.Printf("[tap] parse error: %v\n", err)
		return
	}
	// Check if this log is for one of our watched addresses.
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
		fmt.Printf("[tap] header error: %v\n", err)
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
		fmt.Printf("[tap] credit error: %v\n", err)
		return
	}
	r.deposit.MarkProcessed(log.TxHash.Hex(), log.Index)
}

func (r *TAPRail) handleLog(ctx context.Context, log *types.Log, depositAddr common.Address, credit func(apiKey string, amountWei int64) error) {
	to, value, err := parseTransferLog(log)
	if err != nil {
		fmt.Printf("[tap] parse error: %v\n", err)
		return
	}
	if to != depositAddr {
		return
	}
	header, err := r.client.HeaderByNumber(ctx, nil)
	if err != nil {
		fmt.Printf("[tap] header error: %v\n", err)
		return
	}
	confirmations := header.Number.Uint64() - log.BlockNumber
	if confirmations < r.cfg.DepositConfirmations {
		return
	}
	if r.deposit.IsProcessed(log.TxHash.Hex(), log.Index) {
		return
	}
	if err := credit("", value.Int64()); err != nil {
		fmt.Printf("[tap] credit error: %v\n", err)
		return
	}
	r.deposit.MarkProcessed(log.TxHash.Hex(), log.Index)
}

func (r *TAPRail) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

func (r *TAPRail) CreditDeposit(ctx context.Context, apiKey string, amountWei int64) error {
	if r.balance != nil {
		return r.balance.Add(ctx, apiKey, amountWei)
	}
	return nil
}

func CheckDeposit(ctx context.Context, client *ethclient.Client, cfg *Config, txHash string, logIndex uint) (bool, int64, error) {
	hash := common.HexToHash(txHash)
	receipt, err := client.TransactionReceipt(ctx, hash)
	if err != nil {
		return false, 0, fmt.Errorf("getting receipt: %w", err)
	}
	if int(logIndex) >= len(receipt.Logs) {
		return false, 0, fmt.Errorf("log index out of range")
	}
	log := receipt.Logs[logIndex]
	to, value, err := parseTransferLog(log)
	if err != nil {
		return false, 0, fmt.Errorf("parsing log: %w", err)
	}
	header, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	confirmations := header.Number.Uint64() - log.BlockNumber
	if confirmations < cfg.DepositConfirmations {
		return false, 0, nil
	}
	_ = to
	return true, value.Int64(), nil
}

// ERC20ABI is exported so callers can use it to decode.
var ERC20ABI abi.ABI

func init() {
	var err error
	ERC20ABI, err = abi.JSON(strings.NewReader(`[{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`))
	if err != nil {
		panic("invalid ERC20 ABI: " + err.Error())
	}
}
