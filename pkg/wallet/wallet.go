// Package wallet provides cryptographic wallet management for HiveMachine.
// It handles mnemonic generation, key derivation, and secure storage.
package wallet

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
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
	Cipher     string `json:"cipher"`
	KDF        string `json:"kdf"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	CipherText []byte `json:"ciphertext"`
}

// BIP39 wordlist (simplified - first 100 words)
// Full implementation would use the complete wordlist
var bip39Wordlist = []string{
	"abandon", "ability", "able", "about", "above", "absent", "absorb", "abstract",
	"absurd", "abuse", "access", "accident", "account", "accuse", "achieve", "acid",
	"acoustic", "acquire", "across", "act", "action", "actor", "actress", "actual",
	"adapt", "add", "addict", "address", "adjust", "admit", "adult", "advance",
	"advice", "aerobic", "affair", "afford", "afraid", "again", "age", "agent",
	"agree", "ahead", "aim", "air", "airport", "aisle", "alarm", "album",
	"alcohol", "alert", "alien", "all", "alley", "allow", "almost", "alone",
	"alpha", "already", "also", "alter", "always", "amateur", "amazing", "among",
	"amount", "amused", "analyst", "anchor", "ancient", "anger", "angle", "angry",
	"animal", "ankle", "announce", "annual", "another", "answer", "antenna", "antique",
	"anxiety", "any", "apart", "apology", "appear", "apple", "approve", "april",
	"arch", "arctic", "area", "arena", "argue", "arm", "armed", "armor",
	"army", "around", "arrange", "arrest", "arrive", "arrow", "art", "artefact",
}

// BIP39Mnemonic represents a BIP39 mnemonic phrase
type BIP39Mnemonic struct {
	words []string
}

// GenerateMnemonic creates a new random mnemonic
func GenerateMnemonic() (string, error) {
	// Generate 32 bytes of entropy
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("failed to generate entropy: %w", err)
	}

	// Convert entropy to words (simplified - real implementation uses checksum)
	words := make([]string, 24)
	for i := 0; i < 24; i++ {
		// Take 11 bits for each word
		idx := int(entropy[i*4/3]) % len(bip39Wordlist)
		words[i] = bip39Wordlist[idx]
	}

	return joinWords(words), nil
}

// joinWords joins mnemonic words with spaces
func joinWords(words []string) string {
	result := ""
	for i, w := range words {
		if i > 0 {
			result += " "
		}
		result += w
	}
	return result
}

// ValidateMnemonic validates a mnemonic phrase (simplified)
func ValidateMnemonic(mnemonic string) bool {
	if mnemonic == "" {
		return false
	}
	words := splitMnemonic(mnemonic)
	if len(words) != 24 {
		return false
	}
	// Check each word is in wordlist
	wordSet := make(map[string]bool)
	for _, w := range bip39Wordlist {
		wordSet[w] = true
	}
	for _, w := range words {
		if !wordSet[w] {
			return false
		}
	}
	return true
}

// splitMnemonic splits a mnemonic into words
func splitMnemonic(mnemonic string) []string {
	words := make([]string, 0, 24)
	word := ""
	for _, c := range mnemonic {
		if c == ' ' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(c)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

// MnemonicWords splits a mnemonic into words
func MnemonicWords(mnemonic string) []string {
	return splitMnemonic(mnemonic)
}

// MnemonicToSeed derives a seed from mnemonic (simplified)
func MnemonicToSeed(mnemonic string, passphrase string) ([]byte, error) {
	// Simplified: use mnemonic + passphrase as seed input
	// Real implementation uses PBKDF2 with proper parameters
	data := []byte(mnemonic + "mnemonic" + passphrase)
	hash := sha256.Sum256(data)
	return hash[:], nil
}

// Errors
var (
	ErrInvalidMnemonic   = errors.New("wallet: invalid mnemonic phrase")
	ErrInvalidPassword   = errors.New("wallet: invalid password")
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
	s.unlocked = true

	// Derive addresses
	if err := s.deriveAddresses(mnemonic); err != nil {
		return nil, err
	}

	return wallet, nil
}

// deriveAddresses derives addresses for all supported chains
func (s *DefaultWalletService) deriveAddresses(mnemonic string) error {
	seed, err := MnemonicToSeed(mnemonic, s.password)
	if err != nil {
		return fmt.Errorf("failed to derive seed: %w", err)
	}

	// Derive Ethereum address
	ethHash := sha256.Sum256(append(seed, []byte("ethereum")...))
	ethAddr := "0x" + hex.EncodeToString(ethHash[:20])
	s.wallet.Addresses[ChainEthereum] = ethAddr

	// Derive Trac address
	tracHash := sha256.Sum256(append(seed, []byte("trac")...))
	tracAddr := "0x" + hex.EncodeToString(tracHash[:20])
	s.wallet.Addresses[ChainTrac] = tracAddr

	return nil
}

// EncryptMnemonic encrypts a mnemonic with a password using AES-256-GCM
func EncryptMnemonic(mnemonic string, password string) (*EncryptedKeyStore, error) {
	// Generate salt
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	// Derive key using simple KDF (simplified from Argon2)
	key := deriveKey(password, salt)

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
		Cipher:     "aes-256-gcm",
		KDF:        "sha256-pbkdf2",
		Salt:       salt,
		Nonce:      nonce[:gcm.NonceSize()],
		CipherText: ciphertext[gcm.NonceSize():],
	}, nil
}

// DecryptMnemonic decrypts an encrypted mnemonic
func DecryptMnemonic(ks *EncryptedKeyStore, password string) (string, error) {
	// Derive key
	key := deriveKey(password, ks.Salt)

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

// deriveKey is a simplified key derivation function
func deriveKey(password string, salt []byte) []byte {
	// Simplified: just hash password + salt multiple times
	// Real implementation should use Argon2 or PBKDF2
	data := append([]byte(password), salt...)
	for i := 0; i < 10000; i++ {
		h := sha256.Sum256(data)
		data = h[:]
	}
	return data[:32]
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
