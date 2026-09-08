# Trust Tiers Specification

## Overview

Implement a multi-tier trust system that provides verifiable security guarantees for AI inference, ranging from basic economic trust to hardware-attested confidential computing.

## Trust Tier Levels

| Tier | Name | Attestation | Prompt Privacy | Price Tier |
|------|------|-------------|----------------|------------|
| T1 | Economic | None | ❌ No | Base |
| T2 | Hardware | TPM 2.0 / TEE | ❌ No | Premium |
| T3 | Confidential | SEV-SNP / TDX | ✅ Yes | Highest |
| T4 | KYB | Business Verification | Depends | Premium |

## Tier Definitions

### Tier 1: Economic Trust (Baseline)

**Requirements:**
- Running official HiveMachine software
- Economic incentives through deposit/holdback
- Spot-check verification of model output
- Receipt signing and settlement

**Trust Mechanism:**
- Providers stake economic value
- Dishonest behavior results in financial penalty
- Receipts are signed and verifiable

### Tier 2: Hardware Attestation

**Requirements:**
- TPM 2.0 quote from hardware security chip
- Unique machine identity bound to quote
- Attestation report verification
- Quote freshness validation

**Supported Platforms:**
- TPM 2.0 on Linux/Windows
- Intel TDX (DCAP)
- AMD SEV-SNP (VCEK)
- Apple App Attest
- NVIDIA GB10 Device JWT
- NVIDIA NRAS JWT

**Trust Mechanism:**
- Hardware immutable identity
- Quote proves genuine hardware
- Nonce prevents replay attacks

### Tier 3: Confidential Compute

**Requirements:**
- AMD SEV-SNP or Intel TDX in confidential mode
- GPU in CC mode
- Attestation covers both CPU and GPU
- Memory encryption active

**Trust Mechanism:**
- Even provider cannot read prompts
- Memory encryption by hardware
- Remote attestation of entire stack

### Tier 4: KYB (Know Your Business)

**Requirements:**
- Legal entity verification
- Business identity documentation
- Published identity record
- Ongoing compliance

**Trust Mechanism:**
- Real-world legal accountability
- Public identity on ledger
- Standard business verification

## Data Structures

### Attestation Types

```go
// AttestationTier represents trust tier level
type AttestationTier int

const (
    Tier1 AttestationTier = 1
    Tier2 AttestationTier = 2
    Tier3 AttestationTier = 3
    Tier4 AttestationTier = 4
)

// AttestationRequest for verification
type AttestationRequest struct {
    Report               []byte   // Platform quote/report
    Contract             []byte   // Expected contract
    ExpectedNonce        []byte   // Freshness nonce
    ExpectedProviderPubkey []byte // Provider identity
    NowTS                int64    // Request timestamp
    HardwareQuoteVerifierCommand string // External verifier
}

// VerifiedAttestation after successful verification
type VerifiedAttestation struct {
    EnclaveID        []byte         // Unique enclave identity
    ProviderPubkey   []byte         // Provider's public key
    EnclavePubkey    []byte         // Enclave's public key
    ReportHead       []byte         // Attestation report header
    BootEpoch        int64          // Boot timestamp
    AttTier          AttestationTier // Tier level
    ExecutionMode    string         // Runtime mode
    VerifiedAt       time.Time      // Verification time
}

// Quote types
type QuoteType string

const (
    QuoteTPM2         QuoteType = "tpm2-quote-ek"
    QuoteIntelTDX     QuoteType = "tdx-quote-dcap"
    QuoteAMDSEVSNP    QuoteType = "sev-snp-vcek"
    QuoteAppleAppAttest QuoteType = "apple-app-attest"
    QuoteNVIDIAGB10   QuoteType = "nvidia-gb10-device-jwt"
)
```

### Route Filtering

```go
// RouteAttestationFilter for minimum tier requirements
type RouteAttestationFilter struct {
    MinTier        AttestationTier
    RequireKYB     bool
    MinContext     int64
    RequiredQuant  string
    MinAttTier     *int // From request header
}

// ProviderAttestationState reflects route status
type ProviderAttestationState int

const (
    AttestationMissing AttestationState = iota
    AttestationStale
    AttestationReady
    AttestationExpired
)
```

### Provider Heartbeat with Attestation

```go
// ProviderHeartbeat with attestation head
type ProviderHeartbeat struct {
    Type              string            `json:"t"` // "hb"
    Version           int               `json:"v"`
    ContractVersion   int               `json:"contract_version"`
    Provider          string            `json:"provider"`
    EnclaveID         string            `json:"enclave_id"`
    ModelID           string            `json:"model_id"`
    RoomID            string            `json:"room_id"`
    Slots             HeartbeatSlots    `json:"slots"`
    Queue             HeartbeatQueue    `json:"q"`
    Perf              HeartbeatPerf     `json:"perf"`
    MinAskAU          int64             `json:"min_ask_au"` // Min price in atto-USD
    AttestationHead   []byte            `json:"att"`        // Latest attestation hash
}

type HeartbeatSlots struct {
    Active    int `json:"active"`    // Active requests
    Max       int `json:"max"`       // Maximum capacity
    Free      int `json:"free"`      // Available slots
}

type HeartbeatQueue struct {
    EngineBacklog int `json:"engine_backlog"` // Pending requests
    EstWaitMs     int `json:"est_wait_ms"`    // Estimated wait
}

type HeartbeatPerf struct {
    TokPerSec float64 `json:"tok_s"` // Tokens per second
    TTFTMs    float64 `json:"ttft_ms"` // Time to first token
}
```

