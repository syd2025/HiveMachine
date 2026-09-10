// Package wallet provides cryptographic wallet management for HiveMachine.
// It handles mnemonic generation, key derivation, and secure storage.
package wallet

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	// MnemonicWordCount for 256-bit entropy
	MnemonicWordCount = 24
)

// ChainType represents the blockchain for address derivation
type ChainType string

const (
	ChainEthereum ChainType = "ethereum"
	ChainTrac     ChainType = "trac"
)

// WalletSource indicates how the wallet was created
type WalletSource int

const (
	WalletSourceGenerated WalletSource = iota
	WalletSourceImported
)

// Wallet holds cryptographic material and metadata
type Wallet struct {
	ID         WalletID
	Addresses  map[ChainType]string
	Source     WalletSource
	CreatedAt  time.Time
	LastActive time.Time
	Encrypted  bool
	mu         sync.RWMutex
}

// WalletID uniquely identifies a wallet
type WalletID string

// NewWalletID generates a new wallet ID
func NewWalletID() WalletID {
	b := make([]byte, 16)
	rand.Read(b)
	return WalletID(hex.EncodeToString(b))
}

// GetAddress returns the address for a chain
func (w *Wallet) GetAddress(chain ChainType) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	addr, ok := w.Addresses[chain]
	return addr, ok
}

// EncryptedKeyStore stores encrypted key material
type EncryptedKeyStore struct {
	Cipher       string `json:"cipher"`
	KDF          string `json:"kdf"`
	Salt         []byte `json:"salt"`
	Nonce        []byte `json:"nonce"`
	CipherText   []byte `json:"ciphertext"`
	// Argon2id parameters (required for decryption)
	Memory      uint32 `json:"memory"`      // kibibytes
	Iterations  uint32 `json:"iterations"`  // iterations
	Parallelism uint32 `json:"parallelism"` // threads
}


// Errors
var (
	ErrInvalidMnemonic   = errors.New("wallet: invalid mnemonic phrase")
	ErrInvalidPassword   = errors.New("wallet: invalid password")
	ErrInvalidSeed       = errors.New("wallet: invalid seed (must be 64 bytes)")
	ErrDeriveFailed      = errors.New("wallet: key derivation failed")
	ErrWalletLocked      = errors.New("wallet: locked")
	ErrNoWallet         = errors.New("wallet: not found")
	ErrEncryptionFailed  = errors.New("wallet: encryption failed")
	ErrDecryptionFailed  = errors.New("wallet: decryption failed")
	ErrAddressDerivation = errors.New("wallet: address derivation failed")
)

// WalletService interface for wallet operations
type WalletService interface {
	CreateWallet(ctx context.Context, password string) (*Wallet, error)
	RestoreWallet(ctx context.Context, mnemonic string, password string) (*Wallet, error)
	GetMnemonic(ctx context.Context, password string) (string, error)
	GetAddress(ctx context.Context, chain ChainType) (string, error)
	ListAddresses(ctx context.Context) (map[ChainType]string, error)
	Lock(ctx context.Context) error
	Unlock(ctx context.Context, password string) error
	IsUnlocked() bool
}

// DefaultWalletService provides a default implementation
type DefaultWalletService struct {
	wallet   *Wallet
	keystore *EncryptedKeyStore
	password string
	unlocked bool
	mu       sync.RWMutex
}

// NewWalletService creates a new wallet service
func NewWalletService() *DefaultWalletService {
	return &DefaultWalletService{}
}

// CreateWallet generates a new wallet with the given password
func (s *DefaultWalletService) CreateWallet(ctx context.Context, password string) (*Wallet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate mnemonic
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		return nil, err
	}

	// Encrypt and store
	keystore, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	// Create wallet
	wallet := &Wallet{
		ID:        NewWalletID(),
		Addresses: make(map[ChainType]string),
		Source:    WalletSourceGenerated,
		CreatedAt: time.Now(),
		Encrypted: true,
	}

	s.wallet = wallet
	s.keystore = keystore
	s.password = password
	s.unlocked = true

	// Derive addresses
	if err := s.deriveAddresses(mnemonic); err != nil {
		return nil, err
	}

	return wallet, nil
}

// RestoreWallet creates a wallet from an existing mnemonic
func (s *DefaultWalletService) RestoreWallet(ctx context.Context, mnemonic string, password string) (*Wallet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate mnemonic
	if !ValidateMnemonic(mnemonic) {
		return nil, ErrInvalidMnemonic
	}

	// Encrypt and store
	keystore, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	// Create wallet
	wallet := &Wallet{
		ID:        NewWalletID(),
		Addresses: make(map[ChainType]string),
		Source:    WalletSourceImported,
		CreatedAt: time.Now(),
		Encrypted: true,
	}

	s.wallet = wallet
	s.keystore = keystore
	s.password = password

	// Derive addresses from the mnemonic
	if err := s.deriveAddresses(mnemonic); err != nil {
		return nil, fmt.Errorf("wallet: failed to derive addresses: %w", err)
	}
	return wallet, nil
	return wallet, nil
}

