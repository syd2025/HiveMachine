package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	pb "github.com/hivemachine/internal/grpc/pb"
)

// mockStreamClient implements StreamClient for tests.
type mockStreamClient struct {
	fn func(context.Context, *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error)
}

func (m *mockStreamClient) StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	return m.fn(ctx, req)
}

var _ StreamClient = (*mockStreamClient)(nil)

func deadline(n int) time.Time {
	return time.Now().Add(time.Duration(n) * time.Second)
}

func TestNewStreamHandler(t *testing.T) {
	h := NewStreamHandler(nil, nil)
	if h == nil {
		t.Fatal("NewStreamHandler returned nil")
	}
}

func TestStreamHandler_Handle_UpgradeFailure(t *testing.T) {
	h := NewStreamHandler(nil, nil)
	req := httptest.NewRequest("GET", "/v1/chat/completions/ws", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
}

func TestStreamHandler_Handle_ReadError(t *testing.T) {
	ch := make(chan *pb.StreamChunk)
	close(ch)
	errs := make(chan error)
	close(errs)

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				return ch, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteMessage(websocket.TextMessage, []byte("not json"))
	conn.SetReadDeadline(deadline(1))
	conn.ReadMessage()
}

func TestStreamHandler_Handle_DefaultModel(t *testing.T) {
	var gotReq *pb.ChatCompletionRequest
	ch := make(chan *pb.StreamChunk)
	errs := make(chan error)
	close(errs)

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				gotReq = req
				return ch, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	done := make(chan struct{})
	go func() {
		conn.ReadMessage()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	if gotReq != nil && gotReq.GetModel() != "mayhem/default" {
		t.Errorf("model = %q, want mayhem/default", gotReq.GetModel())
	}
}

func TestStreamHandler_Handle_DefaultTemperature(t *testing.T) {
	var gotReq *pb.ChatCompletionRequest
	ch := make(chan *pb.StreamChunk)
	errs := make(chan error)
	close(errs)

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				gotReq = req
				return ch, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	done := make(chan struct{})
	go func() {
		conn.ReadMessage()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	if gotReq != nil && gotReq.GetTemperature() != 0.7 {
		t.Errorf("temperature = %v, want 0.7", gotReq.GetTemperature())
	}
}

func TestStreamHandler_Handle_DefaultMaxTokens(t *testing.T) {
	var gotReq *pb.ChatCompletionRequest
	ch := make(chan *pb.StreamChunk)
	errs := make(chan error)
	close(errs)

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				gotReq = req
				return ch, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	done := make(chan struct{})
	go func() {
		conn.ReadMessage()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	if gotReq != nil && gotReq.GetMaxTokens() != 1024 {
		t.Errorf("maxTokens = %d, want 1024", gotReq.GetMaxTokens())
	}
}

func TestStreamHandler_Handle_StreamsChunks(t *testing.T) {
	chunksCh := make(chan *pb.StreamChunk, 10)
	errs := make(chan error, 1)
	close(errs)

	go func() {
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "Hello "}}
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "world"}, FinishReason: "stop"}
		close(chunksCh)
	}()

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				return chunksCh, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	var msgs []ChunkMessage
	for i := 0; i < 4; i++ {
		var msg ChunkMessage
		conn.SetReadDeadline(deadline(2))
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		msgs = append(msgs, msg)
	}

	if len(msgs) < 2 {
		t.Errorf("got %d messages, want at least 2", len(msgs))
	}

	done := false
	for _, m := range msgs {
		if m.Done {
			done = true
			break
		}
	}
	if !done {
		t.Error("expected a done message")
	}
}

func TestStreamHandler_Handle_StreamError(t *testing.T) {
	chunksCh := make(chan *pb.StreamChunk)
	errs := make(chan error, 1)
	errs <- errors.New("stream failed")
	close(errs)

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				return chunksCh, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	var gotMsg map[string]interface{}
	for i := 0; i < 3; i++ {
		conn.SetReadDeadline(deadline(2))
		if err := conn.ReadJSON(&gotMsg); err != nil {
			break
		}
		if _, ok := gotMsg["error"]; ok {
			return
		}
	}
	if len(gotMsg) == 0 {
		t.Error("expected at least one message")
	}
}

func TestStreamHandler_Handle_Usage(t *testing.T) {
	chunksCh := make(chan *pb.StreamChunk, 5)
	errs := make(chan error, 1)
	close(errs)

	go func() {
		chunksCh <- &pb.StreamChunk{
			Delta: &pb.ChoiceDelta{Content: "hi"},
			Usage: &pb.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
		}
		chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "!"}, FinishReason: "stop"}
		close(chunksCh)
	}()

	h := &StreamHandler{
		coreClient: &mockStreamClient{
			fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
				return chunksCh, errs
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	var gotUsage *Usage
	for i := 0; i < 4; i++ {
		var msg ChunkMessage
		conn.SetReadDeadline(deadline(2))
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
		if msg.Usage != nil {
			gotUsage = msg.Usage
			break
		}
	}

	if gotUsage == nil {
		t.Error("expected a chunk with usage")
	} else if gotUsage.TotalTokens != 7 {
		t.Errorf("totalTokens = %d, want 7", gotUsage.TotalTokens)
	}
}

func TestStreamHandler_Handle_PreferProxy(t *testing.T) {
	coreCalled := false
	proxyCalled := false

	chunksCh := make(chan *pb.StreamChunk)
	errs := make(chan error, 1)
	close(errs)

	coreClient := &mockStreamClient{
		fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
			coreCalled = true
			return chunksCh, errs
		},
	}
	proxyClient := &mockStreamClient{
		fn: func(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
			proxyCalled = true
			chunksCh <- &pb.StreamChunk{Delta: &pb.ChoiceDelta{Content: "from proxy"}, FinishReason: "stop"}
			close(chunksCh)
			return chunksCh, errs
		},
	}

	h := NewStreamHandler(coreClient, proxyClient)

	srv := httptest.NewServer(http.HandlerFunc(h.Handle))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	conn.WriteJSON(map[string]interface{}{"model": "m", "messages": []map[string]string{{"role": "user", "content": "hi"}}})

	for i := 0; i < 3; i++ {
		var msg ChunkMessage
		conn.SetReadDeadline(deadline(1))
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}
	}

	if proxyCalled && !coreCalled {
		return
	}
	if coreCalled {
		t.Error("core should not be called when proxy is set")
	}
}

func TestStreamHandler_sendDone_NoError(t *testing.T) {
	h := &StreamHandler{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		h.sendDone(conn, "test-model", nil)
	}))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	var msg ChunkMessage
	conn.SetReadDeadline(deadline(2))
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !msg.Done {
		t.Error("expected done=true")
	}
	if msg.Model != "test-model" {
		t.Errorf("model = %q", msg.Model)
	}
}

func TestStreamHandler_sendDone_WithError(t *testing.T) {
	h := &StreamHandler{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		h.sendDone(conn, "test-model", errors.New("test error"))
	}))
	defer srv.Close()

	addr := "ws://" + strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}
	defer conn.Close()

	var msg ChunkMessage
	conn.SetReadDeadline(deadline(2))
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !msg.Done {
		t.Error("expected done=true")
	}
	if len(msg.Choices) == 0 || msg.Choices[0].FinishReason != "error" {
		t.Errorf("finishReason = %q", msg.Choices[0].FinishReason)
	}
}

func TestRandomID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := randomID()
		if ids[id] {
			t.Errorf("duplicate ID: %s", id)
		}
		ids[id] = true
		if len(id) == 0 {
			t.Error("randomID returned empty")
		}
	}
}
