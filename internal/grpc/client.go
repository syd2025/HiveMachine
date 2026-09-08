// Package grpc provides a connect-go client to the Rust core.
package grpc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"

	"github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/internal/grpc/pb/pbconnect"
)

// ErrCoreUnavailable is returned when the Rust core cannot be reached.
var ErrCoreUnavailable = errors.New("rust core unavailable")

// Config tunes client behaviour.
type Config struct {
	Addr       string        // e.g. "127.0.0.1:50051"
	PoolSize   int           // number of concurrent connections (default 4)
	Timeout    time.Duration // per-RPC timeout (default 30s)
	MaxRetries int           // max retry attempts on transient failure (default 2)
}

// Defaults sets zero-value fields to sensible defaults.
func (c *Config) Defaults() {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:50051"
	}
	if c.PoolSize <= 0 {
		c.PoolSize = 4
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
}

// Client wraps a pool of connect clients to the Rust core.
type Client struct {
	cfg  Config
	pool []pbconnect.MayhemServiceClient
	idx  uint64
}

// NewClient returns a connection-pooled client.
func NewClient(cfg Config) *Client {
	cfg.Defaults()
	pool := make([]pbconnect.MayhemServiceClient, cfg.PoolSize)
	httpClient := &http.Client{Timeout: cfg.Timeout}
	baseURL := "http://" + cfg.Addr
	for i := range pool {
		pool[i] = pbconnect.NewMayhemServiceClient(httpClient, baseURL)
	}
	return &Client{cfg: cfg, pool: pool}
}

// pick returns the next client from the pool (round-robin).
func (c *Client) pick() pbconnect.MayhemServiceClient {
	idx := atomic.AddUint64(&c.idx, 1)
	return c.pool[idx%uint64(len(c.pool))]
}

// ListModels calls the Rust core.
func (c *Client) ListModels(ctx context.Context) (*pb.ListModelsResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff(ctx, attempt)
		}
		resp, err := c.pick().ListModels(ctx, connect.NewRequest(&pb.ListModelsRequest{}))
		if err == nil {
			return resp.Msg, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// ChatCompletions calls the Rust core (unary).
func (c *Client) ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff(ctx, attempt)
		}
		resp, err := c.pick().ChatCompletions(ctx, connect.NewRequest(req))
		if err == nil {
			return resp.Msg, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// Completions calls the Rust core (unary).
func (c *Client) Completions(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff(ctx, attempt)
		}
		resp, err := c.pick().Completions(ctx, connect.NewRequest(req))
		if err == nil {
			return resp.Msg, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// StreamCompletions streams completion chunks from the Rust core.
// The caller must fully consume the returned channel before the context is cancelled.
func (c *Client) StreamCompletions(ctx context.Context, req *pb.CompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	chunks := make(chan *pb.StreamChunk, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		stream, err := c.pick().StreamCompletions(ctx, connect.NewRequest(req))
		if err != nil {
			errs <- err
			return
		}
		for stream.Receive() {
			chunks <- stream.Msg()
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			errs <- err
		}
	}()
	return chunks, errs
}

// StreamChatCompletions streams chat completion chunks from the Rust core.
func (c *Client) StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	chunks := make(chan *pb.StreamChunk, 64)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		stream, err := c.pick().StreamChatCompletions(ctx, connect.NewRequest(req))
		if err != nil {
			errs <- err
			return
		}
		for stream.Receive() {
			chunks <- stream.Msg()
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			errs <- err
		}
	}()
	return chunks, errs
}

// ListProviders calls the Rust core.
func (c *Client) ListProviders(ctx context.Context) (*pb.ListProvidersResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff(ctx, attempt)
		}
		resp, err := c.pick().ListProviders(ctx, connect.NewRequest(&pb.ListProvidersRequest{}))
		if err == nil {
			return resp.Msg, nil
		}
		if !isRetryable(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// Ping checks connectivity to the Rust core. Returns nil if reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListModels(ctx)
	return err
}

// isRetryable returns true for transient connect/grpc errors.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var connErr *connect.Error
	if errors.As(err, &connErr) {
		switch connErr.Code() {
		case connect.CodeUnavailable, connect.CodeInternal, connect.CodeDeadlineExceeded:
			return true
		}
	}
	return false
}

// backoff sleeps with exponential jitter before retrying.
func backoff(ctx context.Context, attempt int) {
	base := 100 * time.Millisecond
	sleep := base * time.Duration(1<<(attempt-1))
	if sleep > 2*time.Second {
		sleep = 2 * time.Second
	}
	jitter := time.Duration(float64(sleep) * 0.25 * (float64(attempt%2)*2-1))
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < sleep+jitter {
			return
		}
	}
	select {
	case <-time.After(sleep + jitter):
	case <-ctx.Done():
	}
}
