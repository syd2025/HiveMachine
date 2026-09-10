// Package wallet provides cryptographic wallet management for HiveMachine.
// hd.go implements BIP-32 hierarchical deterministic key derivation.
package wallet

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha512"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Derivation path constants per the spec.
const (
	// EthereumDerivationPath is BIP-44 for Ethereum (coin_type=60).
	EthereumDerivationPath = "m/44'/60'/0'/0/0"
	// TracDerivationPath is BIP-44 for OriginTrail (coin_type=966).
	TracDerivationPath = "m/44'/966'/0'/0/0"
)

var chainDerivationPaths = map[ChainType]string{
	ChainEthereum: EthereumDerivationPath,
	ChainTrac:     TracDerivationPath,
}

// DeriveForPath derives a private key from seed following BIP-32.
func DeriveForPath(seed []byte, pathStr string) ([]byte, error) {
	path, err := accounts.ParseDerivationPath(pathStr)
	if err != nil {
		return nil, err
	}
	return DeriveKeyFromPath(seed, path)
}

// DeriveKeyFromPath derives a private key from seed following the given path.
func DeriveKeyFromPath(seed []byte, path accounts.DerivationPath) ([]byte, error) {
	if len(seed) != 64 {
		return nil, ErrInvalidSeed
	}
	key := seed
	for _, idx := range path {
		var err error
		key, err = deriveChildKey(key, idx)
		if err != nil {
			return nil, err
		}
	}
	return key, nil
}

// DerivePrivateKeyFromSeed derives an ECDSA private key from a BIP39 seed
// using the default Ethereum derivation path.
func DerivePrivateKeyFromSeed(seed []byte) (*ecdsa.PrivateKey, error) {
	if len(seed) != 64 {
		return nil, ErrInvalidSeed
	}
	keyBytes, err := DeriveKeyFromPath(seed, mustParsePath(EthereumDerivationPath))
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(keyBytes)
}

// DeriveTracPrivateKey derives a private key for Trac using the Trac derivation path.
func DeriveTracPrivateKey(seed []byte) (*ecdsa.PrivateKey, error) {
	if len(seed) != 64 {
		return nil, ErrInvalidSeed
	}
	keyBytes, err := DeriveKeyFromPath(seed, mustParsePath(TracDerivationPath))
	if err != nil {
		return nil, err
	}
	return crypto.ToECDSA(keyBytes)
}

// DeriveAddress derives the Ethereum address for a private key.
func DeriveAddress(priv *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(priv.PublicKey)
}

// DeriveAddressFromSeed derives the address for a chain from a BIP39 seed.
func DeriveAddressFromSeed(seed []byte, chain ChainType) (string, error) {
	if len(seed) != 64 {
		return "", ErrInvalidSeed
	}
	pathStr, ok := chainDerivationPaths[chain]
	if !ok {
		return "", fmt.Errorf("wallet: no derivation path for chain %s", chain)
	}
	keyBytes, err := DeriveKeyFromPath(seed, mustParsePath(pathStr))
	if err != nil {
		return "", err
	}
	priv, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(priv.PublicKey).Hex(), nil
}

// ---------------------------------------------------------------------------
// BIP-32 child key derivation
// ---------------------------------------------------------------------------

func deriveChildKey(parentKey []byte, index uint32) ([]byte, error) {
	hardened := index >= 0x80000000

	var data []byte
	if hardened {
		data = append([]byte{0x00}, parentKey...)
	} else {
		priv, err := crypto.ToECDSA(parentKey)
		if err != nil {
			return nil, err
		}
		pub := crypto.CompressPubkey(&priv.PublicKey)
		data = append(pub, uint32ToBytes(index)...)
	}
	data = append(data, uint32ToBytes(index)...)

	h := hmac.New(sha512.New, parentKey)
	h.Write(data)
	il := h.Sum(nil)[:32]

	ilInt := bytesToInt(il)
	parentInt := bytesToInt(parentKey)
	n := curveN()

	child := new(big.Int).Add(ilInt, parentInt)
	child.Mod(child, n)
	if ilInt.Sign() == 0 || child.Sign() == 0 {
		return nil, ErrDeriveFailed
	}

	out := make([]byte, 32)
	child.FillBytes(out)
	return out, nil
}

func mustParsePath(s string) accounts.DerivationPath {
	path, err := accounts.ParseDerivationPath(s)
	if err != nil {
		panic(err)
	}
	return path
}

func uint32ToBytes(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func bytesToInt(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

// secp256k1 group order n = 2^256 - 2^32 - 2^9 - 2^8 - 2^7 - 2^6 - 2^4 - 1.
func curveN() *big.Int {
	n, _ := new(big.Int).SetString("115792089237316195423570985008687907852837564279074904382605163141518161494337", 10)
	return n
}