## Attestation Verification Flow

### Tier 2 TPM 2.0 Flow

```
1. Provider generates quote:
   - Create nonce
   - Call TPM2_Quote with EK handle
   - Include PCR quotes and AIK signature
   
2. Gateway verification:
   - Verify quote signature
   - Check nonce freshness (not replayed)
   - Validate PCR measurements
   - Verify EK certificate chain
   
3. Result:
   - If valid: Store VerifiedAttestation
   - If invalid: Reject route
```

### Tier 3 SEV-SNP Flow

```
1. Provider generates attestation:
   - VM launches with SNP enabled
   - Hardware generates attestation report
   - Report includes VM measurement and VCEK signature
   
2. Gateway verification:
   - Verify VCEK certificate chain
   - Validate attestation report structure
   - Check VMPL and guest policy
   - Verify reported measurements
   
3. Result:
   - If valid: Route eligible for T3
   - Confidential prompts can be routed
```

## State Machine

### Route Attestation State

```
                    ┌─────────────────────────────────────┐
                    │                                     │
                    ▼                                     │
              ┌──────────┐     missing      ┌─────────────┴───┐
   Start ───► │  Live    │◄───────────────► │ AttestationMissing│
              └──────────┘     ready        └──────────────────┘
                    │                                     ▲
                    │stale              stale              │
                    ▼                                     │
         ┌──────────────────┐                             │
         │ AttestationStale │─────────────────────────────┘
         └──────────────────┘            ready
```

### Heartbeat Validation Pipeline

```
1. SchemaCheck      - Verify message schema
2. ContractVersionCheck - Verify contract version
3. TimeValidation   - Check freshness (not stale)
4. SignatureVerification - Verify provider signature
5. ReplayCheck      - Ensure nonce not replayed
6. Success/Reject   - Route becomes Live or dropped
```

## API Interface

```go
// AttestationService handles tier verification
type AttestationService interface {
    // Quote generation (provider side)
    GenerateQuote(ctx context.Context, quoteType QuoteType, nonce []byte) ([]byte, error)
    
    // Quote verification (gateway side)
    VerifyQuote(ctx context.Context, req *AttestationRequest) (*VerifiedAttestation, error)
    
    // State management
    GetAttestationState(providerID string, enclaveID string) (ProviderAttestationState, error)
    GetVerifiedAttestation(providerID string) (*VerifiedAttestation, error)
    
    // Filtering
    FilterByTier(candidates []RouteCandidate, minTier AttestationTier) []RouteCandidate
    MeetsTier(attestation *VerifiedAttestation, requiredTier AttestationTier) bool
}

// TPMService for TPM operations
type TPMService interface {
    CreateAIK(ctx context.Context) (tpm2.ResourceContext, error)
    Quote(ctx context.Context, aik tpm2.ResourceContext, nonce []byte, pcrs []int) ([]byte, error)
    VerifyQuote(quote []byte, nonce []byte, ekCert []byte) (bool, error)
}
```

## Configuration

```go
type TrustTierConfig struct {
    // Tier settings
    DefaultTier       AttestationTier
    RequireAttestation bool
    
    // TPM settings
    TPMDevice         string        // /dev/tpmrm0 or similar
    TPMVersion        string        // "2.0"
    EKCertificatePath string        // EK certificate file
    
    // Quote validation
    QuoteFreshnessMax time.Duration // Max age for quotes
    RequireEKCert     bool          // Require EK certificate validation
    
    // External verifier
    UseExternalVerifier bool
    VerifierCommand    string       // Path to external verifier script
    
    // Cache
    AttestationCacheTTL time.Duration
    
    // TEE settings
    TEEMode           string        // "sev-snp", "tdx", "none"
    VCEKCachePath     string        // AMD VCEK certificate cache
}
```

## Error Codes

| Error | Description |
|-------|-------------|
| `attestation.missing` | No attestation provided |
| `attestation.stale` | Quote is too old |
| `attestation.invalid` | Quote verification failed |
| `attestation.replay` | Nonce was previously used |
| `attestation.tier_insufficient` | Provider tier below requirement |
| `tpm.not_found` | TPM device not available |
| `tpm.quote_failed` | Failed to generate TPM quote |
| `tee.boot_failed` | TEE failed to boot |

## Acceptance Criteria

- **A3.1**: TPM 2.0 quote generation succeeds on Linux/Windows with TPM
- **A3.2**: Quote verification validates signature and nonce
- **A3.3**: Route filtering excludes providers below minimum tier
- **A3.4**: Attestation state transitions correctly (Live → Stale → Live)
- **A3.5**: Replay attacks are detected and rejected
- **A3.6**: SEV-SNP attestation verification validates VCEK chain
- **A3.7**: KYB verification integration point defined (external service)
- **A3.8**: Heartbeat validation pipeline enforces attestation requirements

## Dependencies

```go
// Required Go modules
github.com/google/go-attestation  // TPM operations
github.com/google/go-tpm-tools    // TPM quote verification
github.com/IBM/secret-ballot      // TEE attestation (if needed)
```
