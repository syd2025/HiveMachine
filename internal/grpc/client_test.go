package grpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/internal/grpc/pb/pbconnect"
)

// mockServer implements MayhemServiceHandler for testing.
type mockServer struct {
	pbconnect.UnimplementedMayhemServiceHandler
	listModelsFn      func(context.Context, *connect.Request[pb.ListModelsRequest]) (*pb.ListModelsResponse, error)
	listProvidersFn   func(context.Context, *connect.Request[pb.ListProvidersRequest]) (*pb.ListProvidersResponse, error)
	streamChatChunks   []*pb.StreamChunk
}

func (m *mockServer) StreamChatCompletions(ctx context.Context, req *connect.Request[pb.ChatCompletionRequest], stream *connect.ServerStream[pb.StreamChunk]) error {
	chunks := m.streamChatChunks
	if chunks == nil {
		chunks = []*pb.StreamChunk{
			{Id: "chatcmpl-1", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "Hello "}},
			{Id: "chatcmpl-1", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "world"}, FinishReason: "stop"},
		}
	}
	for _, c := range chunks {
		if err := stream.Send(c); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockServer) ListModels(ctx context.Context, req *connect.Request[pb.ListModelsRequest]) (*connect.Response[pb.ListModelsResponse], error) {
	if m.listModelsFn != nil {
		resp, err := m.listModelsFn(ctx, req)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(resp), nil
	}
	// Default: return empty list
	return connect.NewResponse(&pb.ListModelsResponse{Models: []*pb.Model{}}), nil
}

func (m *mockServer) ListProviders(ctx context.Context, req *connect.Request[pb.ListProvidersRequest]) (*connect.Response[pb.ListProvidersResponse], error) {
	resp, err := m.listProvidersFn(ctx, req)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (m *mockServer) ChatCompletions(ctx context.Context, req *connect.Request[pb.ChatCompletionRequest]) (*connect.Response[pb.ChatCompletionResponse], error) {
	return connect.NewResponse(&pb.ChatCompletionResponse{
		Id:      "chatcmpl-test",
		Created: 1234567890,
		Model:   req.Msg.GetModel(),
		Choices: []*pb.ChatChoice{
			{
				Index: 0,
				Message: &pb.ChatMessage{
					Role:    "assistant",
					Content: "Hello from Rust core",
				},
				FinishReason: "stop",
			},
		},
	}), nil
}

func (m *mockServer) Completions(ctx context.Context, req *connect.Request[pb.CompletionRequest]) (*connect.Response[pb.CompletionResponse], error) {
	return connect.NewResponse(&pb.CompletionResponse{
		Id:      "cmpl-test",
		Created: 1234567890,
		Model:   req.Msg.GetModel(),
		Choices: []*pb.CompletionChoice{
			{
				Index:        0,
				Text:         "Hello from Rust core",
				FinishReason: "stop",
			},
		},
	}), nil
}

func (m *mockServer) StreamCompletions(ctx context.Context, req *connect.Request[pb.CompletionRequest], stream *connect.ServerStream[pb.StreamChunk]) error {
	return stream.Send(&pb.StreamChunk{
		Id:    "chunk-1",
		Model: req.Msg.GetModel(),
		Delta: &pb.ChoiceDelta{Content: "Hello "},
	})
}

func TestClient_ListModels(t *testing.T) {
	svc := &mockServer{
		listModelsFn: func(ctx context.Context, req *connect.Request[pb.ListModelsRequest]) (*pb.ListModelsResponse, error) {
			return &pb.ListModelsResponse{
				Models: []*pb.Model{
					{Id: "mayhem/gpt-4o", Created: 1699999999, OwnedBy: "openmayhem"},
					{Id: "mayhem/llama-3", Created: 1700000000, OwnedBy: "openmayhem"},
				},
			}, nil
		},
	}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{
		Addr: srv.Listener.Addr().String(),
	})
	defer func() {
		// no-op close since we don't hold resources
	}()

	resp, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(resp.Models) != 2 {
		t.Errorf("len(resp.Models) = %d, want 2", len(resp.Models))
	}
	if resp.Models[0].Id != "mayhem/gpt-4o" {
		t.Errorf("resp.Models[0].Id = %q, want %q", resp.Models[0].Id, "mayhem/gpt-4o")
	}
}

func TestClient_ChatCompletions(t *testing.T) {
	svc := &mockServer{}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})

	resp, err := client.ChatCompletions(context.Background(), &pb.ChatCompletionRequest{
		Model: "mayhem/gpt-4o",
		Messages: []*pb.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletions() error = %v", err)
	}
	if resp.Choices[0].Message.Content != "Hello from Rust core" {
		t.Errorf("content = %q, want %q", resp.Choices[0].Message.Content, "Hello from Rust core")
	}
	if resp.Model != "mayhem/gpt-4o" {
		t.Errorf("model = %q, want %q", resp.Model, "mayhem/gpt-4o")
	}
}

