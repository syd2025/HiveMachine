package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"
)

func TestTrustTierString(t *testing.T) {
	tests := []struct {
		tier    TrustTier
		want    string
	}{
		{TierAnonymous, "anonymous"},
		{TierEconomic, "economic"},
		{TierHardware, "hardware"},
		{TierConfidential, "confidential"},
		{TierKYB, "kyb"},
		{TrustTier(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("TrustTier(%d).String() = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

func TestParseTrustTier(t *testing.T) {
	tests := []struct {
		input string
		want  TrustTier
		ok    bool
	}{
		{"anonymous", TierAnonymous, true},
		{"economic", TierEconomic, true},
		{"hardware", TierHardware, true},
		{"tpm", TierHardware, true},
		{"confidential", TierConfidential, true},
		{"sev", TierConfidential, true},
		{"kyb", TierKYB, true},
		{"invalid", 0, false},
	}
	for _, tt := range tests {
		got, err := ParseTrustTier(tt.input)
		if (err == nil) != tt.ok {
			t.Errorf("ParseTrustTier(%q) ok=%v, want ok=%v", tt.input, err == nil, tt.ok)
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseTrustTier(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestVerifyQuote_NonceMismatch(t *testing.T) {
	quote := Quote{
		QuoteData: QuoteData{
			Nonce:     [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SessionID: "session-123",
			Timestamp: time.Now().Unix(),
			PCRHash:   [32]byte{},
		},
		Signature: []byte("sig"),
	}
	expectedNonce := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	err := VerifyQuote(nil, quote, expectedNonce, "session-123", "", time.Now())
	if err == nil {
		t.Fatal("expected nonce mismatch error, got nil")
	}
	verr, ok := err.(*VerifyQuoteError)
	if !ok {
		t.Fatalf("expected *VerifyQuoteError, got %T", err)
	}
	if verr.Code != ErrQuoteNonceMismatch {
		t.Errorf("code = %v, want ErrQuoteNonceMismatch", verr.Code)
	}
}

func TestVerifyQuote_TimestampStale(t *testing.T) {
	nonce := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	oldTime := time.Now().Add(-10 * time.Minute)
	quote := Quote{
		QuoteData: QuoteData{
			Nonce:     nonce,
			SessionID: "session-123",
			Timestamp: oldTime.Unix(),
			PCRHash:   [32]byte{},
		},
		Signature: []byte("sig"),
	}

	err := VerifyQuote(nil, quote, nonce, "session-123", "", time.Now())
	if err == nil {
		t.Fatal("expected stale timestamp error, got nil")
	}
	verr := err.(*VerifyQuoteError)
	if verr.Code != ErrQuoteTimestampStale {
		t.Errorf("code = %v, want ErrQuoteTimestampStale", verr.Code)
	}
}

func TestVerifyQuote_SessionIDMismatch(t *testing.T) {
	nonce := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	quote := Quote{
		QuoteData: QuoteData{
			Nonce:     nonce,
			SessionID: "session-actual",
			Timestamp: time.Now().Unix(),
			PCRHash:   [32]byte{},
		},
		Signature: []byte("sig"),
	}

	err := VerifyQuote(nil, quote, nonce, "session-expected", "", time.Now())
	if err == nil {
		t.Fatal("expected session ID mismatch error, got nil")
	}
	verr := err.(*VerifyQuoteError)
	if verr.Code != ErrQuoteNonceMismatch {
		t.Errorf("code = %v, want ErrQuoteNonceMismatch", verr.Code)
	}
}

func TestVerifyQuote_OK(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	nonce := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	q := QuoteData{
		Nonce:     nonce,
		SessionID: "session-123",
		Timestamp: time.Now().Unix(),
		PCRHash:   [32]byte{},
	}

	dataBytes, err := encodeQuoteData(q)
	if err != nil {
		t.Fatalf("Failed to encode quote data: %v", err)
	}

	hash := sha256.Sum256(dataBytes)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	quote := Quote{
		QuoteData: q,
		Signature: sig,
	}

	err = VerifyQuote(&privateKey.PublicKey, quote, nonce, "session-123", "", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEconomicTrust_MinStake(t *testing.T) {
	if MinStakeCents != 100_00 {
		t.Errorf("MinStakeCents = %d, want 100_00 ($100)", MinStakeCents)
	}
}

func TestTrustTierOrdering(t *testing.T) {
	if TierAnonymous >= TierEconomic {
		t.Error("TierAnonymous should be < TierEconomic")
	}
	if TierEconomic >= TierHardware {
		t.Error("TierEconomic should be < TierHardware")
	}
	if TierHardware >= TierConfidential {
		t.Error("TierHardware should be < TierConfidential")
	}
	if TierConfidential >= TierKYB {
		t.Error("TierConfidential should be < TierKYB")
	}
}
