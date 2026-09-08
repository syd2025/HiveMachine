package dispute

import (
	"testing"
	"time"
)

func TestDisputeState(t *testing.T) {
	tests := []struct {
		state    DisputeState
		expected string
		valid   bool
	}{
		{DisputeStateOpen, "open", false},
		{DisputeStateUnderReview, "under_review", false},
		{DisputeStateResolved, "resolved", true},
		{DisputeStateRejected, "rejected", true},
		{DisputeStateExpired, "expired", true},
		{DisputeState("invalid"), "invalid", false},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("DisputeState.String() = %v, want %v", got, tt.expected)
		}
		if got := tt.state.Valid(); got != tt.valid {
			t.Errorf("DisputeState.Valid() = %v, want %v", got, tt.valid)
		}
	}
}

func TestDisputeStateString(t *testing.T) {
	if DisputeStateOpen.String() != "open" {
		t.Errorf("DisputeStateOpen = %q, want %q", DisputeStateOpen, "open")
	}
	if DisputeStateUnderReview.String() != "under_review" {
		t.Errorf("DisputeStateUnderReview = %q, want %q", DisputeStateUnderReview, "under_review")
	}
}

func TestDisputeTransition(t *testing.T) {
	now := time.Now()
	d := &Dispute{
		ID:          "dp_123",
		APIKey:      "key_abc",
		ReceiptID:   "rc_456",
		Reason:      "quality",
		State:       DisputeStateOpen,
		AmountCents: 1000,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Open → UnderReview: valid
	if err := d.TransitionTo(DisputeStateUnderReview); err != nil {
		t.Errorf("Open → UnderReview: unexpected error %v", err)
	}
	if d.State != DisputeStateUnderReview {
		t.Errorf("state = %v, want UnderReview", d.State)
	}

	// UnderReview → Resolved: valid
	if err := d.TransitionTo(DisputeStateResolved); err != nil {
		t.Errorf("UnderReview → Resolved: unexpected error %v", err)
	}
	if d.State != DisputeStateResolved {
		t.Errorf("state = %v, want Resolved", d.State)
	}
	if d.ResolvedAt == nil {
		t.Error("ResolvedAt should be set")
	}

	// Terminal → anything: invalid
	if err := d.TransitionTo(DisputeStateRejected); err != ErrInvalidTransition {
		t.Errorf("Resolved → Rejected: got %v, want ErrInvalidTransition", err)
	}
}

func TestDisputeInvalidTransitions(t *testing.T) {
	now := time.Now()
	// Valid transitions to test as invalid:
	// Open → Resolved: invalid (must go through UnderReview)
	// Open → Rejected: invalid (must go through UnderReview)
	// UnderReview → Open: invalid
	// UnderReview → Rejected: valid (should not be here)
	// Resolved → Open: invalid
	// Rejected → UnderReview: invalid
	// Expired → Resolved: invalid
	cases := []struct {
		from, to DisputeState
		valid  bool
	}{
		{DisputeStateOpen, DisputeStateResolved, false},
		{DisputeStateOpen, DisputeStateRejected, false},
		{DisputeStateOpen, DisputeStateExpired, true}, // Open can expire directly
		{DisputeStateUnderReview, DisputeStateOpen, false},
		{DisputeStateUnderReview, DisputeStateResolved, true}, // under review can resolve directly
		{DisputeStateResolved, DisputeStateOpen, false},
		{DisputeStateRejected, DisputeStateUnderReview, false},
		{DisputeStateExpired, DisputeStateResolved, false},
	}
	for _, tt := range cases {
		d := &Dispute{State: tt.from, CreatedAt: now, UpdatedAt: now}
		err := d.TransitionTo(tt.to)
		if tt.valid && err != nil {
			t.Errorf("%v → %v: unexpected error %v", tt.from, tt.to, err)
		} else if !tt.valid && err != ErrInvalidTransition {
			t.Errorf("%v → %v: got %v, want ErrInvalidTransition", tt.from, tt.to, err)
		}
	}
}

func TestOpenDisputeExpiry(t *testing.T) {
	now := time.Now()
	d := &Dispute{
		ID:        "dp_expired",
		State:     DisputeStateOpen,
		CreatedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-48 * time.Hour),
	}
	// Open can expire directly
	if err := d.TransitionTo(DisputeStateExpired); err != nil {
		t.Errorf("Open → Expired: unexpected error %v", err)
	}
	if d.State != DisputeStateExpired {
		t.Errorf("state = %v, want Expired", d.State)
	}
}

