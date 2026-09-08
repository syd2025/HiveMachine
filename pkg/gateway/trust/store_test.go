package trust

import (
	"testing"
	"time"
)

func TestInMemoryTrustStore_SetAndGet(t *testing.T) {
	store := NewInMemoryTrustStore()

	// No state initially
	_, err := store.Get("provider-1")
	if err != ErrProviderNotFound {
		t.Errorf("expected ErrProviderNotFound, got %v", err)
	}

	// Set tier
	err = store.SetTier("provider-1", TierEconomic)
	if err != nil {
		t.Fatal(err)
	}

	state, err := store.Get("provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Tier != TierEconomic {
		t.Errorf("Tier = %v, want TierEconomic", state.Tier)
	}
}

func TestInMemoryTrustStore_EconomicTrust(t *testing.T) {
	store := NewInMemoryTrustStore()

	econ := &EconomicTrust{
		StakeCents:  500_00,
		StakeLocked: true,
	}
	err := store.SetEconomicTrust("provider-1", econ)
	if err != nil {
		t.Fatal(err)
	}

	state, err := store.Get("provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Tier != TierEconomic {
		t.Errorf("Tier = %v, want TierEconomic", state.Tier)
	}
	if state.Economic.StakeCents != 500_00 {
		t.Errorf("StakeCents = %d, want 500_00", state.Economic.StakeCents)
	}
}

func TestInMemoryTrustStore_TPMEnrollment(t *testing.T) {
	store := NewInMemoryTrustStore()

	tpm := &TPMTrust{
		AIKPublic:      []byte("test-aik-pub"),
		PCRFingerprint: "abc123",
		EnrolledAt:     time.Now(),
	}
	err := store.EnrollTPM("provider-1", tpm)
	if err != nil {
		t.Fatal(err)
	}

	state, err := store.Get("provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Tier != TierHardware {
		t.Errorf("Tier = %v, want TierHardware", state.Tier)
	}
	if state.AttestedTier != TierHardware {
		t.Errorf("AttestedTier = %v, want TierHardware", state.AttestedTier)
	}
}

func TestInMemoryTrustStore_Slash(t *testing.T) {
	store := NewInMemoryTrustStore()

	store.SetTier("provider-1", TierEconomic)
	store.SetEconomicTrust("provider-1", &EconomicTrust{StakeCents: 200_00, StakeLocked: true})

	err := store.SlashEconomic("provider-1", "downtime violation")
	if err != nil {
		t.Fatal(err)
	}

	state, err := store.Get("provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Economic.Slashed {
		t.Error("expected Slashed=true")
	}
	if state.Economic.SlashReason != "downtime violation" {
		t.Errorf("SlashReason = %q, want %q", state.Economic.SlashReason, "downtime violation")
	}
	if state.Tier != TierAnonymous {
		t.Errorf("Tier after slash = %v, want TierAnonymous", state.Tier)
	}
}

func TestState_CanRoute(t *testing.T) {
	tests := []struct {
		name    string
		state   *State
		minTier TrustTier
		want    bool
	}{
		{
			name:    "nil state",
			state:   nil,
			minTier: TierAnonymous,
			want:    false,
		},
		{
			name:    "slashed economic",
			state:   &State{Tier: TierEconomic, Economic: &EconomicTrust{Slashed: true}},
			minTier: TierEconomic,
			want:    false,
		},
		{
			name:    "tier too low",
			state:   &State{Tier: TierEconomic},
			minTier: TierHardware,
			want:    false,
		},
		{
			name:    "tier sufficient",
			state:   &State{Tier: TierHardware},
			minTier: TierEconomic,
			want:    true,
		},
		{
			name:    "anonymous at minimum",
			state:   &State{Tier: TierAnonymous},
			minTier: TierAnonymous,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.CanRoute(tt.minTier); got != tt.want {
				t.Errorf("CanRoute(%v) = %v, want %v", tt.minTier, got, tt.want)
			}
		})
	}
}

func TestInMemoryTrustStore_QuoteCache(t *testing.T) {
	store := NewInMemoryTrustStore()

	// Not cached initially
	cached, err := store.GetVerifiedQuoteCache("provider-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		t.Error("expected not cached initially")
	}

	// Cache it
	err = store.CacheVerifiedQuote("provider-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}

	// Now cached
	cached, err = store.GetVerifiedQuoteCache("provider-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !cached {
		t.Error("expected cached")
	}

	// Different session not cached
	cached, err = store.GetVerifiedQuoteCache("provider-1", "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		t.Error("expected not cached for session-2")
	}
}
