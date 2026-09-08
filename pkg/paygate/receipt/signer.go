package receipt

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
)

// Signer creates and verifies Ed25519 signatures for receipts.
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewSigner creates a Signer from a 32-byte seed.
// If seed is nil, a new key pair is generated (use only for dev/testing).
func NewSigner(seed []byte) *Signer {
	var priv ed25519.PrivateKey
	if len(seed) == ed25519.SeedSize {
		priv = ed25519.NewKeyFromSeed(seed)
	} else {
		_, priv, _ = ed25519.GenerateKey(rand.Reader)
	}
	return &Signer{privateKey: priv, publicKey: priv.Public().(ed25519.PublicKey)}
}

// NewSignerFromFile loads a Signer from a file containing base64-encoded private key.
// If the file does not exist, generates a new key pair and writes it to the file.
func NewSignerFromFile(path string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		// File exists — decode the key.
		seed, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return nil, fmt.Errorf("receipt: decode signer seed: %w", err)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("receipt: invalid seed size %d, want %d", len(seed), ed25519.SeedSize)
		}
		return NewSigner(seed), nil
	}
	// File doesn't exist — generate new key and persist.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("receipt: generate key: %w", err)
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(priv.Seed())), 0600); err != nil {
		return nil, fmt.Errorf("receipt: persist signer key: %w", err)
	}
	return &Signer{privateKey: priv, publicKey: priv.Public().(ed25519.PublicKey)}, nil
}

// Sign signs a receipt and sets its Signature field.
// The receipt's Signature field must be nil when passed to Sign.
func (s *Signer) Sign(r *Receipt) error {
	if r.Signature != nil {
		return fmt.Errorf("receipt: sign: receipt already has a signature")
	}
	r.Signature = s.SignCanonical(r.Canonical())
	return nil
}

// SignCanonical signs an arbitrary canonical string and returns the raw signature bytes.
func (s *Signer) SignCanonical(canonical string) []byte {
	return ed25519.Sign(s.privateKey, []byte(canonical))
}

// Verify checks the receipt's signature against the stored public key.
func (s *Signer) Verify(r *Receipt) bool {
	if r.Signature == nil {
		return false
	}
	return ed25519.Verify(s.publicKey, []byte(r.Canonical()), r.Signature)
}

// PublicKey returns the signer's public key as base64.
func (s *Signer) PublicKeyB64() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

// PublicKey returns the raw public key bytes.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return s.publicKey
}

// HashCanonical returns a SHA-256 hash of the canonical string.
// Used as a short receipt ID prefix.
func HashCanonical(canonical string) string {
	h := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", h[:8]) // first 8 bytes as hex
}
