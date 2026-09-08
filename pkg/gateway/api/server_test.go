package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/pkg/paygate/receipt"
)

// apiMockClient implements coreClient for tests.
type apiMockClient struct {
	listModelsFn            func(context.Context) (*pb.ListModelsResponse, error)
	listProvidersFn         func(context.Context) (*pb.ListProvidersResponse, error)
	chatCompletionsFn       func(context.Context, *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error)
	completionsFn          func(context.Context, *pb.CompletionRequest) (*pb.CompletionResponse, error)
	streamChatCompletionsFn func(context.Context, *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error)
}

func (m *apiMockClient) ListModels(ctx context.Context) (*pb.ListModelsResponse, error) {
	return m.listModelsFn(ctx)
}

func (m *apiMockClient) ListProviders(ctx context.Context) (*pb.ListProvidersResponse, error) {
	return m.listProvidersFn(ctx)
}

func (m *apiMockClient) ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
	return m.chatCompletionsFn(ctx, req)
}

func (m *apiMockClient) Completions(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
	return m.completionsFn(ctx, req)
}

func (m *apiMockClient) StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	if m.streamChatCompletionsFn != nil {
		return m.streamChatCompletionsFn(ctx, req)
	}
	ch := make(chan *pb.StreamChunk)
	close(ch)
	errs := make(chan error)
	close(errs)
	return ch, errs
}

var _ coreClient = (*apiMockClient)(nil)

