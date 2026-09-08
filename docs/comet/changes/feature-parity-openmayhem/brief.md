# Outcome

Achieve full functional parity between HiveMachine (Go) and OpenMayhem (Rust), transforming HiveMachine from a simplified gateway proxy into a complete P2P AI inference marketplace implementation.

**Target**: HiveMachine implements all OpenMayhem features natively, including P2P networking, trust tiers, market pricing, multi-payment rails, and full workflow execution.

# Scope

## Source coverage

| Source | Status | Coverage | Notes |
|--------|--------|----------|-------|
| https://github.com/Trac-Systems/openmayhem | complete | background | Reference implementation; not directly implemented |
| HiveMachine README.md | complete | covered | Current implementation scope |
| docs/roadmap-feature-completion.md | complete | covered | Previous feature gap analysis |

## Feature parity breakdown

### Currently Implemented in HiveMachine (✅)

1. **API Layer**
   - OpenAI-compatible REST API: `/v1/models`, `/v1/chat/completions`, `/v1/completions`
   - SSE Streaming: `/v1/chat/completions/stream`
   - gRPC communication with Rust core

2. **Resilience**
   - Multi-backend failover with ranked providers
   - Calibration matrix: P50/P95/P99 latency tracking per model+provider
   - Endpoint health scoring and exclusion

3. **Provider Management**
   - Provider registry with heartbeat (30s interval)
   - Reputation scoring (uptime + latency weighted)
   - Provider routing based on reputation
   - Min-ask price per model (provider sets minimum acceptable CPM)

5. **Market Mechanism**
   - Epoch-based dynamic pricing (hourly epochs, utilization-driven multiplier)
   - Price locking per session (locked at open, never repriced mid-session)
   - Market state publication with HMAC signature
   - Cross-rail balance isolation

6. **Extended APIs**
   - `/v1/embeddings`, `/v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions` — 501 stubs (Rust core lacks support)
   - `/v1/workflows` — async job execution via jobs handler
   - `/v1/jobs`, `/v1/jobs/:id/events`, `DELETE /v1/jobs/:id` — job lifecycle

7. **Receipts & Verification**
   - Ed25519 receipt signing (provider keys)
   - Receipt verification endpoint: `POST /v1/receipts/verify`
   - Public key served at `GET /v1/receipts/public_key`
8. **Billing & Payments**
   - Token attribution per API key
   - Quota enforcement (429 on exceeded)
   - Stripe payments via paygate

9. **CLI Tools**
   - `mayhem up/down` - gateway lifecycle
   - `mayhem doctor` - health checks
   - `mayhem models` - list available models
   - `mayhem provider` - provider management
   - `mayhem pay` - payment operations

### Missing Features (❌) - Must Implement

1. **P2P Networking**
   - Encrypted peer-to-peer session establishment
   - Node discovery via DHT (Trac Network)
   - Direct and relayed session transport
   - Session handoff and recovery

2. **Trust Tier System**
   - Tier 1: Economic trust (basic operation)
   - Tier 2: TPM 2.0 hardware attestation
   - Tier 3: Confidential compute (AMD SEV-SNP)
   - Tier 4: KYB (Know Your Business) identity verification
   - Route filtering by minimum tier

3. **Market Mechanism**
   - Dynamic epoch-based pricing (supply/demand)
   - Provider min-ask and user max-bid
   - Price locking per session
   - Market state publication

4. **Multi-Payment Rails**
   - Fiat via Stripe (extend existing)
   - TAP (ERC-20 on Ethereum)
   - TNK (Trac native token)
   - Cross-rail isolation (no mixing)
   - Settlement and claiming

5. **Extended API Endpoints**
   - `/v1/embeddings` - embedding generation
   - `/v1/images/generations` - image generation
   - `/v1/videos` - video generation
   - `/v1/audio/speech` - TTS
   - `/v1/audio/transcriptions` - STT/ASR
   - `/v1/audio/generations` - audio generation
   - `/v1/music/generations` - music generation
   - `/v1/workflows` - ComfyUI workflow execution
   - `/v1/responses` - alternate response format
   - `/v1/jobs/<id>` - async job status

6. **Backend Engine Support**
   - llama.cpp integration (current)
   - vLLM support
   - TensorRT-LLM support
   - MLX for Apple Silicon
   - ComfyUI workflow backend

7. **Advanced Provider Features**
   - Per-provider concurrency limits
   - Daily budget caps
   - Accept-rate limits
   - Multi-model packing
   - Parts inventory management

8. **Wallet & Identity**
   - Local wallet creation/backup/import
   - Mnemonic seed management
   - Ethereum/Tron address derivation
   - Payout destination binding

9. **User Features**
   - Bearer token management with budgets
   - Rate limiting per token
   - Model allowlists
   - Dispute opening and resolution
   - Session history tracking

10. **Dashboard & UI**
    - User dashboard
    - Provider dashboard
    - Network explorer dashboard

11. **Documentation & Compliance**
    - Error code system with categories
    - Receipt signing and verification
    - Audit trail and evidence

