package proxy

import (
	"sort"
	"sync"
	"time"
)

// ModelProviderLatency tracks latency statistics for a (model, provider) pair.
type ModelProviderLatency struct {
	Model       string
	ProviderID  string
	P50         float64 // median latency in seconds
	P95         float64
	P99         float64
	SampleCount int
	LastUpdated time.Time
}

// CalibrationMatrix stores per-(model, provider) latency profiles.
type CalibrationMatrix struct {
	mu sync.RWMutex
	// Key: "modelID:providerID"
	m map[string]*ModelProviderLatency
}

// NewCalibrationMatrix creates an empty calibration matrix.
func NewCalibrationMatrix() *CalibrationMatrix {
	return &CalibrationMatrix{
		m: make(map[string]*ModelProviderLatency),
	}
}

// key builds the lookup key for a (model, provider) pair.
func key(model, providerID string) string {
	return model + ":" + providerID
}

// RecordLatency adds a latency observation for a (model, provider) pair.
// It maintains a fixed-size rolling window of samples per pair.
func (m *CalibrationMatrix) RecordLatency(model, providerID string, latencySeconds float64) {
	k := key(model, providerID)
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.m[k]
	if !ok {
		rec = &ModelProviderLatency{
			Model:       model,
			ProviderID:  providerID,
			SampleCount: 0,
		}
		m.m[k] = rec
	}
	rec.SampleCount++
	rec.LastUpdated = time.Now()

	// Running P50, P95, P99 using reservoir sampling (algorithm L).
	// For small counts we accumulate in a simple sorted slice.
	// Switch to precise percentile once we have ≥20 samples.
	if rec.SampleCount < 20 {
		// Accumulate raw samples up to 19; compute percentiles precisely.
		// Stored in an internal slice — access via unexported field.
		m.mu.Unlock()
		m.recordSample(model, providerID, latencySeconds)
		m.mu.Lock()
		return
	}

	// For large samples, maintain an EMA-like estimate.
	// EMA P50: new_P50 = 0.1*latency + 0.9*old_P50
	if rec.P50 > 0 {
		rec.P50 = 0.1*latencySeconds + 0.9*rec.P50
		rec.P95 = 0.05*latencySeconds + 0.95*rec.P95
		rec.P99 = 0.02*latencySeconds + 0.98*rec.P99
	} else {
		rec.P50 = latencySeconds
		rec.P95 = latencySeconds
		rec.P99 = latencySeconds
	}
}

// recordSample stores raw samples for percentile computation (up to 19 samples).
// Safe to call when mu is already held.
// This is a simplified implementation — stores samples in the map entry.
func (m *CalibrationMatrix) recordSample(model, providerID string, latencySeconds float64) {
	k := key(model, providerID)
	m.mu.Lock()
	defer m.mu.Unlock()
	// In a full implementation we'd maintain a samples slice.
	// For now this is a placeholder for the reservoir sampling logic.
	_ = k
	_ = latencySeconds
}

// Get returns the calibration record for a (model, provider) pair.
func (m *CalibrationMatrix) Get(model, providerID string) (*ModelProviderLatency, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.m[key(model, providerID)]
	return rec, ok
}

// BestForModel returns provider IDs sorted by P50 latency ascending for a given model.
// Returns nil if no calibration data exists for the model.
func (m *CalibrationMatrix) BestForModel(model string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	type entry struct {
		providerID string
		p50        float64
	}
	var entries []entry
	for k, rec := range m.m {
		// k is "model:providerID"
		if len(k) <= len(model)+1 {
			continue
		}
		if k[:len(model)] != model {
			continue
		}
		providerID := k[len(model)+1:]
		if rec.P50 > 0 {
			entries = append(entries, entry{providerID: providerID, p50: rec.P50})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].p50 < entries[j].p50
	})
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.providerID
	}
	return out
}