func TestServer_Health(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	defer s.Close()

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestServer_ListModels(t *testing.T) {
	mock := &apiMockClient{
		listModelsFn: func(context.Context) (*pb.ListModelsResponse, error) {
			return &pb.ListModelsResponse{
				Models: []*pb.Model{
					{Id: "mayhem/gpt-4o", Created: 1699999999, OwnedBy: "openmayhem"},
					{Id: "mayhem/llama-3", Created: 1700000000, OwnedBy: "openmayhem"},
				},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["object"] != "list" {
		t.Errorf("object = %q, want list", resp["object"])
	}
	data, ok := resp["data"].([]interface{})
	if !ok || len(data) != 2 {
		t.Errorf("got %d models, want 2", len(data))
	}
}

func TestServer_ListModels_BadGateway(t *testing.T) {
	mock := &apiMockClient{
		listModelsFn: func(context.Context) (*pb.ListModelsResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502, got %d", w.Code)
	}
}

func TestServer_ListProviders(t *testing.T) {
	mock := &apiMockClient{
		listProvidersFn: func(context.Context) (*pb.ListProvidersResponse, error) {
			return &pb.ListProvidersResponse{
				Providers: []*pb.Provider{
					{Id: "provider-1", Endpoint: "http://localhost:8080", Status: "healthy", Reputation: 0.95},
				},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/providers", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data, ok := resp["providers"].([]interface{})
	if !ok || len(data) != 1 {
		t.Errorf("got %v, want 1 provider", resp["providers"])
	}
}

func TestServer_ChatCompletions_BadRequest(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{invalid}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestServer_ChatCompletions_ValidRequest(t *testing.T) {
	mock := &apiMockClient{
		chatCompletionsFn: func(_ context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
			return &pb.ChatCompletionResponse{
				Id:      "chatcmpl-test",
				Created: 1234567890,
				Model:   req.GetModel(),
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
				Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	reqBody := ChatCompletionRequest{Model: "mayhem/gpt-4o", Messages: []Message{{Role: "user", Content: "Hello"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp ChatCompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", resp.Object)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Choices[0].Message.Content != "Hello from Rust core" {
		t.Errorf("content = %q, want 'Hello from Rust core'", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestServer_ChatCompletions_UpstreamError(t *testing.T) {
	mock := &apiMockClient{
		chatCompletionsFn: func(_ context.Context, _ *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
			return nil, errors.New("rust core unavailable")
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	reqBody := ChatCompletionRequest{Model: "mayhem/gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502, got %d", w.Code)
	}
}

func TestServer_Completions(t *testing.T) {
	mock := &apiMockClient{
		completionsFn: func(_ context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
			return &pb.CompletionResponse{
				Id:      "cmpl-test",
				Created: 1234567890,
				Model:   req.GetModel(),
				Choices: []*pb.CompletionChoice{
					{Index: 0, Text: "Hello from Rust core", FinishReason: "stop"},
				},
				Usage: &pb.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	reqBody := CompletionRequest{Model: "mayhem/gpt-4o", Prompt: "Say hello"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Object != "text_completion" {
		t.Errorf("object = %q, want text_completion", resp.Object)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Choices[0].Text != "Hello from Rust core" {
		t.Errorf("text = %q, want 'Hello from Rust core'", resp.Choices[0].Text)
	}
}

func TestServer_Completions_BadRequest(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("POST", "/v1/completions", strings.NewReader("{invalid}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestServer_NotFound(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("PATCH", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("Expected non-200, got %d", w.Code)
	}
}

// ─── Balance Store ─────────────────────────────────────────────────────────────

func TestBalanceStore_Get(t *testing.T) {
	bs := newBalanceStore()
	bs.Set("key1", 500)
	if got := bs.Get("key1"); got != 500 {
		t.Errorf("Get(key1) = %d, want 500", got)
	}
	if got := bs.Get("missing"); got != 0 {
		t.Errorf("Get(missing) = %d, want 0", got)
	}
}

func TestBalanceStore_Set(t *testing.T) {
	bs := newBalanceStore()
	bs.Set("key1", 100)
	bs.Set("key1", 200)
	if got := bs.Get("key1"); got != 200 {
		t.Errorf("Set overwrote to %d, want 200", got)
	}
}

func TestBalanceStore_Add(t *testing.T) {
	bs := newBalanceStore()
	bs.Add("key1", 100)
	bs.Add("key1", 50)
	if got := bs.Get("key1"); got != 150 {
		t.Errorf("Add gave %d, want 150", got)
	}
}

func TestBalanceStore_Add_NewKey(t *testing.T) {
	bs := newBalanceStore()
	bs.Add("newkey", 42)
	if got := bs.Get("newkey"); got != 42 {
		t.Errorf("Add new key gave %d, want 42", got)
	}
}

// ─── extractAPIKey ────────────────────────────────────────────────────────────

func TestExtractAPIKey_Bearer(t *testing.T) {
	mock := &apiMockClient{listModelsFn: func(context.Context) (*pb.ListModelsResponse, error) { return &pb.ListModelsResponse{Models: []*pb.Model{}}, nil }}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
}

func TestExtractAPIKey_Missing(t *testing.T) {
	mock := &apiMockClient{listModelsFn: func(context.Context) (*pb.ListModelsResponse, error) { return &pb.ListModelsResponse{Models: []*pb.Model{}}, nil }}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
}

func TestExtractAPIKey_NonBearer(t *testing.T) {
	mock := &apiMockClient{listModelsFn: func(context.Context) (*pb.ListModelsResponse, error) { return &pb.ListModelsResponse{Models: []*pb.Model{}}, nil }}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)
	req, _ := http.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
}

// ─── topUp ─────────────────────────────────────────────────────────────────────

func TestTopUp_Valid(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	body := `{"amount": 100}`
	req, _ := http.NewRequest("POST", "/v1/balance/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["balance"] != float64(100) {
		t.Errorf("balance = %v, want 100", resp["balance"])
	}
}

func TestTopUp_InvalidAmount(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	body := `{"amount": -5}`
	req, _ := http.NewRequest("POST", "/v1/balance/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestTopUp_ZeroAmount(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	body := `{"amount": 0}`
	req, _ := http.NewRequest("POST", "/v1/balance/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestTopUp_InvalidJSON(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	req, _ := http.NewRequest("POST", "/v1/balance/topup", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

// ─── getBalance ────────────────────────────────────────────────────────────────

func TestGetBalance_WithAPIKey(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	s.balanceStore.Set("test-key", 250)

	req, _ := http.NewRequest("GET", "/v1/balance", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["balance"] != float64(250) {
		t.Errorf("balance = %v, want 250", resp["balance"])
	}
}

func TestGetBalance_MissingAPIKey(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/v1/balance", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestGetBalance_UnknownKey(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/v1/balance", nil)
	req.Header.Set("Authorization", "Bearer unknown-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["balance"] != float64(0) {
		t.Errorf("balance = %v, want 0 for unknown key", resp["balance"])
	}
}

// ─── ChatCompletions with balance check ───────────────────────────────────────

func TestChatCompletions_InsufficientBalance(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	s.balanceStore.Set("test-key", 0)

	reqBody := ChatCompletionRequest{Model: "mayhem/gpt-4o", Messages: []Message{{Role: "user", Content: "Hello"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402, got %d", w.Code)
	}
}

func TestChatCompletions_DefaultModel(t *testing.T) {
	called := false
	mock := &apiMockClient{
		chatCompletionsFn: func(_ context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
			called = true
			if req.GetModel() != "mayhem/default" {
				t.Errorf("model = %q, want mayhem/default", req.GetModel())
			}
			return &pb.ChatCompletionResponse{
				Id:      "chatcmpl-test",
				Created: 1234567890,
				Model:   req.GetModel(),
				Choices: []*pb.ChatChoice{{Index: 0, Message: &pb.ChatMessage{Role: "assistant", Content: "hi"}, FinishReason: "stop"}},
				Usage:   &pb.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	reqBody := map[string]interface{}{"messages": []map[string]string{{"role": "user", "content": "hi"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("upstream was not called")
	}
}

// ─── Completions upstream error ───────────────────────────────────────────────

func TestCompletions_UpstreamError(t *testing.T) {
	mock := &apiMockClient{
		completionsFn: func(_ context.Context, _ *pb.CompletionRequest) (*pb.CompletionResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	reqBody := CompletionRequest{Model: "mayhem/gpt-4o", Prompt: "hello"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502, got %d", w.Code)
	}
}

// ─── ListProviders via client ──────────────────────────────────────────────────

func TestListProviders_BadGateway(t *testing.T) {
	mock := &apiMockClient{
		listProvidersFn: func(context.Context) (*pb.ListProvidersResponse, error) {
			return nil, errors.New("client error")
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/providers", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502, got %d", w.Code)
	}
}

func TestListProviders_EmptyList(t *testing.T) {
	mock := &apiMockClient{
		listProvidersFn: func(context.Context) (*pb.ListProvidersResponse, error) {
			return &pb.ListProvidersResponse{Providers: []*pb.Provider{}}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/providers", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["providers"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected 0 providers, got %d", len(data))
	}
}

// ─── streamChatCompletions ────────────────────────────────────────────────────

func TestStreamChatCompletions_SSE(t *testing.T) {
	chunksCh := make(chan *pb.StreamChunk, 5)
	errCh := make(chan error, 1)
	close(errCh) // no errors

	go func() {
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "Hello"}, Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6}}
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: " world"}, Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}}
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "!"}, FinishReason: "stop", Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}}
		close(chunksCh)
	}()

	mock := &apiMockClient{
		streamChatCompletionsFn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
			return chunksCh, errCh
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, nil)
	defer s.Close()

	reqBody := ChatCompletionRequest{Model: "mayhem/gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	go s.router.ServeHTTP(w, req)

	// Wait for SSE response headers.
	time.Sleep(100 * time.Millisecond)
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Expected text/event-stream, got %s", ct)
	}
}

func receiptSignerForTest() *receipt.Signer {
	s := receipt.NewSigner(nil)
	return s
}

func TestListReceipts_Unauthorized(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)

	req, _ := http.NewRequest("GET", "/v1/receipts", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", w.Code)
	}
}

func TestListReceipts_Success(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)
	apiKey := "test-key"

	// Save a receipt manually via store.
	r := receipt.NewReceipt("receipt-001", apiKey, "mayhem/gpt-4o", "test-provider",
		5, 10, 15, 0)
	signer.Sign(r)
	s.receiptStore.Save(r)

	req, _ := http.NewRequest("GET", "/v1/receipts", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["receipts"].([]interface{})
	if len(data) != 1 {
		t.Errorf("got %d receipts, want 1", len(data))
	}
}

func TestVerifyReceipt_NotFound(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)

	body := `{"id": "nonexistent"}`
	req, _ := http.NewRequest("POST", "/v1/receipts/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestVerifyReceipt_InvalidJSON(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)

	req, _ := http.NewRequest("POST", "/v1/receipts/verify", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}
}

func TestVerifyReceipt_Success(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)
	apiKey := "verify-test-key"

	r := receipt.NewReceipt("receipt-verify-001", apiKey, "mayhem/gpt-4o", "provider-x",
		3, 7, 10, 0)
	signer.Sign(r)
	s.receiptStore.Save(r)

	body, _ := json.Marshal(map[string]string{"id": "receipt-verify-001"})
	req, _ := http.NewRequest("POST", "/v1/receipts/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["valid"] != true {
		t.Errorf("valid = %v, want true", resp["valid"])
	}
}

func TestReceiptPublicKey_Success(t *testing.T) {
	signer := receiptSignerForTest()
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, signer)

	req, _ := http.NewRequest("GET", "/v1/receipts/public_key", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["public_key"]; !ok {
		t.Errorf("missing public_key in response")
	}
}

func TestReceiptPublicKey_Unavailable(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)

	req, _ := http.NewRequest("GET", "/v1/receipts/public_key", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

// ─── Chat completions with receipt ────────────────────────────────────────────

func TestChatCompletions_WithReceipt(t *testing.T) {
	signer := receiptSignerForTest()
	mock := &apiMockClient{
		chatCompletionsFn: func(_ context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
			return &pb.ChatCompletionResponse{
				Id:      "chatcmpl-receipt-test",
				Created: 1234567890,
				Model:   req.GetModel(),
				Choices: []*pb.ChatChoice{
					{Index: 0, Message: &pb.ChatMessage{Role: "assistant", Content: "receipt test"}, FinishReason: "stop"},
				},
				Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 8, TotalTokens: 13},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, signer)

	s.balanceStore.Set("receipt-key", 10000)
	reqBody := ChatCompletionRequest{Model: "mayhem/gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}}}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer receipt-key")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["receipt_id"]; !ok {
		t.Errorf("Expected receipt_id in response")
	}
}

// ─── Completions with receipt ─────────────────────────────────────────────────

func TestCompletions_WithReceipt(t *testing.T) {
	signer := receiptSignerForTest()
	mock := &apiMockClient{
		completionsFn: func(_ context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
			return &pb.CompletionResponse{
				Id:      "cmpl-receipt-test",
				Created: 1234567890,
				Model:   req.GetModel(),
				Choices: []*pb.CompletionChoice{
					{Index: 0, Text: "receipt test", FinishReason: "stop"},
				},
				Usage: &pb.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5},
			}, nil
		},
	}
	s := NewServer("127.0.0.1:0", mock, nil, nil, signer)

	s.balanceStore.Set("receipt-key-2", 10000)
	reqBody := CompletionRequest{Model: "mayhem/gpt-4o", Prompt: "hello"}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", "/v1/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer receipt-key-2")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["receipt_id"]; !ok {
		t.Errorf("Expected receipt_id in response")
	}
}

// ─── providerIDFromProxy ──────────────────────────────────────────────────────

func TestProviderIDFromProxy_WithoutProxy(t *testing.T) {
	s := NewServer("127.0.0.1:0", &apiMockClient{}, nil, nil, nil)
	if got := providerIDFromProxy(s); got != "" {
		t.Errorf("providerIDFromProxy = %q, want empty string", got)
	}
}
