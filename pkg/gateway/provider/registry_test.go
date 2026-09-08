package provider

import (
	"testing"
	"time"
)

func TestRegistry_FreshIsEmpty(t *testing.T) {
	r := NewRegistry(nil)
	if r.Count() != 0 {
		t.Errorf("fresh registry should be empty, got %d", r.Count())
	}
	if r.Get() != nil {
		t.Error("fresh registry Get() should return nil")
	}
}

func TestRegistry_UpdateHeartbeat_NewProvider(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 45)

	state := r.GetByID("p1")
	if state == nil {
		t.Fatal("provider p1 not found")
	}
	if state.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", state.Status, StatusHealthy)
	}
	if state.latencyMs != 45 {
		t.Errorf("latencyMs = %d, want 45", state.latencyMs)
	}
	if state.consecutiveOk != 1 {
		t.Errorf("consecutiveOk = %d, want 1", state.consecutiveOk)
	}
	if state.LastHeartbeat.IsZero() {
		t.Error("LastHeartbeat should be set")
	}
}

func TestRegistry_UpdateHeartbeat_ExistingProvider(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p1", 20)

	state := r.GetByID("p1")
	if state.consecutiveOk != 2 {
		t.Errorf("consecutiveOk = %d, want 2", state.consecutiveOk)
	}
	if state.totalRequests != 2 {
		t.Errorf("totalRequests = %d, want 2", state.totalRequests)
	}
	if state.latencyMs != 20 {
		t.Errorf("latencyMs = %d, want 20 (latest)", state.latencyMs)
	}
}

func TestRegistry_UpdateFailure_NewProvider(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateFailure("p1")

	state := r.GetByID("p1")
	if state == nil {
		t.Fatal("provider should be created")
	}
	if state.consecutiveOk != 0 {
		t.Errorf("consecutiveOk = %d, want 0", state.consecutiveOk)
	}
	if state.Status != StatusUnhealthy {
		t.Errorf("status = %q, want %q", state.Status, StatusUnhealthy)
	}
}

func TestRegistry_UpdateFailure_ExistingHealthy(t *testing.T) {
	r := NewRegistry(nil)
	// Seed: 3 successful heartbeats → healthy
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p1", 10)

	r.UpdateFailure("p1")

	state := r.GetByID("p1")
	if state.Status != StatusUnhealthy {
		t.Errorf("status = %q, want %q", state.Status, StatusUnhealthy)
	}
	if state.consecutiveOk != 0 {
		t.Errorf("consecutiveOk = %d, want 0", state.consecutiveOk)
	}
}

func TestRegistry_UpdateFailure_ThenSuccess(t *testing.T) {
	r := NewRegistry(nil)

	// Two failures → unhealthy
	r.UpdateFailure("p1")
	r.UpdateFailure("p1")

	state := r.GetByID("p1")
	if state.Status != StatusUnhealthy {
		t.Errorf("status = %q, want %q", state.Status, StatusUnhealthy)
	}

	// Three successes → back to healthy
	r.UpdateHeartbeat("p1", 30)
	r.UpdateHeartbeat("p1", 30)
	r.UpdateHeartbeat("p1", 30)

	state = r.GetByID("p1")
	if state.Status != StatusHealthy {
		t.Errorf("status = %q, want %q", state.Status, StatusHealthy)
	}
	if state.consecutiveOk != 3 {
		t.Errorf("consecutiveOk = %d, want 3", state.consecutiveOk)
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p2", 20)

	snap := r.Get()
	if len(snap) != 2 {
		t.Errorf("len(Get()) = %d, want 2", len(snap))
	}
	if snap["p1"].latencyMs != 10 {
		t.Errorf("p1 latencyMs = %d, want 10", snap["p1"].latencyMs)
	}
}

func TestRegistry_GetByID_NotFound(t *testing.T) {
	r := NewRegistry(nil)
	if r.GetByID("nonexistent") != nil {
		t.Error("GetByID(nonexistent) should return nil")
	}
}

func TestRegistry_SetScores(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 50)

	r.SetLatencyScore("p1", 0.8)
	r.SetUptimeScore("p1", 0.9)

	state := r.GetByID("p1")
	if state.latencyScore != 0.8 {
		t.Errorf("latencyScore = %f, want 0.8", state.latencyScore)
	}
	if state.uptimeScore != 0.9 {
		t.Errorf("uptimeScore = %f, want 0.9", state.uptimeScore)
	}
}

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry(nil)
	if r.Count() != 0 {
		t.Errorf("empty count = %d, want 0", r.Count())
	}
	r.UpdateHeartbeat("p1", 10)
	r.UpdateHeartbeat("p2", 10)
	if r.Count() != 2 {
		t.Errorf("count = %d, want 2", r.Count())
	}
}

func TestRegistry_IsStale_NotFound(t *testing.T) {
	r := NewRegistry(nil)
	if !r.IsStale("nonexistent") {
		t.Error("IsStale(nonexistent) should be true")
	}
}

func TestRegistry_IsStale_Fresh(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)
	if r.IsStale("p1") {
		t.Error("fresh heartbeat should not be stale")
	}
}

func TestRegistry_IsStale_Aged(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 10)

	// Manually age the heartbeat beyond threshold.
	state := r.GetByID("p1")
	state.LastHeartbeat = time.Now().Add(-61 * time.Second)

	if !r.IsStale("p1") {
		t.Error("heartbeat older than 60s should be stale")
	}
}