func TestClient_ListProviders(t *testing.T) {
	svc := &mockServer{
		listProvidersFn: func(ctx context.Context, req *connect.Request[pb.ListProvidersRequest]) (*pb.ListProvidersResponse, error) {
			return &pb.ListProvidersResponse{
				Providers: []*pb.Provider{
					{Id: "provider-1", Endpoint: "http://localhost:8080", Status: "healthy", Reputation: 0.95, Models: []string{"gpt-4o"}},
				},
			}, nil
		},
	}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})

	resp, err := client.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Errorf("len(providers) = %d, want 1", len(resp.Providers))
	}
	if resp.Providers[0].Status != "healthy" {
		t.Errorf("status = %q, want %q", resp.Providers[0].Status, "healthy")
	}
}

func TestClient_Ping(t *testing.T) {
	svc := &mockServer{}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping() error = %v, want nil", err)
	}
}

func TestClient_Ping_Unavailable(t *testing.T) {
	client := NewClient(Config{Addr: "127.0.0.1:59999", Timeout: 100 * time.Millisecond})
	if err := client.Ping(context.Background()); err == nil {
		t.Error("Ping() want error for unavailable server, got nil")
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}
	cfg.Defaults()
	if cfg.Addr != "127.0.0.1:50051" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:50051")
	}
	if cfg.PoolSize != 4 {
		t.Errorf("PoolSize = %d, want 4", cfg.PoolSize)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.MaxRetries != 0 {
		t.Errorf("MaxRetries = %d, want 0", cfg.MaxRetries)
	}
}

func TestClient_RetryOnUnavailable(t *testing.T) {
	// Server that returns unavailable twice then succeeds
	var attempts int
	svc := &mockServer{
		listModelsFn: func(ctx context.Context, req *connect.Request[pb.ListModelsRequest]) (*pb.ListModelsResponse, error) {
			attempts++
			if attempts < 3 {
				return nil, connect.NewError(connect.CodeUnavailable, nil)
			}
			return &pb.ListModelsResponse{Models: []*pb.Model{{Id: "test"}}}, nil
		},
	}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String(), MaxRetries: 3})

	resp, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error after retries = %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if resp.Models[0].Id != "test" {
		t.Errorf("resp.Models[0].Id = %q, want %q", resp.Models[0].Id, "test")
	}
}
func TestClient_StreamCompletions(t *testing.T) {
	svc := &mockServer{}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})
	chunks, errs := client.StreamCompletions(context.Background(), &pb.CompletionRequest{Model: "test-model"})
	var gotChunks []*pb.StreamChunk
	var gotErr error
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				chunks = nil
				goto done
			}
			gotChunks = append(gotChunks, c)
		case e, ok := <-errs:
			if !ok {
				errs = nil
				goto done
			}
			gotErr = e
			goto done
		}
	}
done:
	if len(gotChunks) == 0 && gotErr == nil {
		t.Error("expected at least one chunk or error")
	}
	if len(gotChunks) > 0 && gotChunks[0].GetDelta().GetContent() != "Hello " {
		t.Errorf("content = %q, want %q", gotChunks[0].GetDelta().GetContent(), "Hello ")
	}
}

func TestClient_StreamChatCompletions(t *testing.T) {
	svc := &mockServer{}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})
	chunks, errs := client.StreamChatCompletions(context.Background(), &pb.ChatCompletionRequest{Model: "test-model"})
	var gotChunks []*pb.StreamChunk
	var gotErr error
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				chunks = nil
				goto done
			}
			gotChunks = append(gotChunks, c)
		case e, ok := <-errs:
			if !ok {
				errs = nil
				goto done
			}
			gotErr = e
			goto done
		}
	}
done:
	if len(gotChunks) == 0 && gotErr == nil {
		t.Error("expected at least one chunk or error")
	}
	if len(gotChunks) < 2 {
		t.Errorf("got %d chunks, want at least 2", len(gotChunks))
	}
}

func TestClient_Completions(t *testing.T) {
	svc := &mockServer{}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String()})
	resp, err := client.Completions(context.Background(), &pb.CompletionRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Completions() error = %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if resp.Choices[0].Text != "Hello from Rust core" {
		t.Errorf("text = %q, want %q", resp.Choices[0].Text, "Hello from Rust core")
	}
}

func TestClient_ListProviders_RetrySucceeds(t *testing.T) {
	var attempts int
	svc := &mockServer{
		listProvidersFn: func(ctx context.Context, req *connect.Request[pb.ListProvidersRequest]) (*pb.ListProvidersResponse, error) {
			attempts++
			if attempts < 2 {
				return nil, connect.NewError(connect.CodeUnavailable, nil)
			}
			return &pb.ListProvidersResponse{Providers: []*pb.Provider{{Id: "test"}}}, nil
		},
	}
	path, httpHandler := pbconnect.NewMayhemServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, httpHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewClient(Config{Addr: srv.Listener.Addr().String(), MaxRetries: 2})
	resp, err := client.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error after retry = %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
	if resp.Providers[0].Id != "test" {
		t.Errorf("provider id = %q, want %q", resp.Providers[0].Id, "test")
	}
}
