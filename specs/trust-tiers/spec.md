# Trust Tiers Specification

## Overview

Trust tiers establish minimum attestation requirements for routing decisions. Providers self-declare their tier; the gateway enforces tier-based route filtering. Higher tiers unlock access to more sensitive workloads and premium pricing.

## Tier Definitions

| Tier | Name | Attestation | Requirements |
|------|------|-------------|--------------|
| 0 | Anonymous | None | Basic API key only |
| 1 | Economic | API key + deposit | Economic stake deposited; slashed on misbehavior |
| 2 | Hardware | TPM 2.0 quote | HW-bound key in TPM; quote signed by endorsement key |
| 3 | Confidential | AMD SEV-SNP | VM enclave; remote attestation report verified |
| 4 | KYB | Business verification | Identity verification, contract, AML/KYC |

**This spec covers Tiers 1–2.** Tiers 3–4 are out of scope for this phase (see brief.md non-goals).

## Tier 1: Economic Trust

### Requirements
- Provider deposits stake into a bonding contract (on-chain or gateway-managed escrow)
- Stake is locked for the duration of provider activity
- Misbehavior (downtime, incorrect inference, equivocation) triggers slashing

### Data Model

```go
type EconomicTrust struct {
    Tier           TrustTier   // Always Tier1
    StakeCents     int64       // Locked stake in cents
    StakeLocked    bool        // True once deposited and epoch started
    Slashed        bool        // True if slashed (provider ejected)
    SlashReason    string      // Human-readable slash reason
    SlashedAt      *time.Time  // When slashed
}
```

### Slash Conditions
- More than 3 missed heartbeats in a row → soft slash (reputation penalty)
- Repeated soft slashes → hard slash (stake forfeited, ejected)
- Attestation failure on request → hard slash

### Gateway Behavior
- Tier 1 providers are included in routing by default
- If `StakeCents < minimum_stake`, provider excluded from routing
- Gateway periodically re-checks stake liveness via on-chain query or receipt chain

## Tier 2: TPM 2.0 Hardware Attestation

### Requirements
- Provider machine has a TPM 2.0 chip
- Provider generates an Attestation Identity Key (AIK) in the TPM
- AIK is registered with the gateway via a one-time enrollment ceremony
- Each request is accompanied by a TPM quote (nonce + session transcript)

### Enrollment Ceremony

1. Provider generates an AIK in TPM via `TPM2_CreateAK`
2. Provider obtains an AIK certificate from a Privacy CA (or self-signed for internal deployments)
3. Provider sends AIK public blob + certificate to gateway
4. Gateway verifies certificate chain; stores AIK public key

### TPM Quote Structure

```
QuoteData = {
    nonce:        random_16_bytes,   // Prevent replay
    session_id:   uuid,              // Identify the inference session
    timestamp:    unix_seconds,      // Quote freshness
    pcr_sha256:   [PCR0..PCR23],     // Platform config registers
}

Quote = TPM2_Quote(AIK, QuoteData, signature_algorithm)
```

### Quote Verification (gateway side)

1. Extract AIK public key from stored enrollment
2. Verify signature: `TPM2_VerifyQuote(AIK_pub, QuoteData, quote.signature)`
3. Verify nonce matches the nonce sent for this session
4. Verify timestamp is within ±5 minutes of gateway clock
5. Verify PCR values match expected known-good reference (optional, configurable)
6. If all pass → tier = 2 for this request; record in receipt

### Data Model

```go
type TPMTrust struct {
    Tier         TrustTier  // Always Tier2
    AIKPublic    []byte     // TPM AIK public key (DER-encoded)
    AIKCert      []byte     // AIK certificate (X.509)
    PCREphemeral [24]string // Expected PCR values at enrollment (SHA-256 hex)
    EnrolledAt   time.Time  // Enrollment timestamp
    NonceCounter uint64     // Monotonic counter to prevent nonce reuse
}
```

### Quote Verification Code

```go
// VerifyTPMQuote verifies a TPM 2.0 quote for a Tier 2 provider.
// Returns error if: signature invalid, nonce mismatch, timestamp stale, PCR mismatch.
func VerifyTPMQuote(aikPublic []byte, quoteData, signature []byte, nonce []byte, sessionID string, timestamp int64, pcrs [24]string) error
```

## Trust Tier Selection in Routing

```go
// RouteFilter contains tier requirements for a request.
type RouteFilter struct {
    MinimumTier TrustTier  // Minimum tier required
    RequireTPM  bool       // Force TPM quote even if tier 1 (for high-value sessions)
}

// Infer selects a provider meeting the minimum tier requirement.
func (r *Router) Infer(ctx context.Context, filter RouteFilter, model string) (*Provider, error)
```

## Open Questions

- **Q1**: How does the gateway obtain the TPM endorsement key certificate chain? (Privacy CA vs. self-signed for internal)
- **Q2**: What is the minimum stake amount for Tier 1? (Configurable via `--min-stake-cents`)
- **Q3**: How is slashing enforced on-chain? (Gateway-managed escrow vs. external smart contract)
- **Q4**: How are PCR reference values updated when provider firmware changes?

## Constraints

- Tier enforcement is per-request, not per-connection
- A provider operating at Tier 2 cannot be demoted to Tier 1 within an active session
- Gateway must cache verified AIK public keys to avoid per-request TPM CA lookups
- TPM quote verification latency must be < 50ms (budgeted within inference routing)

## Implementation Notes

- TPM operations use the `github.com/google/go-tpm-tools` library
- AIK enrollment is a one-time setup step via `mayhem provider enroll-tpm <provider_id>`
- Quote verification is called inside the routing hot path; cache verified quotes for the session lifetime
