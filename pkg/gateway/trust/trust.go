package trust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type TrustTier int

const (
	TierAnonymous  TrustTier = iota
	TierEconomic
	TierHardware
	TierConfidential
	TierKYB
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

const MinStakeCents int64 = 100_00

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

type ecdsaSignature struct {
	R *big.Int
	S *big.Int
}

func verifySignature(pub crypto.PublicKey, data, sig []byte) bool {
	if pub == nil || data == nil || sig == nil {
		return false
	}

	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return false
	}

	var parsedSig ecdsaSignature
	_, err := asn1.Unmarshal(sig, &parsedSig)
	if err != nil {
		if len(sig) == 64 {
			parsedSig.R = new(big.Int).SetBytes(sig[:32])
			parsedSig.S = new(big.Int).SetBytes(sig[32:])
		} else {
			return false
		}
	}

	hash := sha256.Sum256(data)
	return ecdsa.Verify(ecdsaPub, hash[:], parsedSig.R, parsedSig.S)
}

func ParseAIKPublicKey(derBytes []byte) (*ecdsa.PublicKey, error) {
	if len(derBytes) == 0 {
		return nil, errors.New("empty AIK public key bytes")
	}
	pub, err := x509.ParsePKIXPublicKey(derBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AIK public key: %w", err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("AIK public key is not ECDSA")
	}
	return ecdsaPub, nil
}

func ParseAIKCertificate(certBytes []byte) (*x509.Certificate, error) {
	if len(certBytes) == 0 {
		return nil, errors.New("empty AIK certificate bytes")
	}
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AIK certificate: %w", err)
	}
	return cert, nil
}

func VerifyAIKCertificateChain(aikCert *x509.Certificate, trustedRoots []*x509.Certificate, intermediateCerts []*x509.Certificate) error {
	if aikCert == nil {
		return errors.New("AIK certificate is nil")
	}
	if len(trustedRoots) == 0 {
		return errors.New("no trusted root certificates provided")
	}
	intermediatePool := x509.NewCertPool()
	for _, cert := range intermediateCerts {
		intermediatePool.AddCert(cert)
	}
	rootPool := x509.NewCertPool()
	for _, root := range trustedRoots {
		rootPool.AddCert(root)
	}
	opts := x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	_, err := aikCert.Verify(opts)
	if err != nil {
		return fmt.Errorf("AIK certificate chain verification failed: %w", err)
	}
	return nil
}

func ValidateNonce(quoteNonce, expectedNonce [16]byte) error {
	if quoteNonce != expectedNonce {
		return errors.New("nonce mismatch: possible replay attack")
	}
	return nil
}

func CreateNonce() ([16]byte, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return nonce, nil
}

func ParseQuoteDataFromTPM(quoteBytes []byte, nonce [16]byte) (*QuoteData, error) {
	if len(quoteBytes) < 32 {
		return nil, errors.New("quote bytes too short")
	}
	offset := 0
	offset += 6
	qualifiedSignerLen := binary.BigEndian.Uint16(quoteBytes[offset : offset+2])
	offset += 2
	if offset+int(qualifiedSignerLen) > len(quoteBytes) {
		return nil, errors.New("invalid qualifiedSigner length")
	}
	qualifiedSigner := quoteBytes[offset : offset+int(qualifiedSignerLen)]
	offset += int(qualifiedSignerLen)
	if len(qualifiedSigner) < 16 {
		return nil, errors.New("qualifiedSigner too short to contain nonce")
	}
	var extractedNonce [16]byte
	copy(extractedNonce[:], qualifiedSigner[len(qualifiedSigner)-16:])
	offset += 17
	offset += 8
	if offset+2 > len(quoteBytes) {
		return nil, errors.New("quote bytes too short for pcr digest")
	}
	pcrDigestLen := binary.BigEndian.Uint16(quoteBytes[offset : offset+2])
	offset += 2
	if offset+int(pcrDigestLen) > len(quoteBytes) {
		return nil, errors.New("invalid pcrDigest length")
	}
	pcrDigest := quoteBytes[offset : offset+int(pcrDigestLen)]
	return &QuoteData{
		Nonce:     extractedNonce,
		SessionID: "",
		Timestamp: time.Now().Unix(),
		PCRHash:   sha256.Sum256(pcrDigest),
	}, nil
}