# Non-goals

- Do NOT copy OpenMayhem's Rust code directly
- Do NOT implement as a wrapper/proxy to OpenMayhem
- Do NOT implement P2P networking until trust tier system is designed
- Do NOT implement confidential compute attestation in first phase
- Do NOT implement KYB verification flow (requires external integration)

# Acceptance examples

## Core Infrastructure
- **A1**: P2P networking layer establishes encrypted sessions between nodes
- **A2**: Node discovery returns list of available providers

## Trust & Security
- **A3**: TPM 2.0 attestation produces valid quote for Tier 2 routes
- **A4**: Route filtering by minimum attestation tier works correctly
- **A5**: Signed receipts verify correctly against provider keys

## Market & Pricing
- **A6**: Dynamic pricing adjusts based on utilization each epoch
- **A7**: Session price locks at open and never reprices mid-session
- **A8**: Provider min-ask controls market participation

## Payments
- **A9**: Stripe fiat payment credits balance correctly
- **A10**: TAP deposit flows through Ethereum and credits balance
- **A11**: TNK deposit settles through Trac Network
- **A12**: Cross-rail isolation prevents payment mixing

## Extended APIs
- **A13**: `/v1/embeddings` returns valid embedding vectors
- **A14**: `/v1/images/generations` initiates and completes image generation
- **A15**: `/v1/audio/transcriptions` converts speech to text
- **A16**: `/v1/workflows` executes ComfyUI workflow graph

## Provider Management
- **A17**: Provider sets concurrency/daily budget limits
- **A18**: Accept-rate limits enforced correctly
- **A19**: Multi-model packing fits models into memory budget

## Wallet & Identity
- **A20**: Wallet mnemonic backup and restore works
- **A21**: Payout destination binding verified on-chain

## User Management
- **A22**: Bearer tokens with per-token budgets enforce spending limits
- **A23**: Dispute opens, resolves, or auto-expires correctly

## Dashboards
- **A24**: User dashboard displays balance, usage, receipts
- **A25**: Provider dashboard shows earnings, routes, capacity

# Constraints and invariants

1. **Language**: Implementation must be in Go (existing codebase)
2. **Backward compatibility**: Existing API endpoints must continue working
3. **No breaking changes**: Current gRPC interface must remain compatible
4. **Modular architecture**: New features must be independently testable
5. **Configuration**: All new features must be configurable without code changes
6. **Error handling**: All error codes must match OpenMayhem specification
7. **Documentation**: New endpoints must have OpenAPI specs

# Decisions

1. **Architecture approach**: Implement features as native Go packages, not as wrappers to OpenMayhem
2. **P2P networking**: Use libp2p Go implementation for peer-to-peer communication
3. **Trust tiers**: TPM attestation via Go-TPM interface; confidential compute as future phase
4. **Market mechanism**: Implement epoch-based state machine with 1-hour duration (matches OpenMayhem)
5. **Payment rails**: Abstract payment interface to support multiple rails; wallet system independent from API Key auth
6. **ComfyUI workflows**: Full implementation with all node types, LoRA, ControlNet support
7. **Wallet integration**: Independent wallet system separate from API Key authentication

# Open questions

All questions resolved:
- ✅ Q1: Use libp2p Go implementation (compatible but native Go)
- ✅ Q2: 1-hour epoch duration (matches OpenMayhem default)
- ✅ Q3: Full ComfyUI workflow implementation
- ✅ Q4: Independent wallet system, separate from API Key auth
- ✅ CONFIRM: Proceed with full OpenMayhem feature parity (except Rust code reuse)

# Verification expectations

1. **Unit tests**: Every new package requires >80% test coverage
2. **Integration tests**: P2P, payment, and trust tier flows require integration tests
3. **API compliance**: Extended endpoints must pass OpenAI compatibility tests
4. **Error code validation**: All error codes must match specification
5. **Performance benchmarks**: P2P latency must be under 100ms for local network
6. **Security review**: Trust tier and payment flows require security review

# Implementation Phases

## Phase 1: Foundation (P2P + Trust Tiers)
- P2P networking layer with libp2p
- Node discovery and session establishment
- Basic trust tier infrastructure
- TPM 2.0 attestation interface

## Phase 2: Market Mechanism
- Epoch-based pricing state machine
- Provider min-ask / user max-bid system
- Price locking per session
- Market state publication

## Phase 3: Multi-Payment Rails
- TAP (Ethereum) integration
- TNK (Trac) integration
- Cross-rail isolation
- Settlement and claiming

## Phase 4: Extended APIs
- Embeddings endpoint
- Image generation endpoint
- Audio transcription/synthesis endpoints
- Async job handling

## Phase 5: Provider Management
- Advanced limit controls
- Multi-model packing
- Parts inventory
- Reputation enhancement

## Phase 6: Workflows & Dashboards
- ComfyUI workflow executor
- User dashboard
- Provider dashboard
- Network explorer

## Phase 7: Identity & Disputes
- Wallet management
- Payout binding
- Dispute resolution
- Session history
