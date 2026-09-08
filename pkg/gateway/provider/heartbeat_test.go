package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockPinger is a Heartbeater that records calls and returns configurable errors.
type mockPinger struct {
	calls   atomic.Int64
	errResp error
}

func (m *mockPinger) Ping(ctx context.Context) error {
	m.calls.Add(1)
	return m.errResp
}

func TestHeartbeatLoop_StartStop(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)

	mp := &mockPinger{}
	h := NewHeartbeatLoop(r, mp)
	h.interval = 100 * time.Millisecond
	h.Start()

	time.Sleep(350 * time.Millisecond)
	h.Stop()

	calls := mp.calls.Load()
	if calls == 0 {
		t.Error("Ping should have been called at least once")
	}
}

func TestHeartbeatLoop_StopWithoutStart(t *testing.T) {
	r := NewRegistry(nil)
	mp := &mockPinger{}
	h := NewHeartbeatLoop(r, mp)
	h.Stop() // must not panic
}

func TestHeartbeatLoop_MultipleStarts(t *testing.T) {
	r := NewRegistry(nil)
	mp := &mockPinger{}
	h := NewHeartbeatLoop(r, mp)
	h.interval = 100 * time.Millisecond
	h.Start()
	h.Start() // second call should be no-op
	time.Sleep(200 * time.Millisecond)
	h.Stop()
}

func TestHeartbeatLoop_UpdatesFailure(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)

	mp := &mockPinger{errResp: errors.New("unreachable")}
	h := NewHeartbeatLoop(r, mp)
	h.interval = 50 * time.Millisecond
	h.Start()

	time.Sleep(250 * time.Millisecond)
	h.Stop()

	state := r.GetByID("p1")
	if state.Status != StatusUnhealthy {
		t.Errorf("status = %q, want %q after failures", state.Status, StatusUnhealthy)
	}
}

func TestHeartbeatLoop_RegisterProvider(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p2", 10)

	mp1 := &mockPinger{}
	mp2 := &mockPinger{}
	h := NewHeartbeatLoop(r, mp1)
	h.interval = 50 * time.Millisecond
	h.RegisterProvider("p1", mp1)
	h.RegisterProvider("p2", mp2)
	h.Start()

	time.Sleep(200 * time.Millisecond)
	h.Stop()

	if mp1.calls.Load() == 0 || mp2.calls.Load() == 0 {
		t.Error("both registered pingers should have been called")
	}
}

func TestHeartbeatLoop_NoProviders(t *testing.T) {
	r := NewRegistry(nil) // empty
	mp := &mockPinger{}
	h := NewHeartbeatLoop(r, mp)
	h.interval = 50 * time.Millisecond
	h.Start()

	time.Sleep(150 * time.Millisecond)
	h.Stop()

	// Should not panic with empty registry
	if mp.calls.Load() != 0 {
		t.Error("shared pinger should not be called when no providers in registry")
	}
}

func TestIsStale_NotFound(t *testing.T) {
	r := NewRegistry(nil)
	if !r.IsStale("nonexistent") {
		t.Error("unknown provider should be stale")
	}
}

func TestIsStale_FreshHeartbeat(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)
	if r.IsStale("p1") {
		t.Error("fresh heartbeat should not be stale")
	}
}

func TestIsStale_OldHeartbeat(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)

	// Manually age the heartbeat.
	state := r.GetByID("p1")
	state.LastHeartbeat = time.Now().Add(-61 * time.Second)

	if !r.IsStale("p1") {
		t.Error("heartbeat older than 60s should be stale")
	}
}
