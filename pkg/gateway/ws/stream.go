package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	pb "github.com/hivemachine/internal/grpc/pb"
)

// Upgrader converts HTTP connections to WebSocket.
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// StreamClient is the subset of grpc.Client used for streaming.
type StreamClient interface {
	StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error)
}

// ProxyStreamClient is the proxy's streaming surface.
type ProxyStreamClient interface {
	StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error)
}

// StreamHandler handles WebSocket chat completions at /v1/chat/completions/ws.
type StreamHandler struct {
	coreClient  StreamClient
	proxyClient ProxyStreamClient
}

// NewStreamHandler creates a new WebSocket stream handler.
func NewStreamHandler(core StreamClient, proxy ProxyStreamClient) *StreamHandler {
	return &StreamHandler{coreClient: core, proxyClient: proxy}
}

// WSRequest is the JSON payload sent by the client over WebSocket.
type WSRequest struct {
	Model       string     `json:"model"`
	Messages    []WMessage `json:"messages"`
	Temperature float32    `json:"temperature"`
	MaxTokens  int        `json:"max_tokens"`
}

// WMessage mirrors the OpenAI chat message shape.
type WMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChunkMessage is a streamed chunk sent to the client.
type ChunkMessage struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		Delta        WDelta `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
	Done  bool    `json:"done"`
}

// WDelta is the delta content in a streamed chunk.
type WDelta struct {
	Content string `json:"content"`
}

// Usage reports token usage for a streamed chunk.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

// Handle upgrades an HTTP connection to WebSocket and runs the streaming protocol.
func (h *StreamHandler) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Read the client's request.
	var req WSRequest
	if err := conn.ReadJSON(&req); err != nil {
		h.sendError(conn, "failed to read request: "+err.Error())
		return
	}

	model := req.Model
	if model == "" {
		model = "mayhem/default"
	}
	temperature := req.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	maxTokens := int32(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	pbReq := &pb.ChatCompletionRequest{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	for _, m := range req.Messages {
		pbReq.Messages = append(pbReq.Messages, &pb.ChatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// Prefer proxy streaming if available.
	streamer := h.coreClient
	if h.proxyClient != nil {
		streamer = h.proxyClient
	}

	chunks, errs := streamer.StreamChatCompletions(ctx, pbReq)

	var wg sync.WaitGroup
	wg.Add(2)

	var sendErr error
	var done bool

	// Receive chunks and forward to WebSocket client.
	go func() {
		defer wg.Done()
		created := time.Now().Unix()
		for chunk := range chunks {
			msg := ChunkMessage{
				ID:      "chatcmpl-" + randomID(),
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
			}
			delta := chunk.GetDelta().GetContent()
			finish := chunk.GetFinishReason()

			msg.Choices = []struct {
				Index        int    `json:"index"`
				Delta        WDelta `json:"delta"`
				FinishReason string `json:"finish_reason,omitempty"`
			}{{Index: 0, Delta: WDelta{Content: delta}, FinishReason: finish}}

			if usage := chunk.GetUsage(); usage != nil {
				msg.Usage = &Usage{
					PromptTokens:     int(usage.GetPromptTokens()),
					CompletionTokens: int(usage.GetCompletionTokens()),
					TotalTokens:     int(usage.GetTotalTokens()),
				}
			}

			if finish != "" {
				msg.Done = true
				done = true
			}

			if err := conn.WriteJSON(msg); err != nil {
				sendErr = err
				cancel()
				return
			}
		}
	}()

	// Forward stream errors.
	go func() {
		defer wg.Done()
		for err := range errs {
			if sendErr != nil {
				return
			}
			h.sendError(conn, "stream error: "+err.Error())
			cancel()
			return
		}
	}()

	wg.Wait()

	if !done {
		h.sendDone(conn, model, sendErr)
	}
}

func (h *StreamHandler) sendError(conn *websocket.Conn, msg string) {
	conn.WriteJSON(map[string]interface{}{"error": msg, "done": true})
}

func (h *StreamHandler) sendDone(conn *websocket.Conn, model string, sendErr error) {
	doneMsg := ChunkMessage{
		ID:      "chatcmpl-" + randomID(),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Done:    true,
	}
	finishReason := ""
	if sendErr != nil {
		finishReason = "error"
	}
	doneMsg.Choices = []struct {
		Index        int    `json:"index"`
		Delta        WDelta `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}{{Index: 0, Delta: WDelta{Content: ""}, FinishReason: finishReason}}
	conn.WriteJSON(doneMsg)
}

// randomID returns a short random hex string for chunk IDs.
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
