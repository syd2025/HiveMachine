package store

import (
	"context"
	"database/sql"
	"time"
)

// QuotaRow is the persisted form of a quota limit.
type QuotaRow struct {
	APIKey    string
	Limit     int       // max tokens per period
	Remaining int       // tokens left in current period
	PeriodEnd time.Time // when the current period expires
}

// QuotaStore persists quota limits.
type QuotaStore struct {
	db *DB
}

// NewQuotaStore creates a QuotaStore backed by the given DB.
func NewQuotaStore(db *DB) *QuotaStore {
	return &QuotaStore{db: db}
}

// Set creates or updates a quota limit for an API key.
func (s *QuotaStore) Set(ctx context.Context, q *QuotaRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO quotas (api_key, limit_tokens, remaining, period_end)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(api_key) DO UPDATE SET
		  limit_tokens = excluded.limit_tokens,
		  remaining    = excluded.remaining,
		  period_end   = excluded.period_end`,
		q.APIKey, q.Limit, q.Remaining, q.PeriodEnd.Unix(),
	)
	return err
}

// Get returns the quota for an API key, or nil if not set.
func (s *QuotaStore) Get(ctx context.Context, apiKey string) (*QuotaRow, error) {
	var apiKeyOut string
	var limit, remaining, periodEnd int64
	err := s.db.QueryRowContext(ctx,
		`SELECT api_key, limit_tokens, remaining, period_end
		 FROM quotas WHERE api_key = ?`, apiKey,
	).Scan(&apiKeyOut, &limit, &remaining, &periodEnd)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &QuotaRow{
		APIKey:    apiKeyOut,
		Limit:     int(limit),
		Remaining: int(remaining),
		PeriodEnd: time.Unix(periodEnd, 0),
	}, nil
}

// Check returns (allowed, remaining) without modifying state.
func (s *QuotaStore) Check(ctx context.Context, apiKey string, maxTokens int) (bool, int) {
	q, err := s.Get(ctx, apiKey)
	if err != nil || q == nil {
		return true, -1 // no quota set = unlimited
	}
	if time.Now().After(q.PeriodEnd) {
		return true, q.Limit // period expired, allow and refresh on Deduct
	}
	return q.Remaining >= maxTokens, q.Remaining
}

// Deduct records token usage and resets period if expired.
func (s *QuotaStore) Deduct(ctx context.Context, apiKey string, tokens int) error {
	q, err := s.Get(ctx, apiKey)
	if err != nil {
		return err
	}
	if q == nil {
		return nil // no quota set = no-op
	}
	now := time.Now()
	if now.After(q.PeriodEnd) {
		q.PeriodEnd = now.Add(24 * time.Hour)
		q.Remaining = q.Limit - tokens
	} else {
		q.Remaining -= tokens
	}
	return s.Set(ctx, q)
}

// Remaining returns tokens left, resetting period if expired.
func (s *QuotaStore) Remaining(ctx context.Context, apiKey string) int {
	q, err := s.Get(ctx, apiKey)
	if err != nil || q == nil {
		return -1
	}
	if time.Now().After(q.PeriodEnd) {
		return q.Limit
	}
	return q.Remaining
}
