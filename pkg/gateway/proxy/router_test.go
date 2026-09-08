package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/pkg/gateway/provider"
)

func TestRouter_RouteAll(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	ctx := context.Background()

	// Register two providers with distinct reputations so sort order is deterministic.
	registry.UpdateHeartbeat("provider-a", 50)
	registry.UpdateHeartbeat("provider-b", 200)

	r := NewRouter(registry, nil, 2)

	scores, err := r.RouteAll(ctx, "gpt-4")
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(scores))
	}
	// Verify both are present (order depends on composite score tie-breaking).
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

	best, err := r.Route(ctx, "gpt-4")
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

	scores, err := r.RouteAll(ctx, "gpt-4")
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if scores != nil {
		t.Errorf("Expected nil for empty registry, got %d providers", len(scores))
	}

	best, err := r.Route(ctx, "gpt-4")
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

	// Zero attempts should default to 1.
	r2 := NewRouter(registry, nil, 0)
	if r2.Attempts() != 1 {
		t.Errorf("Expected 1 attempt default, got %d", r2.Attempts())
	}
}

func TestRouter_StaleProviderExcluded(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)

	registry.UpdateHeartbeat("provider-a", 50)
	// Simulate stale: set last heartbeat to far in the past.
	state := registry.GetByID("provider-a")
	state.LastHeartbeat = time.Now().Add(-2 * time.Hour)

	r := NewRouter(registry, nil, 1)
	ctx := context.Background()

	scores, err := r.RouteAll(ctx, "gpt-4")
	if err != nil {
		t.Fatalf("RouteAll failed: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("Expected 0 providers (stale excluded), got %d", len(scores))
	}
}
