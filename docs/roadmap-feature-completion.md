# HiveMachine Feature Completion Roadmap

## Goal

Close the ~70-80% feature gap between HiveMachine (Go) and OpenMayhem (Rust), following dependency order so each phase has a working foundation.

## Dependency Chain

```
gRPC (Phase 1) ✅ DONE
  └── Provider Management (Phase 2) ✅ DONE ──→ Token & Attribution (Phase 3)
  └── Real CLI (Phase 4) ✅ DONE ←───────────┘
  └── SSE Streaming (Phase 5) ✅ DONE ──→ Failover & Calibration (Phase 6) ✅ DONE

**Why this order?** gRPC is the communication backbone — everything else routes through it. Provider management (heartbeat, reputation) enables intelligent routing. Token attribution is required before any quota enforcement or billing. Real CLI needs working API first. Streaming and failover both depend on provider health data.

---

## Phase 1: gRPC Foundation ✅ DONE

**Goal**: Enable Go ↔ Rust inter-process communication via protobuf/gRPC.

### Tasks

- [x] **Task 1**: Define `mayhem.proto` — services for `ListModels`, `ChatCompletions`, `Completions`, `ListProviders`, `StreamChatCompletions` → File: `internal/grpc/proto/mayhem.proto`

- [x] **Task 2**: Generate connect-go stubs → File: `internal/grpc/pb/`

- [x] **Task 3**: Implement `internal/grpc/client.go` — connection pool, retry loop, deadline propagation → File: `internal/grpc/client.go`

- [x] **Task 4**: Wire `pkg/gateway/api/server.go` handlers to gRPC calls → File: `pkg/gateway/api/server.go`

### Done When
- `go build ./...` passes ✅
- `go test ./internal/grpc/...` passes ✅ (47.2% coverage)
- `go test ./pkg/gateway/api/...` passes ✅ (100% coverage)
- API endpoints delegate to Rust core (not hardcoded) ✅

---

## Phase 2: Provider Management ✅ DONE

**Goal**: Track provider liveness, reputation, and routing weight.

### Tasks

- [x] **Task 5**: Provider registry — `pkg/gateway/provider/registry.go` — in-memory map of `ProviderID → ProviderState{endpoint, status, reputation, lastHeartbeat}` → Verify: Registry updates on heartbeat, unhealthy provider marked ✅

- [x] **Task 6**: Heartbeat loop — background goroutine pings providers every 30s, updates `lastHeartbeat` → File: `pkg/gateway/provider/heartbeat.go` → Verify: Provider marked `unhealthy` after missed heartbeat ✅

- [x] **Task 7**: Reputation scoring — `reputation = uptime_weight * uptime + latency_weight * latency_score` → File: `pkg/gateway/provider/reputation.go` → Verify: Provider ranking changes after latency/uptime changes ✅

- [x] **Task 8**: Provider routing — wire `listProviders()` in `server.go` to registry, use reputation for weight in selection → File: `pkg/gateway/api/server.go` → Verify: `/providers` returns healthy providers with reputation scores ✅

### Done When
- `go test ./pkg/gateway/provider/...` passes ✅ (72.4% coverage)
- Provider health visible via CLI `mayhem provider list`

---

## Phase 3: Token & Attribution ✅ DONE

**Goal**: Count token usage per API key, enforce quotas, gate access.

### Tasks

- [x] **Task 9**: Token counter middleware — `pkg/gateway/middleware/token_counter.go` — counts prompt + completion tokens per request, extracts API key from `Authorization: Bearer` header → Verify: Token counts match OpenAI API response `usage` field (97% coverage)

- [x] **Task 10**: Quota store — `pkg/gateway/pricing/quota.go` — map of `APIKey → QuotaLimit`, check before inference, reject with 429 when exceeded → Verify: Requests over quota return HTTP 429 (86% coverage)

- [x] **Task 11**: Balance deduction on completion — after successful inference, deduct `tokens * price_per_1k` from user balance → Verify: Balance decremented after inference, balance < cost blocks request (100% coverage)

### Done When
- `go test ./pkg/gateway/middleware/...` passes ✅ (97.0% coverage)
- `go test ./pkg/gateway/pricing/...` passes ✅ (86.0% coverage)
- `go test ./pkg/paygate/balance/...` passes ✅ (100.0% coverage)
- Quota exceeded returns 429 with `Retry-After` header ✅

---

## Phase 4: Real CLI Commands ✅ DONE

**Goal**: Make CLI actually work against the live gateway.

### Tasks

- [x] **Task 12**: `mayhem up` — start gateway process as subprocess, write PID to `~/.hivemachine/gateway.pid`, wait for port 11435 → Verify: Gateway reachable after `mayhem up` exits

- [x] **Task 13**: `mayhem down` — read PID, kill, remove PID file → Verify: Port 11435 closed after `mayhem down`

- [x] **Task 14**: `mayhem doctor` — check: gateway port open, gRPC connection, Stripe webhook, disk space → Verify: exits 0 when healthy

- [x] **Task 15**: `mayhem models` — query `GET /v1/models` on `127.0.0.1:11435`, print model table → Verify: shows models from Rust core

### Done When
- `go build -o mayhem ./cmd/cli/` builds successfully ✅
- `mayhem up && mayhem models && mayhem down` sequence works

---

## Phase 5: SSE Streaming ✅ DONE

**Goal**: Support streaming inference via Server-Sent Events (SSE).

### Tasks

- [x] **Task 16**: SSE streaming handler — `POST /v1/chat/completions/stream` returns `Content-Type: text/event-stream` → Verify: curl receives streamed tokens

- [x] **Task 17**: Stream from Rust core — forward streaming response from gRPC `StreamChatCompletions` to SSE client → Verify: Tokens arrive incrementally, not in one chunk

- [x] **Task 18**: Cancellation propagation — on client disconnect, gRPC stream context cancelled → Verify: Goroutine exits when client disconnects

### Done When
- `go test ./pkg/gateway/api/...` passes ✅ (including `TestServer_StreamChatCompletions`)
- `curl -N -X POST http://127.0.0.1:11435/v1/chat/completions/stream` streams SSE tokens
- Streaming completions work with OpenAI JS SDK