// deriveAddresses derives addresses for all supported chains using BIP-32 HD derivation.
func (s *DefaultWalletService) deriveAddresses(mnemonic string) error {
	seed, err := MnemonicToSeed(mnemonic, s.password)
	if err != nil {
		return fmt.Errorf("wallet: failed to derive seed: %w", err)
	}

	// Derive Ethereum address (m/44'/60'/0'/0/0).
	ethAddr, err := DeriveAddressFromSeed(seed, ChainEthereum)
	if err != nil {
		return fmt.Errorf("wallet: failed to derive ethereum address: %w", err)
	}
	s.wallet.Addresses[ChainEthereum] = ethAddr

	// Derive Trac address (m/44'/966'/0'/0/0).
	tracAddr, err := DeriveAddressFromSeed(seed, ChainTrac)
	if err != nil {
		return fmt.Errorf("wallet: failed to derive trac address: %w", err)
	}
	s.wallet.Addresses[ChainTrac] = tracAddr

	return nil
}

// EncryptMnemonic encrypts a mnemonic with a password using AES-256-GCM + Argon2id.
func EncryptMnemonic(mnemonic string, password string) (*EncryptedKeyStore, error) {
	// Generate salt for Argon2id
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	// Derive key using Argon2id
	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, fmt.Errorf("wallet: key derivation failed: %w", err)
	}

	// Encrypt with AES-256-GCM
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(mnemonic), nil)

	return &EncryptedKeyStore{
		Cipher:       "aes-256-gcm",
		KDF:          "argon2id",
		Salt:         salt,
		Nonce:        nonce[:gcm.NonceSize()],
		CipherText:   ciphertext[gcm.NonceSize():],
		Memory:       argon2Memory,
		Iterations:   argon2Iterations,
		Parallelism:  argon2Parallelism,
	}, nil
}

// DecryptMnemonic decrypts an encrypted mnemonic using the stored Argon2id parameters.
func DecryptMnemonic(ks *EncryptedKeyStore, password string) (string, error) {
	// Use stored Argon2id parameters from the keystore.
	// If using legacy keystore without params, use safe defaults.
	mem := ks.Memory
	iters := ks.Iterations
	par := ks.Parallelism
	if mem == 0 {
		// Legacy keystore: use safe defaults for stored (pre-Argon2id) files.
		// We still derive with the new params to maintain forward compatibility.
		mem = argon2Memory
		iters = argon2Iterations
		par = argon2Parallelism
	}
	key := argon2.IDKey([]byte(password), ks.Salt, uint32(iters), uint32(mem), uint8(par), argon2KeyLen)

	// Decrypt
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	plaintext, err := gcm.Open(nil, ks.Nonce, ks.CipherText, nil)
	if err != nil {
		return "", ErrInvalidPassword
	}

	return string(plaintext), nil
}

// Argon2id parameters for key derivation.
// These values are from the OWASP recommendations for Argon2id (2023).
const (
	argon2Memory      = 64 * 1024 // 64 MiB in KiB
	argon2Iterations  = 3
	argon2Parallelism = 4
	argon2KeyLen     = 32        // bytes for AES-256
)

// deriveKey derives a 32-byte key from password and salt using Argon2id.
func deriveKey(password string, salt []byte) ([]byte, error) {
	return argon2.IDKey(
		[]byte(password),
		salt,
		argon2Iterations,
		argon2Memory,
		argon2Parallelism,
		argon2KeyLen,
	), nil
}

// GetMnemonic returns the mnemonic
func (s *DefaultWalletService) GetMnemonic(ctx context.Context, password string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.unlocked {
		return "", ErrWalletLocked
	}

	if s.keystore == nil {
		return "", ErrNoWallet
	}

	return DecryptMnemonic(s.keystore, password)
}

// GetAddress returns the address for a chain
func (s *DefaultWalletService) GetAddress(ctx context.Context, chain ChainType) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.wallet == nil {
		return "", ErrNoWallet
	}

	addr, ok := s.wallet.Addresses[chain]
	if !ok {
		return "", fmt.Errorf("address for chain %s not found", chain)
	}

	return addr, nil
}

// ListAddresses returns all addresses
func (s *DefaultWalletService) ListAddresses(ctx context.Context) (map[ChainType]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.wallet == nil {
		return nil, ErrNoWallet
	}

	result := make(map[ChainType]string)
	for k, v := range s.wallet.Addresses {
		result[k] = v
	}
	return result, nil
}

// Lock locks the wallet
func (s *DefaultWalletService) Lock(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unlocked = false
	return nil
}

// Unlock unlocks the wallet with the password
func (s *DefaultWalletService) Unlock(ctx context.Context, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.keystore == nil {
		return ErrNoWallet
	}

	// Verify password by attempting decryption
	_, err := DecryptMnemonic(s.keystore, password)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}

	s.password = password
	s.unlocked = true

	return nil
}

// IsUnlocked returns whether the wallet is unlocked
func (s *DefaultWalletService) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unlocked
}
