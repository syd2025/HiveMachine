// Package chain provides Ethereum (TAP) and Trac (TNK) blockchain payment integration.
package chain

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	// ChainID definitions
	ChainIDEthereum = 1
	ChainIDTrac    = 999999 // Placeholder - would need actual Trac chain ID

	// Address length (Ethereum addresses are 20 bytes)
	AddressLength = 20

	// Confirmations required before crediting
	MinConfirmations = 6
)

// ChainType represents the blockchain type
type ChainType string

const (
	ChainTypeEthereum ChainType = "ethereum"
	ChainTypeTrac     ChainType = "trac"
)

// Deposit represents an on-chain deposit
type Deposit struct {
	ID            DepositID
	Chain         ChainType
	Address       string    // Destination address on this chain
	TxHash        string    // Transaction hash
	FromAddress   string    // Sender address
	AmountWei     *big.Int // Amount in smallest unit (wei for ETH/TAP)
	AmountUSD     float64   // USD value at time of deposit
	Confirmations int       // Current confirmations
	Status        DepositStatus
	BlockNumber   uint64    // Block where deposit was mined
	BlockHash    string
	Timestamp    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DepositID uniquely identifies a deposit
type DepositID string

// DepositStatus represents the status of a deposit
type DepositStatus int

const (
	DepositStatusPending    DepositStatus = iota // Awaiting confirmation
	DepositStatusConfirming                     // In progress
	DepositStatusConfirmed                     // Sufficient confirmations
	DepositStatusCompleted                    // Credited to user balance
	DepositStatusFailed                      // Failed/ignored
)

func (s DepositStatus) String() string {
	switch s {
	case DepositStatusPending:
		return "pending"
	case DepositStatusConfirming:
		return "confirming"
	case DepositStatusConfirmed:
		return "confirmed"
	case DepositStatusCompleted:
		return "completed"
	case DepositStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// DepositRequest represents a deposit request
type DepositRequest struct {
	Chain   ChainType
	Amount  *big.Int // In wei/smallest unit
	Address string    // Destination address
}

// DepositResult is the result of initiating a deposit
type DepositResult struct {
	DepositID DepositID
	TxHash   string
	Amount   *big.Int
	Status   DepositStatus
}

// ChainClient interface for blockchain operations
type ChainClient interface {
	// Chain identification
	ChainType() ChainType
	ChainID() int64

	// Address operations
	DeriveAddress(seed []byte, path string) (string, error)
	ValidateAddress(addr string) error

	// Balance operations
	GetBalance(ctx context.Context, address string) (*big.Int, error)
	GetBalanceWithBlock(ctx context.Context, address string) (*BalanceResult, error)

	// Transaction operations
	SendTransaction(ctx context.Context, req *TransactionRequest) (*TransactionResult, error)
	GetTransaction(ctx context.Context, txHash string) (*Transaction, error)

	// Deposit monitoring
	WatchDeposits(ctx context.Context, addresses []string, handler DepositHandler) error
	GetDepositEvents(ctx context.Context, address string, fromBlock uint64) ([]*Deposit, error)

	// Block operations
	GetBlockNumber(ctx context.Context) (uint64, error)
	GetBlock(ctx context.Context, blockNumber uint64) (*Block, error)
}

// BalanceResult includes block number for confirmation tracking
type BalanceResult struct {
	Balance    *big.Int
	BlockNumber uint64
}

// TransactionRequest for sending transactions
type TransactionRequest struct {
	From     string
	To       string
	Amount   *big.Int
	Data     []byte // For contract calls
	GasLimit uint64
	GasPrice *big.Int
}

// TransactionResult from a sent transaction
type TransactionResult struct {
	TxHash   string
	From     string
	To       string
	Amount   *big.Int
	GasUsed  uint64
	GasPrice *big.Int
	Status   bool
	BlockNumber uint64
}

// Transaction represents a blockchain transaction
type Transaction struct {
	Hash        string
	From        string
	To          string
	Amount      *big.Int
	GasUsed     uint64
	GasPrice    *big.Int
	Status      bool
	BlockNumber uint64
	BlockHash   string
	Timestamp   time.Time
	Confirmations int
}

// Block represents a blockchain block
type Block struct {
	Number       uint64
	Hash         string
	ParentHash   string
	Timestamp    time.Time
	Transactions []string
}

// DepositHandler is called when a deposit is detected
type DepositHandler interface {
	HandleDeposit(ctx context.Context, deposit *Deposit) error
}

// DepositHandlerFunc is a function-based deposit handler
type DepositHandlerFunc func(ctx context.Context, deposit *Deposit) error

func (f DepositHandlerFunc) HandleDeposit(ctx context.Context, deposit *Deposit) error {
	return f(ctx, deposit)
}

// Errors
var (
	ErrInvalidAddress      = errors.New("chain: invalid address format")
	ErrInsufficientBalance = errors.New("chain: insufficient balance")
	ErrTransactionFailed  = errors.New("chain: transaction failed")
	ErrNotConfirmed       = errors.New("chain: deposit not confirmed")
	ErrChainUnavailable   = errors.New("chain: chain unavailable")
)

// ValidateAddress validates an Ethereum-style address
func ValidateAddress(addr string) error {
	if len(addr) != 42 {
		return fmt.Errorf("%w: expected 42 chars, got %d", ErrInvalidAddress, len(addr))
	}
	if addr[:2] != "0x" {
		return fmt.Errorf("%w: must start with 0x", ErrInvalidAddress)
	}
	_, err := hex.DecodeString(addr[2:])
	if err != nil {
		return fmt.Errorf("%w: invalid hex: %v", ErrInvalidAddress, err)
	}
	return nil
}

// ParseAddress parses and validates an address
func ParseAddress(addr string) ([]byte, error) {
	if err := ValidateAddress(addr); err != nil {
		return nil, err
	}
	return hex.DecodeString(addr[2:])
}

// FormatAddress formats bytes as an address
func FormatAddress(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}

// ParseAmount parses an amount string in various formats
func ParseAmount(s string, decimals int) (*big.Int, error) {
	// Try parsing as integer first
	if amount, ok := new(big.Int).SetString(s, 10); ok {
		// Apply decimals multiplier
		multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
		return amount.Mul(amount, multiplier), nil
	}

	// Try parsing as float and convert
	var value float64
	_, err := fmt.Sscanf(s, "%f", &value)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	// Convert float to wei
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	floatWei := big.NewInt(int64(value * float64(decimals)))
	return floatWei.Mul(floatWei, multiplier), nil
}

// FormatAmount formats a wei amount to ETH/TAP string
func FormatAmount(wei *big.Int, decimals int) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)

	whole := new(big.Int).Div(wei, divisor)
	remainder := new(big.Int).Mod(wei, divisor)

	remainderStr := fmt.Sprintf("%d", remainder)
	if len(remainderStr) < decimals {
		remainderStr = fmt.Sprintf("%0"+fmt.Sprintf("%d", decimals)+"s", remainderStr)
	}

	return fmt.Sprintf("%s.%s", whole.String(), remainderStr)
}

// WeiToUSD converts wei to USD using price (simplified)
func WeiToUSD(wei *big.Int, priceUSD float64) float64 {
	// Convert wei to ETH (1 ETH = 1e18 wei)
	eth := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e18))
	usd := new(big.Float).Mul(eth, big.NewFloat(priceUSD))
	f64, _ := usd.Float64()
	return f64
}

