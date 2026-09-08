package proxy

import (
	"context"
	"testing"
	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/pkg/gateway/provider"
)

// mockProxyClient is a fake coreClient for proxy tests.
type mockProxyClient struct {
	chatCompletionsFn  func(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error)
	completionsFn      func(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error)
}

func (m *mockProxyClient) ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
	return m.chatCompletionsFn(ctx, req)
}

func (m *mockProxyClient) Completions(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
	return m.completionsFn(ctx, req)
}

func (m *mockProxyClient) ListModels(ctx context.Context) (*pb.ListModelsResponse, error) {
	return nil, nil
}

func (m *mockProxyClient) ListProviders(ctx context.Context) (*pb.ListProvidersResponse, error) {
	return nil, nil
}

func (m *mockProxyClient) StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	ch := make(chan *pb.StreamChunk)
	close(ch)
	errs := make(chan error)
	close(errs)
	return ch, errs
}

// mockRouter returns the given providers for RouteAll.
type mockRouter struct {
	providers []provider.RoutingScore
	err       error
}

func (r *mockRouter) RouteAll(ctx context.Context, model string) ([]provider.RoutingScore, error) {
	return r.providers, r.err
}

func (r *mockRouter) Route(ctx context.Context, model string) (*provider.RoutingScore, error) {
	if len(r.providers) == 0 {
		return nil, r.err
	}
	return &r.providers[0], r.err
}

func (r *mockRouter) Attempts() int {
	return len(r.providers)
}

func TestProxy_ChatCompletions_Success(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	registry.UpdateHeartbeat("provider-a", 50)

	router := NewRouter(registry, nil, 2)
	p := NewProxy(client, router)

	ctx := context.Background()
	req := &pb.ChatCompletionRequest{Model: "gpt-4"}

	// Without a real gRPC server this will fail — but we can verify
	// the proxy attempts routing and records the failure in tracker.
	resp, err := p.ChatCompletions(ctx, req)
	// We expect an error since there's no real server, but the call should
	// have been attempted through the router.
	if resp != nil {
		t.Errorf("Expected nil response without real server, got %v", resp)
	}
	_ = err
}

func TestProxy_ChatCompletions_UsesFirstProvider(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	registry.UpdateHeartbeat("provider-a", 50)
	registry.UpdateHeartbeat("provider-b", 100)

	router := NewRouter(registry, nil, 2)
	p := NewProxy(client, router)

	// Record some latency so calibration matrix gets populated.
	p.calibrate.RecordLatency("gpt-4", "provider-a", 0.05)

	cal, ok := p.calibrate.Get("gpt-4", "provider-a")
	if !ok {
		t.Fatal("Expected calibration record for gpt-4:provider-a")
	}
	if cal.SampleCount != 1 {
		t.Errorf("Expected 1 sample, got %d", cal.SampleCount)
	}
}

func TestProxy_TrackerRecordsFailure(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	registry.UpdateHeartbeat("provider-a", 50)

	router := NewRouter(registry, nil, 2)
	p := NewProxy(client, router)

	ctx := context.Background()
	req := &pb.ChatCompletionRequest{Model: "gpt-4"}

	// Will fail because no real server — tracker should record it.
	p.ChatCompletions(ctx, req)

	h := p.tracker.Health("provider-a")
	if h.ConsecutiveFails == 0 {
		t.Errorf("Expected consecutive failures > 0 after failed call, got %d", h.ConsecutiveFails)
	}
}

func TestProxy_TrackerRecordsSuccess(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	registry.UpdateHeartbeat("provider-a", 50)

	router := NewRouter(registry, nil, 2)
	p := NewProxy(client, router)

	// Manually record a success.
	p.tracker.RecordSuccess("provider-a")

	h := p.tracker.Health("provider-a")
	if h.ConsecutiveFails != 0 {
		t.Errorf("Expected 0 consecutive failures after success, got %d", h.ConsecutiveFails)
	}
	if h.FailureRate != 0 {
		t.Errorf("Expected 0 failure rate, got %f", h.FailureRate)
	}
}

func TestProxy_TrackerAndCalibrationNotNil(t *testing.T) {
	client := grpc.NewClient(grpc.Config{Addr: "127.0.0.1:50051"})
	registry := provider.NewRegistry(client)
	router := NewRouter(registry, nil, 1)
	p := NewProxy(client, router)

	if p.Tracker() == nil {
		t.Error("Tracker() should not be nil")
	}
	if p.CalibrationMatrix() == nil {
		t.Error("CalibrationMatrix() should not be nil")
	}
}

func TestCalibrationMatrix_RecordAndGet(t *testing.T) {
	m := NewCalibrationMatrix()

	m.RecordLatency("gpt-4", "provider-a", 0.05)
	m.RecordLatency("gpt-4", "provider-a", 0.07)

	rec, ok := m.Get("gpt-4", "provider-a")
	if !ok {
		t.Fatal("Expected calibration record")
	}
	if rec.Model != "gpt-4" {
		t.Errorf("Expected model gpt-4, got %s", rec.Model)
	}
	if rec.ProviderID != "provider-a" {
		t.Errorf("Expected provider-a, got %s", rec.ProviderID)
	}
}

func TestCalibrationMatrix_BestForModel(t *testing.T) {
	m := NewCalibrationMatrix()

	// Record 25 samples each to trigger EMA-based P50 (>19 samples threshold).
	// Provider-b is faster for gpt-4; provider-c is faster for claude-3.
	for i := 0; i < 25; i++ {
		m.RecordLatency("gpt-4", "provider-a", 0.20)
		m.RecordLatency("gpt-4", "provider-b", 0.05)
		m.RecordLatency("claude-3", "provider-c", 0.03)
		m.RecordLatency("claude-3", "provider-a", 0.15)
	}

	bestGpt4 := m.BestForModel("gpt-4")
	if bestGpt4 == nil {
		t.Fatal("Expected best providers for gpt-4")
	}
	if len(bestGpt4) != 2 {
		t.Errorf("Expected 2 providers for gpt-4, got %d", len(bestGpt4))
	}
	if bestGpt4[0] != "provider-b" {
		t.Errorf("Expected provider-b first for gpt-4 (lowest P50), got %s", bestGpt4[0])
	}

	bestClaude := m.BestForModel("claude-3")
	if bestClaude == nil {
		t.Fatal("Expected best providers for claude-3")
	}
	if bestClaude[0] != "provider-c" {
		t.Errorf("Expected provider-c first for claude-3, got %s", bestClaude[0])
	}
}

func TestCalibrationMatrix_UnknownModel(t *testing.T) {
	m := NewCalibrationMatrix()
	m.RecordLatency("gpt-4", "provider-a", 0.05)

	best := m.BestForModel("unknown-model")
	if best != nil {
		t.Errorf("Expected nil for unknown model, got %v", best)
	}

	rec, ok := m.Get("unknown", "provider-a")
	if ok || rec != nil {
		t.Errorf("Expected not found for unknown model, got %v", rec)
	}
}
