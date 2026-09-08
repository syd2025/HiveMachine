# Wallet & Identity Specification

## Overview

Implement wallet management for cryptographic operations, mnemonic-based key derivation, and payout destination binding. The wallet system is independent from API Key authentication.

## Wallet Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Wallet Service                         │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Key Derive  │  │  Signing    │  │  Address Management │  │
│  │ (BIP39/32)  │  │  (secp256k1)│  │  (Multi-chain)      │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│                                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Encryption  │  │  Mnemonic   │  │  Payout Binding     │  │
│  │ (AES-256)   │  │  (BIP39)    │  │  (On-chain verify)  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Data Structures

### Wallet

```go
// Wallet holds cryptographic material
type Wallet struct {
    ID           WalletID
    Address      map[ChainType]string  // Address per chain
    Source       WalletSource
    CreatedAt    time.Time
    LastActivity time.Time
    Encrypted    bool
}

// ChainType for multi-chain support
type ChainType string

const (
    ChainEthereum ChainType = "ethereum"  // TAP payments
    ChainTrac     ChainType = "trac"      // TNK payments
)

// WalletSource
type WalletSource int

const (
    WalletSourceGenerated WalletSource = iota  // New mnemonic
    WalletSourceImported                       // Imported key/mnemonic
)

// EncryptedKeyStore for secure key storage
type EncryptedKeyStore struct {
    Cipher     string  // "aes-256-gcm"
    KDF        string  // "argon2i"
    Salt       []byte
    Nonce      []byte
    CipherText []byte  // Encrypted mnemonic/key
}
```

### Mnemonic Operations

```go
// MnemonicWords is a validated BIP39 mnemonic
type MnemonicWords []string

// GenerateMnemonic creates a new 24-word mnemonic
func GenerateMnemonic() (MnemonicWords, error) {
    entropy := make([]byte, 32)  // 256 bits
    if _, err := rand.Read(entropy); err != nil {
        return nil, err
    }
    
    mnemonic, err := bip39.NewEntropyToMnemonic(entropy)
    if err != nil {
        return nil, err
    }
    
    return strings.Split(mnemonic, " "), nil
}

// MnemonicToSeed derives seed from mnemonic
func MnemonicToSeed(mnemonic MnemonicWords, passphrase string) ([]byte, error) {
    return bip39.NewSeed(strings.Join(mnemonic, " "), passphrase)
}

// SeedToKey derives private key from seed
func SeedToKey(seed []byte, path DerivationPath) ([]byte, error) {
    // Use go-ethereum HD wallet derivation
    wallet, err := hd.NewWalletFromSeed(seed)
    if err != nil {
        return nil, err
    }
    
    key, err := wallet.Derive(path, false)
    if err != nil {
        return nil, err
    }
    
    return key.PrivateKey.Bytes(), nil
}

// Default derivation paths
const (
    EthereumDerivationPath = "m/44'/60'/0'/0/0"
    TracDerivationPath     = "m/44'/966'/0'/0/0"  // Trac uses coin type 966
)
```

### Payout Binding

```go
// PayoutBinding for receiving payments
type PayoutBinding struct {
    ID          BindingID
    ProviderID  string
    Rail        RailType
    Chain       ChainType
    Address     string
    Status      BindingState
    ActivatesAt int64   // Epoch number
    Signature   []byte  // Provider signature proving ownership
    Proof       []byte  // On-chain proof (e.g., signed message)
    CreatedAt   time.Time
    VerifiedAt  *time.Time
}

// BindingState
type BindingState int

const (
    BindingStatePending BindingState = iota
    BindingStateVerified    // Ownership verified
    BindingStateActive      // Can receive payments
    BindingStateRotated     // Replaced by new binding
    BindingStateRevoked     // Provider removed binding
)

// BindingProof for ownership verification
type BindingProof struct {
    Challenge   string  // Random challenge from system
    Address     string  // Address being bound
    Signature   []byte  // Signature of challenge by address private key
    Chain       ChainType
}
```