func TestInMemoryDisputeStore(t *testing.T) {
	store := NewInMemoryDisputeStore()
	now := time.Now()

	d := &Dispute{
		ID:          "dp_1",
		APIKey:      "key_abc",
		ReceiptID:   "rc_1",
		Reason:      "quality",
		State:       DisputeStateOpen,
		AmountCents: 500,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.Open(d); err != nil {
		t.Fatalf("Open() error: %v", err)
	}

	got, err := store.ByID("dp_1")
	if err != nil {
		t.Fatalf("ByID() error: %v", err)
	}
	if got.ID != "dp_1" {
		t.Errorf("ByID().ID = %q, want %q", got.ID, "dp_1")
	}

	_, err = store.ByID("dp_nonexistent")
	if err != ErrDisputeNotFound {
		t.Errorf("ByID nonexistent: got %v, want ErrDisputeNotFound", err)
	}

	list, err := store.ByAPIKey("key_abc")
	if err != nil {
		t.Fatalf("ByAPIKey() error: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len(list) = %d, want 1", len(list))
	}

	if err := store.UpdateState("dp_1", DisputeStateUnderReview); err != nil {
		t.Fatalf("UpdateState() error: %v", err)
	}
	got, _ = store.ByID("dp_1")
	if got.State != DisputeStateUnderReview {
		t.Errorf("state = %v, want UnderReview", got.State)
	}

	if err := store.SetResolution("dp_1", "refunded", DisputeStateResolved); err != nil {
		t.Fatalf("SetResolution() error: %v", err)
	}
	got, _ = store.ByID("dp_1")
	if got.State != DisputeStateResolved {
		t.Errorf("state = %v, want Resolved", got.State)
	}
	if got.Resolution != "refunded" {
		t.Errorf("resolution = %q, want %q", got.Resolution, "refunded")
	}
}

func TestInMemoryDisputeStoreExpireOld(t *testing.T) {
	store := NewInMemoryDisputeStore()
	now := time.Now()

	store.Open(&Dispute{
		ID:        "dp_old",
		State:     DisputeStateOpen,
		CreatedAt: now.Add(-49 * time.Hour),
		UpdatedAt: now.Add(-49 * time.Hour),
	})
	store.Open(&Dispute{
		ID:        "dp_recent",
		State:     DisputeStateOpen,
		CreatedAt: now.Add(-1 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Hour),
	})

	count, err := store.ExpireOld(48 * time.Hour)
	if err != nil {
		t.Fatalf("ExpireOld() error: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	d, _ := store.ByID("dp_old")
	if d.State != DisputeStateExpired {
		t.Errorf("dp_old state = %v, want Expired", d.State)
	}
	d2, _ := store.ByID("dp_recent")
	if d2.State != DisputeStateOpen {
		t.Errorf("dp_recent state = %v, want Open", d2.State)
	}
}

func TestInMemoryDisputeStoreList(t *testing.T) {
	store := NewInMemoryDisputeStore()
	now := time.Now()

	store.Open(&Dispute{ID: "dp_1", State: DisputeStateOpen, CreatedAt: now, UpdatedAt: now})
	store.Open(&Dispute{ID: "dp_2", State: DisputeStateOpen, CreatedAt: now, UpdatedAt: now})
	store.Open(&Dispute{ID: "dp_3", State: DisputeStateResolved, CreatedAt: now, UpdatedAt: now})

	all, err := store.List()
	if err != nil || len(all) != 3 {
		t.Fatalf("List() = %d, want 3", len(all))
	}

	open, err := store.List(DisputeStateOpen)
	if err != nil || len(open) != 2 {
		t.Errorf("List(Open) = %d, want 2", len(open))
	}

	resolved, err := store.List(DisputeStateResolved)
	if err != nil || len(resolved) != 1 {
		t.Errorf("List(Resolved) = %d, want 1", len(resolved))
	}
}
