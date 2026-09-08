package provider

import (
	"math"
	"testing"
)

func TestComputeReputation_ZeroRequests(t *testing.T) {
	state := &ProviderState{
		ID:            "p1",
		totalRequests: 0,
		healthyReqs:   0,
		latencyMs:     0,
		latencyScore:  0,
	}
	score := ComputeReputation(state, nil)
	if score != 0 {
		t.Errorf("score = %f, want 0", score)
	}
}

func TestComputeReputation_Perfect(t *testing.T) {
	state := &ProviderState{
		ID:            "p1",
		totalRequests: 100,
		healthyReqs:   100,
		latencyMs:     50, // well below p50=100
	}
	score := ComputeReputation(state, nil)
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0", score)
	}
}

func TestComputeReputation_Combination(t *testing.T) {
	// With 80/100 uptime and latency at p50 (score=1.0):
	// score = 0.6*0.8 + 0.4*1.0 = 0.88
	state := &ProviderState{
		ID:            "p1",
		totalRequests: 100,
		healthyReqs:   80,
		latencyMs:     100, // exactly at p50 → latencyScore = 1.0
	}
	score := ComputeReputation(state, nil)
	if score != 0.88 {
		t.Errorf("score = %f, want 0.88", score)
	}
}

func TestComputeLatencyScore_BelowP50(t *testing.T) {
	cfg := &ReputationConfig{LatencyP50: 100, LatencyP95: 2000}
	if s := computeLatencyScore(50, cfg); s != 1.0 {
		t.Errorf("below p50: got %f, want 1.0", s)
	}
}

func TestComputeLatencyScore_AboveP95(t *testing.T) {
	cfg := &ReputationConfig{LatencyP50: 100, LatencyP95: 2000}
	if s := computeLatencyScore(2500, cfg); s != 0.0 {
		t.Errorf("above p95: got %f, want 0.0", s)
	}
}

func TestComputeLatencyScore_Interpolated(t *testing.T) {
	cfg := &ReputationConfig{LatencyP50: 100, LatencyP95: 2000}
	// halfway: offset = 950, range = 1900 → score = 1 - 950/1900 = 0.5
	if s := computeLatencyScore(1050, cfg); math.Abs(s-0.5) > 0.001 {
		t.Errorf("at 1050ms: got %f, want ~0.5", s)
	}
}

func TestComputeUptimeScore_ZeroRequests(t *testing.T) {
	state := &ProviderState{totalRequests: 0, healthyReqs: 0}
	if s := computeUptimeScore(state); s != 0.0 {
		t.Errorf("zero requests: got %f, want 0.0", s)
	}
}

func TestComputeUptimeScore_AllHealthy(t *testing.T) {
	state := &ProviderState{totalRequests: 50, healthyReqs: 50}
	if s := computeUptimeScore(state); s != 1.0 {
		t.Errorf("all healthy: got %f, want 1.0", s)
	}
}

func TestComputeUptimeScore_Partial(t *testing.T) {
	state := &ProviderState{totalRequests: 100, healthyReqs: 75}
	if s := computeUptimeScore(state); s != 0.75 {
		t.Errorf("75/100: got %f, want 0.75", s)
	}
}

func TestRecordLatency(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 50) // seed it

	r.RecordLatency("p1", 100)

	state := r.GetByID("p1")
	if state.latencyScore == 0 {
		t.Error("latencyScore should be updated after RecordLatency")
	}
}

func TestUpdateFromHeartbeat(t *testing.T) {
	r := NewRegistry(nil)
	r.UpdateHeartbeat("p1", 50)

	beforeRep := r.GetByID("p1").Reputation
	r.UpdateFromHeartbeat("p1", 80)
	afterRep := r.GetByID("p1").Reputation

	if afterRep <= beforeRep {
		t.Logf("reputation: before=%f after=%f", beforeRep, afterRep)
	}

	if r.GetByID("p1").consecutiveOk != 2 {
		t.Errorf("consecutiveOk = %d, want 2", r.GetByID("p1").consecutiveOk)
	}
}

func TestRecordLatency_NotFound(t *testing.T) {
	r := NewRegistry(nil)
	r.RecordLatency("nonexistent", 100) // must not panic
}
