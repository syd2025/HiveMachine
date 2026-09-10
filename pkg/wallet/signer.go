package wallet

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// SignTxRequest groups the inputs for signing an Ethereum transaction.
type SignTxRequest struct {
	To       string
	Nonce    uint64
	GasLimit uint64
	GasPrice int64 // in wei
	Value    int64 // in wei
	Data     []byte
	ChainID  int64
}

// SignErrorCode classifies signing failures.
type SignErrorCode int

const (
	SignErrorUnknown SignErrorCode = iota
	SignErrorLocked
	SignErrorChainUnsupported
	SignErrorInvalidAddress
	SignErrorCrypto
)

// SignError wraps a signing failure.
type SignError struct {
	Code    SignErrorCode
	Message string
	Err     error
}

func (e *SignError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("sign: [%d] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("sign: [%d] %s", e.Code, e.Message)
}

func (e *SignError) Unwrap() error { return e.Err }

// DerivePrivateKey derives an ECDSA private key from the 64-byte mnemonic seed
// using the first 32 bytes (BIP-44 derivation: m/44'/60'/0'/0/0 uses seed directly).
func DerivePrivateKey(seed []byte) (*ecdsa.PrivateKey, error) {
	if len(seed) < 32 {
		return nil, fmt.Errorf("seed too short: need at least 32 bytes")
	}
	// Use only the first 32 bytes as the private key.
	priv, err := crypto.ToECDSA(seed[:32])
	if err != nil {
		return nil, fmt.Errorf("derive key from seed: %w", err)
	}
	return priv, nil
}

// SignEthereumTx signs an Ethereum transaction using the provided private key.
// The signature follows EIP-155 (chain ID encoded in v value).
func SignEthereumTx(req *SignTxRequest, priv *ecdsa.PrivateKey) ([]byte, error) {
	if !common.IsHexAddress(req.To) {
		return nil, &SignError{Code: SignErrorInvalidAddress, Message: "invalid recipient address"}
	}

	to := common.HexToAddress(req.To)

	chainID := big.NewInt(req.ChainID)
	gasPrice := big.NewInt(req.GasPrice)
	if req.GasPrice == 0 {
		gasPrice = big.NewInt(50_000_000_000) // 50 gwei fallback
	}

	var gasTipCap, gasFeeCap *big.Int
	if req.ChainID != 1 { // non-mainnet: use dynamic fee
		gasTipCap = new(big.Int).Div(gasPrice, big.NewInt(2))
		gasFeeCap = gasPrice
	} else {
		gasTipCap = big.NewInt(2_000_000_000)
		gasFeeCap = big.NewInt(100_000_000_000)
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     req.Nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       req.GasLimit,
		To:        &to,
		Value:     big.NewInt(req.Value),
		Data:      req.Data,
	})

	signer := types.LatestSignerForChainID(chainID)
	signed, err := types.SignTx(tx, signer, priv)
	if err != nil {
		return nil, &SignError{Code: SignErrorCrypto, Message: "signing failed", Err: err}
	}

	encoded, err := signed.MarshalBinary()
	if err != nil {
		return nil, &SignError{Code: SignErrorCrypto, Message: "rlp encoding failed", Err: err}
	}

	return encoded, nil
}

// SignMessage returns the ECDSA signature of msg using priv.
// The message is prefixed with "\x19Ethereum Signed Message:\n<len>" per EIP-191.
func SignMessage(msg []byte, priv *ecdsa.PrivateKey) ([]byte, error) {
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	signable := []byte(prefix)
	signable = append(signable, msg...)

	h := crypto.Keccak256Hash(signable)
	sig, err := crypto.Sign(h.Bytes(), priv)
	if err != nil {
		return nil, &SignError{Code: SignErrorCrypto, Message: "message signing failed", Err: err}
	}
	return sig, nil
}

// VerifySignature checks that sig is a valid EIP-191 signature of msg by addr.
func VerifySignature(addr string, msg, sig []byte) bool {
	if !common.IsHexAddress(addr) {
		return false
	}
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	signable := append([]byte(prefix), msg...)
	h := crypto.Keccak256Hash(signable)

	// crypto.SigToPub recovers the public key from the signature.
	pubKey, err := crypto.SigToPub(h.Bytes(), sig)
	if err != nil {
		return false
	}
	derivedAddr := crypto.PubkeyToAddress(*pubKey)
	return derivedAddr == common.HexToAddress(addr)
}
