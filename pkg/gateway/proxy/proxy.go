package proxy

import (
	"context"
	"log"
	"time"

	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/internal/grpc/pb"
)

// Proxy is the inference proxy: it routes requests through the Router,
// tries providers in order, records latency into CalibrationMatrix and
// outcomes into Tracker, and returns the first successful response.
type Proxy struct {
	client     *grpc.Client
	router    *Router
	tracker   *Tracker
	calibrate *CalibrationMatrix
	filter    RouteFilter
}

// NewProxy creates a Proxy.
func NewProxy(client *grpc.Client, router *Router) *Proxy {
	return &Proxy{
		client:     client,
		router:    router,
		tracker:    NewTracker(5 * time.Minute),
		calibrate: NewCalibrationMatrix(),
	}
}

// WithRouteFilter sets the minimum trust tier for routing.
func (p *Proxy) WithRouteFilter(f RouteFilter) *Proxy {
	p.filter = f
	return p
}

// ChatCompletions attempts ChatCompletions on the best available provider,
// falling back to the next if the call fails.
func (p *Proxy) ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error) {
	model := req.GetModel()
	providers, err := p.router.RouteAll(ctx, model, p.filter)
	if err != nil || len(providers) == 0 {
		return nil, err
	}

	var lastErr error
	for i, prov := range providers {
		if i >= p.router.Attempts() {
			break
		}

		start := time.Now()
		resp, err := p.client.ChatCompletions(ctx, req)
		latency := time.Since(start).Seconds()

		if err == nil {
			p.tracker.RecordSuccess(prov.ID)
			p.calibrate.RecordLatency(model, prov.ID, latency)
			return resp, nil
		}

		p.tracker.RecordFailure(prov.ID)
		lastErr = err
		log.Printf("[proxy] provider %s failed for model %s: %v", prov.ID, model, err)
	}

	return nil, lastErr
}

// Completions is like ChatCompletions but for text completions.
func (p *Proxy) Completions(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error) {
	model := req.GetModel()
	providers, err := p.router.RouteAll(ctx, model, p.filter)
	if err != nil || len(providers) == 0 {
		return nil, err
	}

	var lastErr error
	for i, prov := range providers {
		if i >= p.router.Attempts() {
			break
		}

		start := time.Now()
		resp, err := p.client.Completions(ctx, req)
		latency := time.Since(start).Seconds()

		if err == nil {
			p.tracker.RecordSuccess(prov.ID)
			p.calibrate.RecordLatency(model, prov.ID, latency)
			return resp, nil
		}

		p.tracker.RecordFailure(prov.ID)
		lastErr = err
		log.Printf("[proxy] provider %s failed for model %s: %v", prov.ID, model, err)
	}

	return nil, lastErr
}

// StreamChatCompletions streams chat completions through the best available provider.
// It does not retry across providers because the stream is already open.
func (p *Proxy) StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error) {
	model := req.GetModel()
	providers, err := p.router.RouteAll(ctx, model, p.filter)
	if err != nil || len(providers) == 0 {
		ch := make(chan *pb.StreamChunk)
		close(ch)
		errs := make(chan error, 1)
		if err != nil {
			errs <- err
		}
		close(errs)
		return ch, errs
	}

	// Proxy the stream; track outcome asynchronously.
	chunks, errs := p.client.StreamChatCompletions(ctx, req)
	go func() {
		err := <-errs
		if err != nil {
			p.tracker.RecordFailure(providers[0].ID)
		} else {
			p.tracker.RecordSuccess(providers[0].ID)
		}
	}()
	return chunks, errs
}

// Tracker returns the health tracker (read-only access for monitoring).
func (p *Proxy) Tracker() *Tracker {
	return p.tracker
}

// CalibrationMatrix returns the calibration matrix (read-only access for monitoring).
func (p *Proxy) CalibrationMatrix() *CalibrationMatrix {
	return p.calibrate
}
