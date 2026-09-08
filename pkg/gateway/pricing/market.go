package pricing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// MarketState represents the published state of the market at a given epoch.
type MarketState struct {
	Epoch        uint64           // Epoch number = unix_seconds / 3600
	PublishedAt  time.Time        // Publication timestamp
	ModelPrices  map[string]int64 // model → price in cents per 1M tokens
	ProviderCaps map[string]int64 // providerID → available capacity (requests/epoch)
	TotalDemand  int64            // Total inference requests this epoch
	TotalCapacity int64           // Total available capacity this epoch
	Signature    []byte           // HMAC-SHA256 signature over the state
}

// MarketStateStore persists market states.
type MarketStateStore interface {
	Latest() (*MarketState, error)
	Save(s *MarketState) error
	ByEpoch(epoch uint64) (*MarketState, error)
}

// InMemoryMarketStateStore holds market states in memory.
type InMemoryMarketStateStore struct {
	mu    sync.RWMutex
	states map[uint64]*MarketState
}

// NewInMemoryMarketStateStore creates an empty in-memory store.
func NewInMemoryMarketStateStore() *InMemoryMarketStateStore {
	return &InMemoryMarketStateStore{states: make(map[uint64]*MarketState)}
}

// Latest returns the most recent market state.
func (s *InMemoryMarketStateStore) Latest() (*MarketState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *MarketState
	for _, st := range s.states {
		if latest == nil || st.Epoch > latest.Epoch {
			latest = st
		}
	}
	return latest, nil
}

// Save stores a market state.
func (s *InMemoryMarketStateStore) Save(state *MarketState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.Epoch] = state
	return nil
}

// ByEpoch returns the market state for a specific epoch.
func (s *InMemoryMarketStateStore) ByEpoch(epoch uint64) (*MarketState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state, ok := s.states[epoch]; ok {
		return state, nil
	}
	return nil, nil
}

// Epoch returns the epoch number for a given time.
func EpochFor(t time.Time) uint64 {
	return uint64(t.Unix()) / 3600
}

// Market manages the epoch-based market pricing state machine.
type Market struct {
	store       MarketStateStore
	priceStore  *InMemoryStore
	signKey     []byte

	mu              sync.RWMutex
	currentState    *MarketState
	lockedPrices    map[string]int64 // sessionID → locked price (cents per 1M tokens)
	epochStats      map[uint64]*epochStats // epoch → stats

	// Config
	priceElasticity  float64 // premium multiplier sensitivity (default 0.5)
	discountFactor   float64 // discount factor when underutilized (default 0.2)
	epochInterval    time.Duration // should be 1 hour

	// Callbacks
	onPublish func(*MarketState) // called after state is published
}

// epochStats accumulates demand/capacity during an epoch.
type epochStats struct {
	mu          sync.Mutex
	totalReqs   int64
	totalCap    int64
	modelReqs   map[string]int64
	modelPrices map[string]int64 // provider-set prices for this epoch
}

func newEpochStats() *epochStats {
	return &epochStats{
		modelReqs:   make(map[string]int64),
		modelPrices: make(map[string]int64),
	}
}

// AddRequest records an inference request for the current epoch.
func (e *epochStats) AddRequest(model string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.totalReqs++
	e.modelReqs[model]++
}

// AddCapacity adds provider capacity for a model.
func (e *epochStats) AddCapacity(model string, cap int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.totalCap += cap
}

// MarketConfig holds market configuration.
type MarketConfig struct {
	PriceElasticity float64 // premium multiplier when over capacity (default 0.5)
	DiscountFactor  float64 // discount when under capacity (default 0.2)
	EpochInterval   time.Duration // tick interval (default 1 hour)
	SignKey         []byte // secret key for HMAC signatures
}

// DefaultMarketConfig returns sensible defaults.
func DefaultMarketConfig() MarketConfig {
	return MarketConfig{
		PriceElasticity: 0.5,
		DiscountFactor:  0.2,
		EpochInterval:   time.Hour,
		SignKey:         nil, // nil = no signatures (development mode)
	}
}

// NewMarket creates a market with the given configuration.
func NewMarket(cfg MarketConfig, store MarketStateStore, priceStore *InMemoryStore) *Market {
	if cfg.PriceElasticity <= 0 {
		cfg.PriceElasticity = 0.5
	}
	if cfg.DiscountFactor <= 0 {
		cfg.DiscountFactor = 0.2
	}
	if cfg.EpochInterval == 0 {
		cfg.EpochInterval = time.Hour
	}
	m := &Market{
		store:          store,
		priceStore:     priceStore,
		signKey:        cfg.SignKey,
		lockedPrices:   make(map[string]int64),
		epochStats:     make(map[uint64]*epochStats),
		priceElasticity: cfg.PriceElasticity,
		discountFactor:  cfg.DiscountFactor,
		epochInterval:   cfg.EpochInterval,
	}
	// Bootstrap: compute initial state for current epoch.
	m.computeAndPublish(time.Now())
	return m
}

// LockPrice locks the current market price for a new session.
// Returns the locked price in cents per 1M tokens.
func (m *Market) LockPrice(sessionID, model string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	epoch := EpochFor(time.Now())
	stats := m.epochStats[epoch]
	if stats == nil {
		stats = newEpochStats()
		m.epochStats[epoch] = stats
	}

	// Compute market price for this model.
	price := m.computeModelPriceLocked(model, stats)

	m.lockedPrices[sessionID] = price
	return price, nil
}