## Key Derivation

### BIP39 Mnemonic

```go
// Supported languages
type WordListLanguage string

const (
    LanguageEnglish WordListLanguage = "english"
    LanguageChinese WordListLanguage = "chinese_simplified"
    LanguageJapanese WordListLanguage = "japanese"
)

// Create wallet from mnemonic
func CreateWalletFromMnemonic(
    mnemonic MnemonicWords,
    password string,
    language WordListLanguage,
) (*Wallet, error) {
    // Validate mnemonic
    if !bip39.IsMnemonicValid(strings.Join(mnemonic, " ")) {
        return nil, ErrInvalidMnemonic
    }
    
    // Derive seed
    seed, err := MnemonicToSeed(mnemonic, "mnemonic"+password)
    if err != nil {
        return nil, err
    }
    
    // Derive keys for each chain
    addresses := make(map[ChainType]string)
    
    ethKey, err := SeedToKey(seed, EthereumDerivationPath)
    if err != nil {
        return nil, err
    }
    addresses[ChainEthereum] = crypto.PubkeyToAddress(crypto.FromECDSAPub(
        crypto.S256().Pubkey().(*ecdsa.PublicKey),
    ))
    
    tracKey, err := SeedToKey(seed, TracDerivationPath)
    if err != nil {
        return nil, err
    }
    addresses[ChainTrac] = deriveTracAddress(tracKey)
    
    // Encrypt and store
    encryptedStore, err := encryptMnemonic(mnemonic, password)
    if err != nil {
        return nil, err
    }
    
    return &Wallet{
        ID:      NewWalletID(),
        Address: addresses,
        Source:  WalletSourceImported,
    }, nil
}
```

## Encryption

### Secure Storage

```go
// Encrypt mnemonic with Argon2i + AES-256-GCM
func encryptMnemonic(mnemonic MnemonicWords, password string) (*EncryptedKeyStore, error) {
    // Derive key using Argon2id
    salt := make([]byte, 32)
    rand.Read(salt)
    
    key, err := argon2id.Key(
        []byte(password),
        salt,
        3,      // iterations
        64*1024, // memory (64 MB)
        4,      // parallelism
        32,     // key length
    )
    if err != nil {
        return nil, err
    }
    
    // Encrypt with AES-256-GCM
    cipher, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    
    gcm, err := cipher.NewGCM(cipher)
    if err != nil {
        return nil, err
    }
    
    nonce := make([]byte, gcm.NonceSize())
    rand.Read(nonce)
    
    plaintext := []byte(strings.Join(mnemonic, " "))
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    
    return &EncryptedKeyStore{
        Cipher:     "aes-256-gcm",
        KDF:        "argon2id",
        Salt:       salt,
        Nonce:      nonce[:gcm.NonceSize()],
        CipherText: ciphertext[gcm.NonceSize():],
    }, nil
}

// Decrypt mnemonic
func (s *EncryptedKeyStore) Decrypt(password string) (MnemonicWords, error) {
    // Derive key
    key, err := argon2id.Key(
        []byte(password),
        s.Salt,
        3, 64*1024, 4, 32,
    )
    if err != nil {
        return nil, err
    }
    
    // Decrypt
    cipher, _ := aes.NewCipher(key)
    gcm, _ := cipher.NewGCM(cipher)
    
    plaintext, err := gcm.Open(nil, s.Nonce, s.CipherText, nil)
    if err != nil {
        return nil, ErrInvalidPassword
    }
    
    return strings.Split(string(plaintext), " "), nil
}
```

## API Interface

