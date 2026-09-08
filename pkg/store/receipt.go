package store

import (
	"context"
	"database/sql"
	"time"
)

// ReceiptRow is the persisted form of a receipt.
type ReceiptRow struct {
	ID               string
	APIKey           string
	Model            string
	ProviderID       string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostCents        int64
	CreatedAt        int64
	Signature        []byte
}

// ReceiptStore persists signed usage receipts.
type ReceiptStore struct {
	db *DB
}

// NewReceiptStore creates a ReceiptStore backed by the given DB.
func NewReceiptStore(db *DB) *ReceiptStore {
	return &ReceiptStore{db: db}
}

// Save inserts or replaces a receipt.
func (s *ReceiptStore) Save(ctx context.Context, r *ReceiptRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO receipts
		 (id, api_key, model, provider_id, prompt_tokens, completion_tokens,
		  total_tokens, cost_cents, created_at, signature)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		  prompt_tokens = excluded.prompt_tokens,
		  completion_tokens = excluded.completion_tokens,
		  total_tokens = excluded.total_tokens,
		  cost_cents = excluded.cost_cents,
		  signature = excluded.signature`,
		r.ID, r.APIKey, r.Model, r.ProviderID,
		r.PromptTokens, r.CompletionTokens, r.TotalTokens,
		r.CostCents, r.CreatedAt, r.Signature,
	)
	return err
}

// ByID returns a receipt by its ID.
func (s *ReceiptStore) ByID(ctx context.Context, id string) (*ReceiptRow, error) {
	var r ReceiptRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, api_key, model, provider_id, prompt_tokens,
		        completion_tokens, total_tokens, cost_cents, created_at, signature
		 FROM receipts WHERE id = ?`, id,
	).Scan(&r.ID, &r.APIKey, &r.Model, &r.ProviderID,
		&r.PromptTokens, &r.CompletionTokens, &r.TotalTokens,
		&r.CostCents, &r.CreatedAt, &r.Signature)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &r, err
}

// ByAPIKey returns all receipts for an API key, most recent first.
func (s *ReceiptStore) ByAPIKey(ctx context.Context, apiKey string) ([]*ReceiptRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key, model, provider_id, prompt_tokens,
		        completion_tokens, total_tokens, cost_cents, created_at, signature
		 FROM receipts WHERE api_key = ?
		 ORDER BY created_at DESC LIMIT 100`, apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var receipts []*ReceiptRow
	for rows.Next() {
		var r ReceiptRow
		err := rows.Scan(&r.ID, &r.APIKey, &r.Model, &r.ProviderID,
			&r.PromptTokens, &r.CompletionTokens, &r.TotalTokens,
			&r.CostCents, &r.CreatedAt, &r.Signature)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, &r)
	}
	return receipts, rows.Err()
}

// Recent returns the n most recent receipts across all accounts.
func (s *ReceiptStore) Recent(ctx context.Context, n int) ([]*ReceiptRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key, model, provider_id, prompt_tokens,
		        completion_tokens, total_tokens, cost_cents, created_at, signature
		 FROM receipts
		 ORDER BY created_at DESC LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var receipts []*ReceiptRow
	for rows.Next() {
		var r ReceiptRow
		err := rows.Scan(&r.ID, &r.APIKey, &r.Model, &r.ProviderID,
			&r.PromptTokens, &r.CompletionTokens, &r.TotalTokens,
			&r.CostCents, &r.CreatedAt, &r.Signature)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, &r)
	}
	return receipts, rows.Err()
}

// Stats returns aggregate stats for an API key within the given time window.
func (s *ReceiptStore) Stats(ctx context.Context, apiKey string, since time.Time) (*Stats, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(prompt_tokens),0),
		        COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cost_cents),0)
		 FROM receipts
		 WHERE api_key = ? AND created_at >= ?`, apiKey, since.Unix())
	var stats Stats
	err := row.Scan(&stats.RequestCount, &stats.PromptTokens,
		&stats.CompletionTokens, &stats.TotalCostCents)
	return &stats, err
}

// Stats holds aggregate usage statistics.
type Stats struct {
	RequestCount     int
	PromptTokens     int
	CompletionTokens int
	TotalCostCents   int64
}