// DepositEvent from chain logs
type DepositEvent struct {
	From    string
	To      string
	Amount  *big.Int
	TxHash  string
	Block   uint64
	Index   uint
}

// ToDeposit converts an event to a Deposit
func (e *DepositEvent) ToDeposit(chain ChainType, address string) *Deposit {
	return &Deposit{
		Chain:       chain,
		Address:     address,
		TxHash:      e.TxHash,
		FromAddress: e.From,
		AmountWei:   e.Amount,
		BlockNumber: e.Block,
		Status:      DepositStatusPending,
	}
}

// DepositStore interface for persisting deposits
type DepositStore interface {
	Save(ctx context.Context, deposit *Deposit) error
	Get(ctx context.Context, id DepositID) (*Deposit, error)
	GetByTxHash(ctx context.Context, chain ChainType, txHash string) (*Deposit, error)
	ListPending(ctx context.Context, chain ChainType) ([]*Deposit, error)
	UpdateConfirmations(ctx context.Context, id DepositID, confirmations int, status DepositStatus) error
	MarkCompleted(ctx context.Context, id DepositID) error
}

// ChainConfig holds configuration for a chain client
type ChainConfig struct {
	RPCURL       string
	ChainID      int64
	ChainType    ChainType
	Confirmations int
	StartBlock   uint64
}

// EthereumConfig returns config for Ethereum mainnet
func EthereumConfig(rpcURL string) *ChainConfig {
	return &ChainConfig{
		RPCURL:       rpcURL,
		ChainID:      1,
		ChainType:    ChainTypeEthereum,
		Confirmations: 6,
		StartBlock:   0,
	}
}

// TracConfig returns config for Trac chain
func TracConfig(rpcURL string) *ChainConfig {
	return &ChainConfig{
		RPCURL:       rpcURL,
		ChainID:      999999,
		ChainType:    ChainTypeTrac,
		Confirmations: 1, // Faster for native chain
		StartBlock:   0,
	}
}

// MarshalJSON for DepositStatus
func (s DepositStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON for DepositStatus
func (s *DepositStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	switch str {
	case "pending":
		*s = DepositStatusPending
	case "confirming":
		*s = DepositStatusConfirming
	case "confirmed":
		*s = DepositStatusConfirmed
	case "completed":
		*s = DepositStatusCompleted
	case "failed":
		*s = DepositStatusFailed
	default:
		*s = DepositStatusPending
	}
	return nil
}
