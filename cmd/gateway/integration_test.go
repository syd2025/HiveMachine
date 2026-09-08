package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/internal/grpc/pb/pbconnect"
	"github.com/hivemachine/pkg/gateway/api"
	"github.com/hivemachine/pkg/gateway/provider"
	"github.com/hivemachine/pkg/gateway/proxy"
)

// mockRustCore implements MayhemServiceHandler via generated pbconnect code.
type mockRustCore struct {
	pbconnect.UnimplementedMayhemServiceHandler
}

func (m *mockRustCore) ListModels(ctx context.Context, req *connect.Request[pb.ListModelsRequest]) (*connect.Response[pb.ListModelsResponse], error) {
	return connect.NewResponse(&pb.ListModelsResponse{
		Models: []*pb.Model{
			{Id: "mayhem/gpt-4o", Created: 1710000000, OwnedBy: "openmayhem"},
			{Id: "mayhem/llama-3-70b", Created: 1710000001, OwnedBy: "openmayhem"},
		},
	}), nil
}

func (m *mockRustCore) ChatCompletions(ctx context.Context, req *connect.Request[pb.ChatCompletionRequest]) (*connect.Response[pb.ChatCompletionResponse], error) {
	var content string
	for _, msg := range req.Msg.GetMessages() {
		content += msg.GetContent() + " "
	}
	content = strings.TrimSpace(content)
	if content == "" {
		content = "Hello!"
	}
	return connect.NewResponse(&pb.ChatCompletionResponse{
		Id:      "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Created: time.Now().Unix(),
		Model:   req.Msg.GetModel(),
		Choices: []*pb.ChatChoice{
			{
				Index: 0,
				Message: &pb.ChatMessage{
					Role:    "assistant",
					Content: "Mock: " + content,
				},
				FinishReason: "stop",
			},
		},
		Usage: &pb.Usage{PromptTokens: 10, CompletionTokens: 8, TotalTokens: 18},
	}), nil
}

func (m *mockRustCore) Completions(ctx context.Context, req *connect.Request[pb.CompletionRequest]) (*connect.Response[pb.CompletionResponse], error) {
	return connect.NewResponse(&pb.CompletionResponse{
		Id:      "cmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Created: time.Now().Unix(),
		Model:   req.Msg.GetModel(),
		Choices: []*pb.CompletionChoice{
			{
				Index:        0,
				Text:         "Mock completion: " + req.Msg.GetPrompt(),
				FinishReason: "stop",
			},
		},
		Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 6, TotalTokens: 11},
	}), nil
}

func (m *mockRustCore) StreamCompletions(ctx context.Context, req *connect.Request[pb.CompletionRequest], stream *connect.ServerStream[pb.StreamChunk]) error {
	stream.Send(&pb.StreamChunk{Id: "c1", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "Mock "}})
	stream.Send(&pb.StreamChunk{Id: "c2", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "stream "}})
	stream.Send(&pb.StreamChunk{Id: "c3", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: req.Msg.GetPrompt()}, FinishReason: "stop"})
	return nil
}

func (m *mockRustCore) StreamChatCompletions(ctx context.Context, req *connect.Request[pb.ChatCompletionRequest], stream *connect.ServerStream[pb.StreamChunk]) error {
	stream.Send(&pb.StreamChunk{Id: "s1", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "Mock "}})
	stream.Send(&pb.StreamChunk{Id: "s2", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "chat "}})
	stream.Send(&pb.StreamChunk{Id: "s3", Model: req.Msg.GetModel(), Delta: &pb.ChoiceDelta{Content: "stream"}, FinishReason: "stop"})
	return nil
}

func (m *mockRustCore) ListProviders(ctx context.Context, req *connect.Request[pb.ListProvidersRequest]) (*connect.Response[pb.ListProvidersResponse], error) {
	return connect.NewResponse(&pb.ListProvidersResponse{
		Providers: []*pb.Provider{
			{Id: "mock-provider-1", Endpoint: "http://localhost:9999", Status: "healthy", Reputation: 0.95, Models: []string{"gpt-4o"}},
			{Id: "mock-provider-2", Endpoint: "http://localhost:9998", Status: "healthy", Reputation: 0.88, Models: []string{"llama-3-70b"}},
		},
	}), nil
}

// startMockCore starts the mock gRPC server and returns its address + cleanup.
func startMockCore(t *testing.T) (addr string, cleanup func()) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock core listen: %v", err)
	}
	addr = lis.Addr().String()
	mux := http.NewServeMux()
	path, handler := pbconnect.NewMayhemServiceHandler(&mockRustCore{})
	mux.Handle(path, handler)
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener.Close()
	srv.Listener = lis
	srv.Start()
	return addr, srv.Close
}

