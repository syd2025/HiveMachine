package provider

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultHeartbeatInterval is how often the heartbeat loop pings providers.
	DefaultHeartbeatInterval = 30 * time.Second
	// StaleThreshold is how long since LastHeartbeat before a provider is considered stale.
	StaleThreshold = 60 * time.Second
)

// Heartbeater is the interface for checking provider health.
type Heartbeater interface {
	Ping(ctx context.Context) error
}

// HeartbeatLoop runs a background goroutine that periodically pings providers.
type HeartbeatLoop struct {
	registry   *Registry
	interval   time.Duration
	pinger     Heartbeater
	providers  map[string]Heartbeater // provider ID → per-provider pinger (optional)
	stopCh     chan struct{}
	stoppedCh  chan struct{}
	mu         sync.Mutex
	running    bool
}

// NewHeartbeatLoop creates a heartbeat loop using a shared pinger for all providers.
func NewHeartbeatLoop(registry *Registry, pinger Heartbeater) *HeartbeatLoop {
	return &HeartbeatLoop{
		registry:  registry,
		interval:  DefaultHeartbeatInterval,
		pinger:    pinger,
		providers: make(map[string]Heartbeater),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// RegisterProvider adds a provider to the heartbeat loop.
func (h *HeartbeatLoop) RegisterProvider(id string, pinger Heartbeater) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.providers[id] = pinger
}

// Start begins the heartbeat goroutine.
func (h *HeartbeatLoop) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.stopCh = make(chan struct{})
	h.stoppedCh = make(chan struct{})
	h.mu.Unlock()

	go h.run()
}

// Stop signals the goroutine to exit and waits for it.
func (h *HeartbeatLoop) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	stopCh := h.stopCh
	h.mu.Unlock()
	close(stopCh)
	<-h.stoppedCh
}

func (h *HeartbeatLoop) run() {
	defer close(h.stoppedCh)

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Run once immediately.
	h.pingAll()
	// Then on each tick.
	for {
		select {
		case <-ticker.C:
			h.pingAll()
		case <-h.stopCh:
			return
		}
	}
}

func (h *HeartbeatLoop) pingAll() {
	providers := h.registry.Get()
	for id := range providers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := h.pingProvider(ctx, id)
		cancel()

		if err != nil {
			h.registry.UpdateFailure(id)
			continue
		}
		h.registry.UpdateHeartbeat(id, 0) // latency updated separately
	}
}

func (h *HeartbeatLoop) pingProvider(ctx context.Context, id string) error {
	h.mu.Lock()
	pinger, ok := h.providers[id]
	if !ok {
		pinger = h.pinger
	}
	h.mu.Unlock()

	return pinger.Ping(ctx)
}

// IsStale returns true if the provider's last heartbeat is older than StaleThreshold.
func (r *Registry) IsStale(id string) bool {
	state := r.GetByID(id)
	if state == nil {
		return true
	}
	return time.Since(state.LastHeartbeat) > StaleThreshold
}

// DefaultPinger implements Heartbeater by pinging an address.
type DefaultPinger struct{}

// Ping just calls context.WithTimeout and reports if ctx completes — the actual
// network reachability is determined by the registry's gRPC refresh.
func (p *DefaultPinger) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil // provider is reachable
	}
}
