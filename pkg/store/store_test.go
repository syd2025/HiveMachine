package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpen_TempFile(t *testing.T) {
	f, err := os.CreateTemp("", "hivemachine_store_test_*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Errorf("Open returned nil db")
	}
}

func TestBalanceStore_CRUD(t *testing.T) {
	f, err := os.CreateTemp("", "hivemachine_balance_test_*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	bs := NewBalanceStore(db)
	ctx := context.Background()

	// Set initial balance.
	if err := bs.Set(ctx, "key1", 10000); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// HasSufficientBalance.
	if !bs.HasSufficientBalance(ctx, "key1", 5000) {
		t.Errorf("HasSufficientBalance: expected true for 5000 of 10000")
	}
	if bs.HasSufficientBalance(ctx, "key1", 15000) {
		t.Errorf("HasSufficientBalance: expected false for 15000 of 10000")
	}

	// Deduct.
	newBal, err := bs.Deduct(ctx, "key1", 3000)
	if err != nil {
		t.Fatalf("Deduct: %v", err)
	}
	if newBal != 7000 {
		t.Errorf("Deduct returned balance: got %d, want 7000", newBal)
	}

	// Get.
	bal, err := bs.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if bal != 7000 {
		t.Errorf("Get: got %d, want 7000", bal)
	}

	// AddCredits.
	if err := bs.AddCredits(ctx, "key1", 500); err != nil {
		t.Fatalf("AddCredits: %v", err)
	}
	bal, _ = bs.Get(ctx, "key1")
	if bal != 7500 {
		t.Errorf("Get after AddCredits: got %d, want 7500", bal)
	}

	// Get non-existent key returns 0, nil.
	gotBal, gotErr := bs.Get(ctx, "nonexistent")
	if gotErr != nil {
		t.Errorf("Get nonexistent: err = %v, want nil", gotErr)
	}
	if gotBal != 0 {
		t.Errorf("Get nonexistent: got %d, want 0", gotBal)
	}
}

func TestReceiptStore_SaveAndRetrieve(t *testing.T) {
	f, err := os.CreateTemp("", "hivemachine_receipt_test_*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	rs := NewReceiptStore(db)
	ctx := context.Background()

	receipt := &ReceiptRow{
		ID:               "receipt-1",
		APIKey:           "key1",
		Model:            "gpt-4o",
		ProviderID:       "openai",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CostCents:        30,
		CreatedAt:        time.Now().Unix(),
		Signature:        []byte("sig"),
	}

	if err := rs.Save(ctx, receipt); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// ByID.
	fetched, err := rs.ByID(ctx, "receipt-1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if fetched.APIKey != "key1" {
		t.Errorf("ByID: APIKey = %q, want key1", fetched.APIKey)
	}
	if fetched.CostCents != 30 {
		t.Errorf("ByID: CostCents = %d, want 30", fetched.CostCents)
	}

	// ByAPIKey.
	receipts, err := rs.ByAPIKey(ctx, "key1")
	if err != nil {
		t.Fatalf("ByAPIKey: %v", err)
	}
	if len(receipts) != 1 {
		t.Errorf("ByAPIKey: got %d receipts, want 1", len(receipts))
	}

	// Stats.
	stats, err := rs.Stats(ctx, "key1", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.RequestCount != 1 {
		t.Errorf("Stats RequestCount: got %d, want 1", stats.RequestCount)
	}
	if stats.TotalCostCents != 30 {
		t.Errorf("Stats TotalCostCents: got %d, want 30", stats.TotalCostCents)
	}
}

func TestQuotaStore(t *testing.T) {
	f, err := os.CreateTemp("", "hivemachine_quota_test_*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	qs := NewQuotaStore(db)
	ctx := context.Background()

	q := &QuotaRow{
		APIKey:    "key1",
		Limit:     10000,
		Remaining: 10000,
		PeriodEnd: time.Now().Add(24 * time.Hour),
	}
	if err := qs.Set(ctx, q); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify DB directly.
	var limit, remaining, periodEnd int64
	db.QueryRowContext(ctx,
		"SELECT limit_tokens, remaining, period_end FROM quotas WHERE api_key = ?", "key1",
	).Scan(&limit, &remaining, &periodEnd)
	t.Logf("After Set: limit=%d remaining=%d periodEnd=%d", limit, remaining, periodEnd)

	if limit != 10000 {
		t.Errorf("DB limit: got %d, want 10000", limit)
	}
	if remaining != 10000 {
		t.Errorf("DB remaining: got %d, want 10000", remaining)
	}

	// Check within limit.
	allowed, rem := qs.Check(ctx, "key1", 5000)
	if !allowed {
		t.Errorf("Check: expected allowed, got false")
	}
	if rem != 10000 {
		t.Errorf("Check rem: got %d, want 10000", rem)
	}

	// Deduct.
	if err := qs.Deduct(ctx, "key1", 3000); err != nil {
		t.Fatalf("Deduct: %v", err)
	}

	// Verify DB directly after deduct.
	db.QueryRowContext(ctx,
		"SELECT limit_tokens, remaining, period_end FROM quotas WHERE api_key = ?", "key1",
	).Scan(&limit, &remaining, &periodEnd)
	t.Logf("After Deduct: limit=%d remaining=%d periodEnd=%d", limit, remaining, periodEnd)

	if remaining != 7000 {
		t.Errorf("DB remaining after deduct: got %d, want 7000", remaining)
	}

	rem = qs.Remaining(ctx, "key1")
	if rem != 7000 {
		t.Errorf("Remaining after deduct: got %d, want 7000", rem)
	}
}