// startGateway creates a gateway server wired to the given coreAddr, using
// a net/http server directly so we can retrieve the actual listen address.
func startGateway(t *testing.T, coreAddr string) (baseURL string, cleanup func()) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("gateway listen: %v", err)
	}

	grpcClient := grpc.NewClient(grpc.Config{Addr: coreAddr, PoolSize: 1})
	registry := provider.NewRegistry(grpcClient)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	registry.Refresh(ctx)
	cancel()
	router := proxy.NewRouter(registry, nil, 2)
	inferenceProxy := proxy.NewProxy(grpcClient, router)
	srv := api.NewServer(lis.Addr().String(), grpcClient, registry, inferenceProxy, nil)
	srv.SetBalance("test-key", 10000)

	httpSrv := &http.Server{Handler: srv.Router()}
	go httpSrv.Serve(lis)

	return "http://" + lis.Addr().String(), func() {
		httpSrv.Close()
		lis.Close()
	}
}

// TestIntegration_ListModels verifies /v1/models returns models from the mock core.
func TestIntegration_ListModels(t *testing.T) {
	coreAddr, coreCleanup := startMockCore(t)
	defer coreCleanup()

	baseURL, gwCleanup := startGateway(t, coreAddr)
	defer gwCleanup()

	resp, err := http.Get(baseURL + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			Id      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("object = %q, want list", result.Object)
	}
	if len(result.Data) == 0 {
		t.Fatal("no models returned from mock core")
	}
	if result.Data[0].Id == "" {
		t.Error("model id is empty")
	}
}

// TestIntegration_ChatCompletions verifies /v1/chat/completions routes to the mock core.
func TestIntegration_ChatCompletions(t *testing.T) {
	coreAddr, coreCleanup := startMockCore(t)
	defer coreCleanup()

	baseURL, gwCleanup := startGateway(t, coreAddr)
	defer gwCleanup()

	body := `{"model":"mayhem/gpt-4o","messages":[{"role":"user","content":"say hello"}]}`
	req, _ := http.NewRequest("POST", baseURL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Id      string `json:"id"`
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Object != "chat.completion" {
		t.Errorf("object = %q, want chat.completion", result.Object)
	}
	if len(result.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if result.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q, want assistant", result.Choices[0].Message.Role)
	}
	if !strings.Contains(result.Choices[0].Message.Content, "Mock:") {
		t.Errorf("content = %q, want to contain Mock:", result.Choices[0].Message.Content)
	}
	if result.Usage.TotalTokens == 0 {
		t.Error("usage not populated from mock core")
	}
}

// TestIntegration_Completions verifies /v1/completions routes to the mock core.
func TestIntegration_Completions(t *testing.T) {
	coreAddr, coreCleanup := startMockCore(t)
	defer coreCleanup()

	baseURL, gwCleanup := startGateway(t, coreAddr)
	defer gwCleanup()

	body := `{"model":"mayhem/llama-3-70b","prompt":"hello world"}`
	req, _ := http.NewRequest("POST", baseURL+"/v1/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Id      string `json:"id"`
		Object  string `json:"object"`
		Choices []struct {
			Text         string `json:"text"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Object != "text_completion" {
		t.Errorf("object = %q, want text_completion", result.Object)
	}
	if len(result.Choices) == 0 {
		t.Fatal("no choices returned")
	}
	if !strings.Contains(result.Choices[0].Text, "Mock completion:") {
		t.Errorf("text = %q, want to contain Mock completion:", result.Choices[0].Text)
	}
}

// TestIntegration_ListProviders verifies /providers returns providers from the mock core.
func TestIntegration_ListProviders(t *testing.T) {
	coreAddr, coreCleanup := startMockCore(t)
	defer coreCleanup()

	baseURL, gwCleanup := startGateway(t, coreAddr)
	defer gwCleanup()

	req, _ := http.NewRequest("GET", baseURL+"/providers", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1/providers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Providers []struct {
			Id     string `json:"id"`
			Status string `json:"status"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result.Providers) == 0 {
		t.Fatal("no providers returned from mock core")
	}
}
func TestIntegration_Health(t *testing.T) {
	coreAddr, coreCleanup := startMockCore(t)
	defer coreCleanup()

	baseURL, gwCleanup := startGateway(t, coreAddr)
	defer gwCleanup()

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
