package store

import (
	"context"
	"database/sql"
	"time"
)

// BalanceStore persists account balances.
type BalanceStore struct {
	db *DB
}

// NewBalanceStore creates a BalanceStore backed by the given DB.
func NewBalanceStore(db *DB) *BalanceStore {
	return &BalanceStore{db: db}
}

// Get returns the balance in cents for an API key, or 0 if not found.
func (s *BalanceStore) Get(ctx context.Context, apiKey string) (int64, error) {
	var balance int64
	err := s.db.QueryRowContext(ctx,
		"SELECT balance FROM balances WHERE api_key = ?", apiKey,
	).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return balance, err
}

// Set sets the balance for an API key.
func (s *BalanceStore) Set(ctx context.Context, apiKey string, balanceCents int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO balances (api_key, balance, last_update)
		 VALUES (?, ?, ?)
		 ON CONFLICT(api_key) DO UPDATE SET balance = ?, last_update = ?`,
		apiKey, balanceCents, time.Now().Unix(), balanceCents, time.Now().Unix(),
	)
	return err
}

// Deduct subtracts costCents from the balance.
// Returns the new balance, or an error if insufficient.
func (s *BalanceStore) Deduct(ctx context.Context, apiKey string, costCents int64) (int64, error) {
	var balance int64
	err := s.db.QueryRowContext(ctx,
		"SELECT balance FROM balances WHERE api_key = ?", apiKey,
	).Scan(&balance)
	if err == sql.ErrNoRows {
		return 0, ErrInsufficientBalance
	}
	if err != nil {
		return 0, err
	}
	if balance < costCents {
		return balance, ErrInsufficientBalance
	}
	newBalance := balance - costCents
	_, err = s.db.ExecContext(ctx,
		"UPDATE balances SET balance = ?, last_update = ? WHERE api_key = ?",
		newBalance, time.Now().Unix(), apiKey,
	)
	return newBalance, err
}

// AddCredits adds credits to an account.
func (s *BalanceStore) AddCredits(ctx context.Context, apiKey string, cents int64) error {
	var balance int64
	err := s.db.QueryRowContext(ctx,
		"SELECT balance FROM balances WHERE api_key = ?", apiKey,
	).Scan(&balance)
	if err == sql.ErrNoRows {
		balance = 0
	} else if err != nil {
		return err
	}

	newBalance := balance + cents
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO balances (api_key, balance, last_update)
		 VALUES (?, ?, ?)
		 ON CONFLICT(api_key) DO UPDATE SET balance = ?, last_update = ?`,
		apiKey, newBalance, time.Now().Unix(), newBalance, time.Now().Unix(),
	)
	return err
}

// HasSufficientBalance returns true if the account can afford costCents.
func (s *BalanceStore) HasSufficientBalance(ctx context.Context, apiKey string, costCents int64) bool {
	balance, _ := s.Get(ctx, apiKey)
	return balance >= costCents
}
