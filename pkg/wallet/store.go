// Package wallet provides cryptographic wallet management for HiveMachine.
// It handles mnemonic generation, key derivation, and secure storage.
package wallet

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrWalletNotFound is returned when no wallet is found in the store.
var ErrWalletNotFound = errors.New("wallet not found")

// ErrWalletAlreadyExists is returned when creating a wallet but one already exists.
var ErrWalletAlreadyExists = errors.New("wallet already exists")

// WalletStore persists wallet state to disk.
type WalletStore interface {
	// Get returns the stored wallet, or ErrWalletNotFound.
	Get() (*Wallet, error)
	// Save stores a wallet. Returns ErrWalletAlreadyExists if a wallet already exists
	// and overwrite is false.
	Save(w *Wallet, overwrite bool) error
	// Exists returns true if a wallet is stored.
	Exists() bool
	// Path returns the file path of the stored wallet.
	Path() string
}

// fileWalletMeta is the non-sensitive subset of Wallet stored on disk.
type fileWalletMeta struct {
	ID         WalletID            `json:"id"`
	Source     WalletSource        `json:"source"`
	CreatedAt  int64               `json:"created_at"`
	LastActive int64               `json:"last_active"`
	Addresses  map[ChainType]string `json:"addresses"`
}

// fileWalletPayload is the JSON structure stored on disk.
type fileWalletPayload struct {
	Schema int                `json:"schema"`
	Meta   *fileWalletMeta   `json:"meta"`
	Key    *EncryptedKeyStore `json:"key,omitempty"`
}

const walletFileSchemaVersion = 1

// FileWalletStore persists wallet as an encrypted JSON file.
type FileWalletStore struct {
	mu   sync.RWMutex
	path string
	// boundKS is embedded when saving so Save can serialize it alongside meta.
	boundKS *EncryptedKeyStore
}

// NewFileWalletStore returns a FileWalletStore that persists to dir.
// The wallet file is named "wallet.json".
func NewFileWalletStore(dir string) (*FileWalletStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &FileWalletStore{
		path: filepath.Join(dir, "wallet.json"),
	}, nil
}

// Exists implements WalletStore.
func (s *FileWalletStore) Exists() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := os.Stat(s.path)
	return err == nil
}

// Path implements WalletStore.
func (s *FileWalletStore) Path() string {
	return s.path
}

// bindKeyStore connects the keystore so Save can embed it.
func (s *FileWalletStore) bindKeyStore(ks *EncryptedKeyStore) {
	s.mu.Lock()
	s.boundKS = ks
	s.mu.Unlock()
}

// Get implements WalletStore.
func (s *FileWalletStore) Get() (*Wallet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}

	var payload fileWalletPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if payload.Meta == nil {
		return nil, ErrWalletNotFound
	}

	wallet := &Wallet{
		ID:        WalletID(payload.Meta.ID),
		Addresses: payload.Meta.Addresses,
		Source:    WalletSource(payload.Meta.Source),
		CreatedAt:  unixToTime(payload.Meta.CreatedAt),
		LastActive: unixToTime(payload.Meta.LastActive),
		Encrypted:  payload.Key != nil,
		mu:         sync.RWMutex{},
	}
	return wallet, nil
}

// Save implements WalletStore. overwrite=false returns ErrWalletAlreadyExists if a file exists.
func (s *FileWalletStore) Save(w *Wallet, overwrite bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !overwrite {
		if _, err := os.Stat(s.path); err == nil {
			return ErrWalletAlreadyExists
		}
	}

	meta := &fileWalletMeta{
		ID:         w.ID,
		Source:     w.Source,
		CreatedAt:  w.CreatedAt.Unix(),
		LastActive: w.LastActive.Unix(),
		Addresses:  w.Addresses,
	}

	payload := fileWalletPayload{
		Schema: walletFileSchemaVersion,
		Meta:   meta,
		Key:    s.boundKS,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Atomic write: temp file + rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func unixToTime(unix int64) time.Time {
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}
