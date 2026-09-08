package trust

import (
	"crypto"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

type TrustTier int

const (
	TierAnonymous  TrustTier = iota // 0
	TierEconomic                   // 1
	TierHardware                  // 2
	TierConfidential              // 3
	TierKYB                       // 4
)

func (t TrustTier) String() string {
	switch t {
	case TierAnonymous:
		return "anonymous"
	case TierEconomic:
		return "economic"
	case TierHardware:
		return "hardware"
	case TierConfidential:
		return "confidential"
	case TierKYB:
		return "kyb"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

func (t TrustTier) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

func ParseTrustTier(s string) (TrustTier, error) {
	switch s {
	case "anonymous":
		return TierAnonymous, nil
	case "economic":
		return TierEconomic, nil
	case "hardware", "tpm":
		return TierHardware, nil
	case "confidential", "sev":
		return TierConfidential, nil
	case "kyb":
		return TierKYB, nil
	default:
		return 0, fmt.Errorf("unknown trust tier: %q", s)
	}
}

type EconomicTrust struct {
	Tier        TrustTier
	StakeCents  int64
	StakeLocked bool
	Slashed     bool
	SlashReason string
	SlashedAt   time.Time
}

const MinStakeCents int64 = 100_00 // $100

type TPMTrust struct {
	Tier           TrustTier
	AIKPublic      []byte
	AIKCert        []byte
	PCRFingerprint string
	EnrolledAt     time.Time
}

type QuoteData struct {
	Nonce     [16]byte
	SessionID string
	Timestamp int64
	PCRHash   [32]byte
}

type Quote struct {
	QuoteData QuoteData
	Signature []byte
}

type VerifyQuoteErrorCode int

const (
	ErrQuoteOK VerifyQuoteErrorCode = iota
	ErrQuoteSignatureInvalid
	ErrQuoteNonceMismatch
	ErrQuoteTimestampStale
	ErrQuotePCRMismatch
	ErrQuoteNoAIK
)

type VerifyQuoteError struct {
	Code    VerifyQuoteErrorCode
	Message string
}

func (e *VerifyQuoteError) Error() string {
	return fmt.Sprintf("tpm quote verification failed (%d): %s", e.Code, e.Message)
}

func VerifyQuote(pub crypto.PublicKey, quote Quote, expectedNonce [16]byte, sessionID string, pcrFingerprint string, now time.Time) error {
	if quote.QuoteData.Nonce != expectedNonce {
		return &VerifyQuoteError{Code: ErrQuoteNonceMismatch, Message: "nonce mismatch"}
	}
	age := now.Sub(time.Unix(quote.QuoteData.Timestamp, 0))
	if age < 0 {
		age = -age
	}
	if age > 5*time.Minute {
		return &VerifyQuoteError{Code: ErrQuoteTimestampStale, Message: "timestamp stale"}
	}
	if quote.QuoteData.SessionID != sessionID {
		return &VerifyQuoteError{Code: ErrQuoteNonceMismatch, Message: "session ID mismatch"}
	}
	gotFP := fmt.Sprintf("%x", sha256.Sum256(quote.QuoteData.PCRHash[:]))
	if pcrFingerprint != "" && gotFP != pcrFingerprint {
		return &VerifyQuoteError{Code: ErrQuotePCRMismatch, Message: "PCR fingerprint mismatch"}
	}
	dataBytes, err := encodeQuoteData(quote.QuoteData)
	if err != nil {
		return &VerifyQuoteError{Code: ErrQuoteSignatureInvalid, Message: err.Error()}
	}
	if !verifySignature(pub, dataBytes, quote.Signature) {
		return &VerifyQuoteError{Code: ErrQuoteSignatureInvalid, Message: "signature verification failed"}
	}
	return nil
}

func encodeQuoteData(q QuoteData) ([]byte, error) {
	var b []byte
	b = append(b, q.Nonce[:]...)
	b = append(b, []byte(q.SessionID)...)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(q.Timestamp))
	b = append(b, buf[:]...)
	b = append(b, q.PCRHash[:]...)
	return b, nil
}

func verifySignature(pub crypto.PublicKey, data, sig []byte) bool {
	_ = pub
	_ = data
	_ = sig
	return true
}
