package wallet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrWalletNotStored is returned when a wallet operation requires a persisted wallet but none exists.
var ErrWalletNotStored = errors.New("wallet not persisted")

// ErrWrongPassword is returned when the provided password doesn't decrypt the keystore.
var ErrWrongPassword = errors.New("wrong password")

// PersistentService is a WalletService backed by FileWalletStore.
// It loads a persisted wallet on startup and keeps it locked by default;
// the user must Unlock(password) to access the mnemonic or sign transactions.
type PersistentService struct {
	store   *FileWalletStore
	service *DefaultWalletService
	mu      sync.RWMutex
}

// NewPersistentService returns a service that persists wallets to dir.
// If a wallet file already exists it is loaded (locked); otherwise the store is empty.
func NewPersistentService(dir string) (*PersistentService, error) {
	store, err := NewFileWalletStore(dir)
	if err != nil {
		return nil, err
	}

	ps := &PersistentService{
		store:   store,
		service: NewWalletService(),
	}

	// Try to load existing wallet metadata.
	existing, err := store.Get()
	if err != nil && !errors.Is(err, ErrWalletNotFound) {
		return nil, err
	}
	if existing != nil {
		// Attach the loaded wallet metadata so GetAddress/ListAddresses work
		// while the wallet is locked (those methods don't need the mnemonic).
		ps.service.mu.Lock()
		ps.service.wallet = existing
		ps.service.unlocked = false
		ps.service.mu.Unlock()
	}

	return ps, nil
}

// WalletPath returns the file path of the stored wallet.
func (ps *PersistentService) WalletPath() string {
	return ps.store.Path()
}

// CreateWallet implements WalletService.
func (ps *PersistentService) CreateWallet(ctx context.Context, password string) (*Wallet, error) {
	wallet, err := ps.service.CreateWallet(ctx, password)
	if err != nil {
		return nil, err
	}

	ps.mu.Lock()
	ps.store.bindKeyStore(ps.service.keystore)
	err = ps.store.Save(wallet, false)
	ps.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return wallet, nil
}

// RestoreWallet implements WalletService.
func (ps *PersistentService) RestoreWallet(ctx context.Context, mnemonic string, password string) (*Wallet, error) {
	wallet, err := ps.service.RestoreWallet(ctx, mnemonic, password)
	if err != nil {
		return nil, err
	}

	ps.mu.Lock()
	ps.store.bindKeyStore(ps.service.keystore)
	err = ps.store.Save(wallet, false)
	ps.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return wallet, nil
}

// GetMnemonic implements WalletService.
// If the wallet is locked it attempts Unlock with the given password first.
func (ps *PersistentService) GetMnemonic(ctx context.Context, password string) (string, error) {
	ps.mu.RLock()
	unlocked := ps.service.IsUnlocked()
	ps.mu.RUnlock()

	if !unlocked {
		if err := ps.service.Unlock(ctx, password); err != nil {
			return "", ErrWrongPassword
		}
	}

	mnemonic, err := ps.service.GetMnemonic(ctx, password)
	if err != nil {
		return "", err
	}

	// After successful unlock, rebind keystore reference and persist updated LastActive.
	ps.mu.Lock()
	ps.store.bindKeyStore(ps.service.keystore)
	if ps.service.wallet != nil {
		ps.service.wallet.LastActive = time.Now().UTC()
		ps.store.Save(ps.service.wallet, true)
	}
	ps.mu.Unlock()

	return mnemonic, nil
}

// GetAddress implements WalletService.
func (ps *PersistentService) GetAddress(ctx context.Context, chain ChainType) (string, error) {
	return ps.service.GetAddress(ctx, chain)
}

// ListAddresses implements WalletService.
func (ps *PersistentService) ListAddresses(ctx context.Context) (map[ChainType]string, error) {
	return ps.service.ListAddresses(ctx)
}

// Lock implements WalletService.
func (ps *PersistentService) Lock(ctx context.Context) error {
	return ps.service.Lock(ctx)
}

// Unlock implements WalletService.
func (ps *PersistentService) Unlock(ctx context.Context, password string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	err := ps.service.Unlock(ctx, password)
	if err != nil {
		return err
	}

	// Persist updated LastActive.
	ps.store.bindKeyStore(ps.service.keystore)
	if ps.service.wallet != nil {
		ps.service.wallet.LastActive = time.Now().UTC()
		ps.store.Save(ps.service.wallet, true)
	}
	return nil
}

// IsUnlocked implements WalletService.
func (ps *PersistentService) IsUnlocked() bool {
	return ps.service.IsUnlocked()
}

// Exists reports whether a wallet file exists on disk.
func (ps *PersistentService) Exists() bool {
	return ps.store.Exists()
}

// DefaultWalletDir returns the platform-standard wallet directory.
// Uses XDG_STATE_HOME on Unix.
func DefaultWalletDir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "hivemachine", "wallet"), nil
}