// computeModelPriceLocked computes the market price for a model.
// Must be called with m.mu held.
func (m *Market) computeModelPriceLocked(model string, stats *epochStats) int64 {
	// Get base price from store.
	basePrice, ok := m.priceStore.Get(model)
	if !ok {
		// Fallback to mayhem/default.
		basePrice, _ = m.priceStore.Get("mayhem/default")
	}
	baseCPM := int64(math.Round((basePrice.PromptPricePer1K + basePrice.CompletionPricePer1K) * 100_00)) // cents per 1M tokens

	modelReqs := int64(0)
	if stats != nil {
		stats.mu.Lock()
		modelReqs = stats.modelReqs[model]
		stats.mu.Unlock()
	}

	// If no demand data, return base price.
	if modelReqs == 0 {
		return baseCPM
	}

	// Compute utilization.
	// capacityEstimate = modelReqs * 1.5 as a proxy when no explicit capacity data.
	capacity := modelReqs * 3 / 2 // rough capacity estimate
	if capacity == 0 {
		capacity = 1
	}

	utilization := float64(modelReqs) / float64(capacity)

	var multiplier float64
	if utilization > 1.0 {
		multiplier = 1.0 + (utilization-1.0)*m.priceElasticity
	} else {
		multiplier = 1.0 - (1.0-utilization)*m.discountFactor
	}

	// Clamp: [0.5x, 3.0x] of base price.
	if multiplier < 0.5 {
		multiplier = 0.5
	}
	if multiplier > 3.0 {
		multiplier = 3.0
	}

	price := int64(math.Round(float64(baseCPM) * multiplier))
	return price
}

// PriceFor returns the market price for a model.
// If a session is active (price locked), returns the locked price.
func (m *Market) PriceFor(model, sessionID string) int64 {
	m.mu.RLock()
	if price, ok := m.lockedPrices[sessionID]; ok {
		m.mu.RUnlock()
		return price
	}
	m.mu.RUnlock()

	// Return current market price for this epoch.
	epoch := EpochFor(time.Now())
	stats := m.epochStats[epoch]
	return m.computeModelPriceLocked(model, stats)
}

// ReleaseSession removes the locked price for a session.
func (m *Market) ReleaseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lockedPrices, sessionID)
}

// RecordRequest records an inference request for the current epoch.
// Used for demand tracking.
func (m *Market) RecordRequest(model string) {
	epoch := EpochFor(time.Now())
	m.mu.Lock()
	stats := m.epochStats[epoch]
	if stats == nil {
		stats = newEpochStats()
		m.epochStats[epoch] = stats
	}
	m.mu.Unlock()

	stats.AddRequest(model)
}

// computeAndPublish computes and publishes market state for the given time.
func (m *Market) computeAndPublish(t time.Time) {
	epoch := EpochFor(t)

	m.mu.Lock()
	stats := m.epochStats[epoch]
	if stats == nil {
		stats = newEpochStats()
		m.epochStats[epoch] = stats
	}

	// Compute prices for all known models.
	modelPrices := make(map[string]int64)
	for model := range m.priceStore.prices {
		modelPrices[model] = m.computeModelPriceLocked(model, stats)
	}

	state := &MarketState{
		Epoch:        epoch,
		PublishedAt:  t,
		ModelPrices:  modelPrices,
		ProviderCaps: make(map[string]int64),
		TotalDemand:  func() int64 {
			stats.mu.Lock()
			defer stats.mu.Unlock()
			return stats.totalReqs
		}(),
		TotalCapacity: func() int64 {
			stats.mu.Lock()
			defer stats.mu.Unlock()
			return stats.totalCap
		}(),
	}

	// Sign the state if a sign key is configured.
	if m.signKey != nil {
		state.Signature = m.sign(state)
	}

	m.currentState = state
	if m.store != nil {
		_ = m.store.Save(state)
	}

	onPublish := m.onPublish
	m.mu.Unlock()

	if onPublish != nil {
		onPublish(state)
	}
}

// sign produces an HMAC-SHA256 signature over the market state.
func (m *Market) sign(state *MarketState) []byte {
	h := hmac.New(sha256.New, m.signKey)
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], state.Epoch)
	binary.BigEndian.PutUint64(buf[8:], uint64(state.PublishedAt.Unix()))
	h.Write(buf[:])
	for model, price := range state.ModelPrices {
		h.Write([]byte(model))
		binary.Write(h, binary.BigEndian, price)
	}
	return h.Sum(nil)
}

// LatestState returns the most recently published market state.
func (m *Market) LatestState() *MarketState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentState
}

// Start begins the epoch tick goroutine.
// It recomputes and publishes market state at each epoch boundary.
func (m *Market) Start() {
	go m.ticker()
}

func (m *Market) ticker() {
	// Compute time until next epoch boundary.
	now := time.Now()
	epochSeconds := uint64(now.Unix()) / 3600 * 3600
	nextEpoch := time.Unix(int64(epochSeconds+3600), 0)
	if nextEpoch.Before(now) {
		nextEpoch = nextEpoch.Add(time.Hour)
	}

	ticker := time.NewTicker(m.epochInterval)
	defer ticker.Stop()

	// Wait until next epoch boundary, then tick.
	select {
	case <-time.After(time.Until(nextEpoch)):
		m.computeAndPublish(time.Now())
	case <-ticker.C:
		m.computeAndPublish(time.Now())
	}

	for range ticker.C {
		m.computeAndPublish(time.Now())
	}
}

// OnPublish sets a callback called after each market state publication.
func (m *Market) OnPublish(fn func(*MarketState)) {
	m.onPublish = fn
}

// AddProviderCapacity records a provider's capacity for market state.
func (m *Market) AddProviderCapacity(providerID, model string, capacity int64) {
	epoch := EpochFor(time.Now())
	m.mu.Lock()
	stats := m.epochStats[epoch]
	if stats == nil {
		stats = newEpochStats()
		m.epochStats[epoch] = stats
	}
	m.mu.Unlock()
	stats.AddCapacity(model, capacity)
}