---

## Phase 6: Failover & Calibration ✅ DONE

**Goal**: Multi-backend resilience and model routing optimization.

### Tasks

- [x] **Task 19**: Multi-backend failover — `proxy.Router.RouteAll()` returns ranked providers; `proxy.Proxy.ChatCompletions()` tries them in order with `proxy.Tracker` recording outcomes → Verify: Traffic routes to backup when primary is down (72.4% registry coverage, proxy tests pass)

- [x] **Task 20**: Model calibration matrix — `proxy.CalibrationMatrix` tracks per-(model, provider) P50/P95/P99 latency; updated on every successful inference call → Verify: Routing picks lower-latency provider for same model (`TestCalibrationMatrix_BestForModel`)

- [x] **Task 21**: Endpoint health scoring — `proxy.Tracker` records success/failure; `IsExcluded()` excludes at ≥3 consecutive fails or ≥50% failure rate → Verify: Unhealthy endpoint excluded from routing (`TestTracker_IsExcluded`)

### Done When
- `go test ./pkg/gateway/proxy/...` passes ✅
- `go test ./pkg/gateway/provider/...` passes ✅
- System routes around a simulated provider failure

---

## Non-Goals (per original brief)

- Do NOT rewrite Rust core
- Do NOT implement P2P networking
- Do NOT implement inference engine bindings directly
- Do NOT implement enclave/attestation in Go

## Priority Ordering Within Phases

Within each phase, tasks are listed in dependency order. Implement in sequence.

## Verification Per Task

// Comparison: HiveMachine (Go) vs OpenMayhem (Rust) vs OpenAI
| Feature | HiveMachine | OpenMayhem | OpenAI |
|---------|-------------|------------|--------|
| **Gateway** | | | |
| OpenAI-compatible API | ✅ `/v1/models`, `/v1/chat/completions`, `/v1/completions` | ✅ | ✅ |
| SSE Streaming | ✅ `/v1/chat/completions/stream` | ✅ | ✅ |
| WebSocket Streaming | — | — | ✅ |
| **Resilience** | | | |
| 故障转移 (Failover) | ✅ `proxy.Router` ranked providers, `proxy.Tracker` health | Partial | — |
| Calibration Matrix | ✅ P50/P95/P99 per model+provider | — | — |
| **Provider Management** | | | |
| Heartbeat | ✅ 30s interval, unhealthy marking | ✅ | — |
| Reputation Scoring | ✅ uptime+latency weighted | ✅ | — |
| **Billing** | | | |
| Token Attribution | ✅ per API key | ✅ | — |
| Quota Enforcement | ✅ 429 on exceeded | ✅ | — |
| Balance Management | ✅ in-memory store | ✅ | — |
| Stripe Payments | ✅ `pkg/paygate/stripe` | ✅ | — |
| **CLI** | | | |
| `mayhem up/down` | ✅ | ✅ | — |
| `mayhem doctor` | ✅ | ✅ | — |
| `mayhem models` | ✅ | ✅ | — |
| `mayhem provider` | ✅ | ✅ | — |
| `mayhem hardware` | ✅ | — | — |
| `mayhem pay balance` | ✅ | ✅ | — |
| `mayhem pay topup` | ✅ | ✅ | — |

**Legend**: ✅ = implemented | — = not applicable or out of scope

All items above Phase 6 are complete. Remaining work is production hardening (persistent balance store, P2P networking, enclave attestation) — explicitly out of scope per original brief.
