// Package wallet provides cryptographic wallet management for HiveMachine.
// bip39.go implements BIP-39 mnemonic phrase generation, validation, and seed derivation.
package wallet

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// GenerateMnemonic creates a new BIP-39 mnemonic with 256 bits of entropy (24 words).
// Returns the space-separated mnemonic phrase.
func GenerateMnemonic() (string, error) {
	return GenerateMnemonicWithEntropy(32)
}

// GenerateMnemonicWithEntropy creates a BIP-39 mnemonic from the given entropy size in bytes.
func GenerateMnemonicWithEntropy(entropySize int) (string, error) {
	switch entropySize {
	case 16, 20, 24, 28, 32:
		// valid
	default:
		return "", errors.New("entropy size must be 16, 20, 24, 28, or 32 bytes")
	}
	entropy := make([]byte, entropySize)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("wallet: failed to generate entropy: %w", err)
	}
	return entropyToMnemonic(entropy)
}

func entropyToMnemonic(entropy []byte) (string, error) {
	hash := sha256.Sum256(entropy)
	checksumBits := len(entropy) * 8 / 32
	totalBits := len(entropy)*8 + checksumBits

	bitBuf := make([]bool, totalBits)
	bitIdx := 0
	for _, b := range entropy {
		for i := 7; i >= 0 && bitIdx < totalBits; i-- {
			bitBuf[bitIdx] = (b >> uint(i)) & 1 == 1
			bitIdx++
		}
	}
	bitBuf[bitIdx] = (hash[0] >> 7) & 1 == 1

	words := make([]string, 0, totalBits/11)
	for i := 0; i+11 <= totalBits; i += 11 {
		idx := 0
		for j := 0; j < 11; j++ {
			idx = idx<<1 | boolToInt(bitBuf[i+j])
		}
		if idx >= 2048 {
			return "", errors.New("wallet: invalid word index")
		}
		words = append(words, bip39WordList[idx])
	}
	return strings.Join(words, " "), nil
}

// ValidateMnemonic checks if a mnemonic phrase is valid per BIP-39.
func ValidateMnemonic(mnemonic string) bool {
	words := strings.Fields(mnemonic)
	if len(words) != 24 {
		return false
	}
	for _, w := range words {
		if WordIndex(w) < 0 {
			return false
		}
	}

	// Reconstruct 32-byte entropy from the first 248 bits of the mnemonic.
	entropySize := 32
	entropy := make([]byte, entropySize)
	bitIdx := 0
	for _, word := range words {
		idx := WordIndex(word)
		for i := 10; i >= 0 && bitIdx < entropySize*8; i-- {
			bit := byte((idx >> uint(i)) & 1)
			byteIdx := bitIdx / 8
			bitPos := 7 - uint(bitIdx%8)
			entropy[byteIdx] |= bit << bitPos
			bitIdx++
		}
	}

	// Verify checksum: MSB of SHA256(entropy) must equal the checksum bit in mnemonic.
	hash := sha256.Sum256(entropy)
	expectedCS := uint8((hash[0] >> 7) & 1)

	// Checksum bit is at position 256 in the concatenated bit string.
	// That falls in word 23, bit position 256 % 11 = 3 (0-indexed from MSB).
	// The checksum bit is bit 10-3 = bit 7 of word 23's 11-bit index (0-indexed from MSB).
	// i.e., bit (10 - (256 % 11)) = bit 7 of word 23's index.
	csWordBit := uint(10 - (entropySize*8 % 11)) // = 10 - 3 = 7
	actualCS := uint8((WordIndex(words[entropySize*8/11]) >> csWordBit) & 1)

	return actualCS == expectedCS
}

// MnemonicToSeed derives a 64-byte seed from a mnemonic and optional passphrase.
func MnemonicToSeed(mnemonic string, passphrase string) ([]byte, error) {
	salt := "mnemonic" + passphrase
	return pbkdf2.Key([]byte(mnemonic), []byte(salt), 2048, 64, sha256.New), nil
}

// MnemonicWords splits a mnemonic into its component words.
func MnemonicWords(mnemonic string) []string {
	return strings.Fields(mnemonic)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
