# HiveMachine

OpenAI-compatible API gateway with multi-provider failover, token billing, and gRPC backend communication.

## Overview

HiveMachine is a Go-based API gateway that proxies LLM inference requests to a Rust core ([OpenMayhem](https://github.com/syd2025/OpenMayhem)), providing an OpenAI-compatible REST API, streaming via SSE, and built-in billing/quota management.

```
Client → HiveMachine (Go Gateway) → OpenMayhem (Rust Core) → LLM Providers
```

## Features

- **OpenAI-compatible API** — `/v1/models`, `/v1/chat/completions`, `/v1/completions`
- **SSE Streaming** — `/v1/chat/completions/stream` with cancellation propagation
- **Multi-provider failover** — ranked providers with automatic fallback on failure
- **Calibration matrix** — P50/P95/P99 latency tracking per model+provider for smart routing
- **Token attribution** — per-API-key token counting and quota enforcement
- **Balance management** — balance deduction on successful inference, 429 when exceeded
- **Stripe payments** — balance top-up via `paygate`
- **Provider heartbeat** — 30s health checks, unhealthy marking, reputation scoring
- **CLI tools** — `mayhem up/down`, `mayhem doctor`, `mayhem models`, `mayhem provider`, `mayhem hardware`, `mayhem pay`

## Architecture

```
cmd/
  cli/          # mayhem CLI entry point
  gateway/      # API gateway server
  paygate/      # Payment service
  hivemachine/  # Main binary
internal/
  grpc/         # gRPC client → Rust core
  runtime/      # Runtime utilities
pkg/
  agent/        # Agent logic
  cli/          # CLI command implementations
  gateway/
    api/        # HTTP handlers
    middleware/ # Auth, token counting, logging
    pricing/    # Quota and pricing
    provider/   # Provider registry, heartbeat, reputation
    proxy/      # Routing, failover, calibration
    store/      # In-memory data stores
  paygate/      # Stripe integration, balance management
  task/         # Task management
  types/        # Shared Go types
  workflow/     # Workflow orchestration
```

## Quick Start

```bash
# Build
go build -o mayhem ./cmd/cli/

# Start gateway
./mayhem up

# Check health
./mayhem doctor

# List models
./mayhem models

# Stop gateway
./mayhem down
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/models` | List available models |
| `POST` | `/v1/chat/completions` | Chat completion |
| `POST` | `/v1/chat/completions/stream` | Streaming chat completion |
| `POST` | `/v1/completions` | Text completion |

## Configuration

Gateway listens on port `11435` by default. Configure via environment or `~/.hivemachine/`.

## Status

All phases complete — see [docs/roadmap-feature-completion.md](docs/roadmap-feature-completion.md) for full feature matrix.

**In scope**: OpenAI-compatible API, streaming, failover, token billing, CLI tools.  
**Out of scope**: P2P networking, enclave/attestation, direct inference engine bindings.
