# Outcome

Achieve full feature parity between HiveMachine and OpenMayhem by implementing the remaining decentralized infrastructure: P2P networking, trust tier attestation, and multi-payment rails (TAP/TNK).

# Gap Analysis

| Feature | HiveMachine | OpenMayhem | Gap |
|---------|-------------|------------|-----|
| P2P networking | String stubs | Full libp2p + DHT | P2P networking |
| Trust tiers | AcceptRateLimit float | TPM 2.0 + SEV-SNP + KYB | Trust tier system |
| TAP rail | RailType defined | ERC-20 on Ethereum | Ethereum deposit + settlement |
| TNK rail | RailType defined | Trac native token | Trac Network integration |
| Epoch pricing | ✅ Implemented | ✅ | — |
| Multi-model packing | Routing only | VRAM packing | Rust core needed |
| ComfyUI workflows | ✅ Wired | ✅ | — |
| Dashboards | Out of scope | UI | Out of scope |

# Implementation Phases

## Phase 1: P2P Networking
- Trust tier system design (determines what P2P sessions must prove)
- libp2p host implementation (`pkg/p2p/host.go`)
- DHT-based node discovery (`pkg/p2p/dht.go`)
- Encrypted session establishment (`pkg/p2p/session.go`)
- Session handoff and recovery

## Phase 2: Trust Tier System
- Tier 1: Economic trust (API key + stake)
- Tier 2: TPM 2.0 hardware attestation via Go-TPM
- Tier 3: SEV-SNP confidential compute attestation
- Tier 4: KYB identity verification (deferred, external integration)
- Route filtering by minimum tier

## Phase 3: TAP Payment Rail
- Ethereum wallet derivation (already have in `pkg/wallet/`)
- ERC-20 TAP deposit parsing
- On-chain balance verification
- Settlement and claiming

## Phase 4: TNK Payment Rail
- Trac Network integration
- TNK deposit settlement
- Cross-rail isolation (already implemented)

# Acceptance Criteria

## P2P Networking
- **B1**: P2P host establishes encrypted sessions between nodes
- **B2**: Node discovery via DHT returns list of available providers
- **B3**: Session price locked at open (already implemented in market.go)
- **B4**: Direct and relayed transport fallback works

## Trust Tiers
- **B5**: TPM 2.0 attestation produces valid quote for Tier 2 routes
- **B6**: Route filtering by minimum attestation tier works correctly
- **B7**: Tier 3 SEV-SNP attestation verifies confidential compute

## Payment Rails
- **B8**: TAP deposit flows through Ethereum and credits balance
- **B9**: TNK deposit settles through Trac Network
- **B10**: Cross-rail isolation prevents payment mixing (already implemented in RailStore)

# Non-Goals
- Do NOT implement KYB verification flow (requires external integration)
- Do NOT implement confidential compute attestation in first phase (defer to Phase 2)
- Dashboards remain out of scope for Go gateway codebase
- Multi-model packing requires Rust core changes (deferred)

# Constraints
1. All Go, no Rust code reuse
2. Backward compatible with existing API endpoints
3. P2P implementation must be independently testable
4. Trust tier design must be completed before P2P implementation
