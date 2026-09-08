package proxy

import (
	"testing"
	"time"
)

func TestTracker_RecordSuccess(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	tr.RecordSuccess("provider-a")

	h := tr.Health("provider-a")
	if h.ConsecutiveFails != 0 {
		t.Errorf("Expected 0 consecutive failures after success, got %d", h.ConsecutiveFails)
	}
	if h.FailureRate != 0 {
		t.Errorf("Expected 0 failure rate after success, got %f", h.FailureRate)
	}
}

func TestTracker_RecordFailure(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	tr.RecordFailure("provider-a")

	h := tr.Health("provider-a")
	if h.ConsecutiveFails != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", h.ConsecutiveFails)
	}
	if h.FailureRate != 1.0 {
		t.Errorf("Expected 1.0 failure rate after 1 failure / 1 total, got %f", h.FailureRate)
	}
}

func TestTracker_ConsecutiveFailuresResetOnSuccess(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	tr.RecordFailure("provider-a")
	tr.RecordFailure("provider-a")
	tr.RecordSuccess("provider-a")

	if tr.ConsecutiveFailures("provider-a") != 0 {
		t.Errorf("Expected 0 consecutive failures after success, got %d", tr.ConsecutiveFailures("provider-a"))
	}

	h := tr.Health("provider-a")
	if h.FailureRate != 0 {
		t.Errorf("Expected 0 failure rate after success, got %f", h.FailureRate)
	}
}

func TestTracker_IsExcluded(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	// 2 failures with 3 prior successes = 40% failure rate — not excluded.
	for i := 0; i < 3; i++ {
		tr.RecordSuccess("provider-a")
	}
	tr.RecordFailure("provider-a")
	tr.RecordFailure("provider-a")
	if tr.IsExcluded("provider-a") {
		t.Errorf("Expected not excluded after 2 failures with prior successes (40%% rate)")
	}

	// 3rd consecutive failure: 3 fails / 6 total = 50% — excluded at ≥50%.
	tr.RecordFailure("provider-a")
	if !tr.IsExcluded("provider-a") {
		t.Errorf("Expected excluded after 3 failures / 6 total (50%% rate)")
	}
}

func TestTracker_FailureRateExclusion(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	// 4 successes, 2 failures = 33% failure rate (not excluded).
	for i := 0; i < 4; i++ {
		tr.RecordSuccess("provider-a")
	}
	for i := 0; i < 2; i++ {
		tr.RecordFailure("provider-a")
	}

	h := tr.Health("provider-a")
	if h.FailureRate != 2.0/6.0 {
		t.Errorf("Expected failure rate 2/6, got %f", h.FailureRate)
	}
	if tr.IsExcluded("provider-a") {
		t.Errorf("Expected not excluded at 33%% failure rate")
	}
}

func TestTracker_Health_UnknownProvider(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	h := tr.Health("unknown")
	if h.ProviderID != "unknown" {
		t.Errorf("Expected providerID 'unknown', got %s", h.ProviderID)
	}
	if h.ConsecutiveFails != 0 {
		t.Errorf("Expected 0 failures for unknown provider, got %d", h.ConsecutiveFails)
	}
}

func TestTracker_RecordFailureReturnValue(t *testing.T) {
	tr := NewTracker(1 * time.Minute)

	// 1 failure with 0 prior = 100% rate = excluded.
	h := tr.RecordFailure("provider-a")
	if h.ConsecutiveFails != 1 {
		t.Errorf("Expected 1 consecutive fail in return value, got %d", h.ConsecutiveFails)
	}
	if !h.IsExcluded {
		t.Errorf("Expected excluded at 100%% failure rate (1/1)")
	}
}