```go
// WalletService for wallet operations
type WalletService interface {
    // Creation
    CreateWallet(ctx context.Context, password string) (*Wallet, error)
    RestoreWallet(ctx context.Context, mnemonic string, password string) (*Wallet, error)
    
    // Backup/Restore
    GetMnemonic(ctx context.Context, password string) (string, error)  // Requires confirmation
    ExportPrivateKey(ctx context.Context, chain ChainType, password string) (string, error)
    
    // Address queries
    GetAddress(ctx context.Context, chain ChainType) (string, error)
    ListAddresses(ctx context.Context) (map[ChainType]string, error)
    
    // Signing
    SignMessage(ctx context.Context, chain ChainType, message []byte) ([]byte, error)
    SignTransaction(ctx context.Context, chain ChainType, tx interface{}) ([]byte, error)
    
    // Payout bindings
    CreatePayoutBinding(ctx context.Context, rail RailType, address string) (*PayoutBinding, error)
    VerifyPayoutBinding(ctx context.Context, bindingID BindingID, proof *BindingProof) error
    RotatePayoutBinding(ctx context.Context, rail RailType, newAddress string) error
    GetPayoutBinding(ctx context.Context, rail RailType) (*PayoutBinding, error)
    
    // Security
    ChangePassword(ctx context.Context, oldPassword, newPassword string) error
    LockWallet(ctx context.Context) error
    UnlockWallet(ctx context.Context, password string) error
}
```

## Binding Verification

### Ethereum Address Ownership

```go
// Verify Ethereum address ownership
func VerifyEthereumOwnership(address string, proof *BindingProof) error {
    // Recover signer from signature
    msgHash := crypto.Keccak256Hash([]byte(proof.Challenge))
    sigPublicKey, err := crypto.SigToPub(msgHash.Bytes(), proof.Signature)
    if err != nil {
        return ErrInvalidSignature
    }
    
    recoveredAddress := crypto.PubkeyToAddress(*sigPublicKey)
    if !strings.EqualFold(recoveredAddress.Hex(), address) {
        return ErrAddressMismatch
    }
    
    return nil
}

// Challenge generation for binding
func GenerateBindingChallenge() string {
    challenge := make([]byte, 32)
    rand.Read(challenge)
    return hex.EncodeToString(challenge)
}
```

## CLI Commands

```bash
# Wallet operations
mayhem wallet show              # Show addresses
mayhem wallet backup            # Reveal mnemonic (requires confirmation)
mayhem wallet import            # Import existing wallet
mayhem wallet passwd            # Change password

# Payout bindings
mayhem provider payout set --rail tap --submit     # Set TAP payout
mayhem provider payout set --rail tnk --submit     # Set TNK payout
mayhem provider payout set --rail fiat --submit    # Set Stripe payout
mayhem provider payout get                         # Show current binding
mayhem provider payout rotate --rail tap --submit  # Rotate binding

# Status
mayhem balance                  # Show balances per rail
mayhem deposit status           # Show pending deposits
```

## Acceptance Criteria

- **A20.1**: New wallet generates valid 24-word mnemonic
- **A20.2**: Mnemonic backup and restore works correctly
- **A20.3**: Different passwords produce different encrypted stores
- **A20.4**: Invalid password fails decryption
- **A21.1**: Payout binding signature verifies correctly
- **A21.2**: Binding activates at correct epoch
- **A21.3**: Address derivation matches standard (m/44'/60'/0'/0/0)
- **A21.4**: Multiple chain addresses derived from single mnemonic

## Security Considerations

### Key Storage

1. **Never store plaintext keys on disk**
2. **Use Argon2id for password-based key derivation**
3. **AES-256-GCM for authenticated encryption**
4. **Secure memory handling for sensitive data**

### Operational Security

1. **Mnemonic shown only once during creation**
2. **Backup requires explicit user confirmation**
3. **Password change re-encrypts all data**
4. **Auto-lock after inactivity period**

## Error Codes

| Error | Description |
|-------|-------------|
| `wallet.invalid_mnemonic` | Mnemonic phrase is invalid |
| `wallet.invalid_password` | Password incorrect |
| `wallet.locked` | Wallet is locked |
| `wallet.no_wallet` | No wallet exists |
| `wallet.binding_failed` | Payout binding verification failed |
| `wallet.signature_invalid` | Signature verification failed |
| `wallet.derivation_failed` | Key derivation failed |
