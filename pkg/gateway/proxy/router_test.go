package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/pkg/gateway/provider"
	"github.com/hivemachine/pkg/gateway/trust"
)

func TestRouter_RouteAll(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	ctx := context.Background()

	registry.UpdateHeartbeat("provider-a", 50)
	registry.UpdateHeartbeat("provider-b", 200)

	r := NewRouter(registry, nil, 2)

	scores, err := r.RouteAll(ctx, "gpt-4", RouteFilter{})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(scores))
	}
	ids := make(map[string]bool)
	for _, s := range scores {
		ids[s.ID] = true
	}
	if !ids["provider-a"] || !ids["provider-b"] {
		t.Errorf("Expected both providers, got %v", ids)
	}
}

func TestRouter_Route(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	registry.UpdateHeartbeat("provider-a", 50)

	r := NewRouter(registry, nil, 1)
	ctx := context.Background()

	best, err := r.Route(ctx, "gpt-4", RouteFilter{})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if best == nil {
		t.Fatal("Expected a provider, got nil")
	}
	if best.ID != "provider-a" {
		t.Errorf("Expected provider-a, got %s", best.ID)
	}
}

func TestRouter_EmptyRegistry(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	r := NewRouter(registry, nil, 1)
	ctx := context.Background()

	scores, err := r.RouteAll(ctx, "gpt-4", RouteFilter{})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if scores != nil {
		t.Errorf("Expected nil for empty registry, got %d providers", len(scores))
	}

	best, err := r.Route(ctx, "gpt-4", RouteFilter{})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if best != nil {
		t.Errorf("Expected nil provider for empty registry, got %v", best)
	}
}

func TestRouter_Attempts(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	r := NewRouter(registry, nil, 3)

	if r.Attempts() != 3 {
		t.Errorf("Expected 3 attempts, got %d", r.Attempts())
	}

	r2 := NewRouter(registry, nil, 0)
	if r2.Attempts() != 1 {
		t.Errorf("Expected 1 attempt default, got %d", r2.Attempts())
	}
}

func TestRouter_StaleProviderExcluded(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)

	registry.UpdateHeartbeat("provider-a", 50)
	state := registry.GetByID("provider-a")
	state.LastHeartbeat = time.Now().Add(-2 * time.Hour)

	r := NewRouter(registry, nil, 1)
	ctx := context.Background()

	scores, err := r.RouteAll(ctx, "gpt-4", RouteFilter{})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("Expected 0 providers (stale excluded), got %d", len(scores))
	}
}

func TestRouter_TierFiltering(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	ts := trust.NewInMemoryTrustStore()

	registry.UpdateHeartbeat("tier1-provider", 50)
	registry.UpdateHeartbeat("tier2-provider", 100)

	// tier1-provider is economic tier.
	ts.SetTier("tier1-provider", trust.TierEconomic)
	// tier2-provider is hardware tier.
	ts.EnrollTPM("tier2-provider", &trust.TPMTrust{})

	r := NewRouter(registry, nil, 10).WithTrustStore(ts)
	ctx := context.Background()

	// Request TierEconomic — both should pass.
	scores, err := r.RouteAll(ctx, "gpt-4", RouteFilter{MinTier: trust.TierEconomic})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 2 {
		t.Errorf("Expected 2 providers for TierEconomic, got %d", len(scores))
	}

	// Request TierHardware — only tier2-provider should pass.
	scores, err = r.RouteAll(ctx, "gpt-4", RouteFilter{MinTier: trust.TierHardware})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 1 {
		t.Errorf("Expected 1 provider for TierHardware, got %d", len(scores))
	}
	if scores[0].ID != "tier2-provider" {
		t.Errorf("Expected tier2-provider, got %s", scores[0].ID)
	}

	// Request TierKYB — no provider should pass.
	scores, err = r.RouteAll(ctx, "gpt-4", RouteFilter{MinTier: trust.TierKYB})
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("Expected 0 providers for TierKYB, got %d", len(scores))
	}
}

func TestRouter_SlashedProviderExcluded(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	ts := trust.NewInMemoryTrustStore()

	registry.UpdateHeartbeat("provider-a", 50)
	ts.SetTier("provider-a", trust.TierEconomic)
	ts.SetEconomicTrust("provider-a", &trust.EconomicTrust{StakeCents: 200_00, StakeLocked: true})

	r := NewRouter(registry, nil, 1).WithTrustStore(ts)
	ctx := context.Background()

	// Should pass initially.
	best, err := r.Route(ctx, "gpt-4", RouteFilter{MinTier: trust.TierEconomic})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if best == nil {
		t.Fatal("Expected a provider, got nil")
	}

	// Slash the provider.
	ts.SlashEconomic("provider-a", "downtime violation")

	// Should now be excluded.
	best, err = r.Route(ctx, "gpt-4", RouteFilter{MinTier: trust.TierEconomic})
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if best != nil {
		t.Errorf("Expected nil (slashed provider excluded), got %s", best.ID)
	}
}
