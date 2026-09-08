package dispute

import (
	"errors"
	"sync"
	"time"
)

// DisputeState represents the lifecycle state of a dispute.
type DisputeState string

const (
	DisputeStateOpen        DisputeState = "open"         // Created, awaiting review
	DisputeStateUnderReview DisputeState = "under_review" // Being investigated
	DisputeStateResolved    DisputeState = "resolved"     // Funds returned or credited
	DisputeStateRejected   DisputeState = "rejected"     // Dispute denied
	DisputeStateExpired    DisputeState = "expired"      // Auto-expired after timeout
)

// String returns the string representation of the dispute state.
func (s DisputeState) String() string { return string(s) }

func (s DisputeState) Valid() bool {
	return s == DisputeStateResolved || s == DisputeStateRejected || s == DisputeStateExpired
}

// Dispute represents a billing dispute opened by a user.
type Dispute struct {
	ID          string
	APIKey      string
	ReceiptID   string // Receipt being disputed
	Reason      string // User-provided reason
	State       DisputeState
	AmountCents int64  // Amount disputed in cents
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ResolvedAt  *time.Time // Set when terminal state reached
	Resolution  string     // Resolution note (e.g., "refunded", "rejected: no evidence")
	Evidence    []string   // Evidence file URLs or descriptions
}

// TransitionTo updates the dispute state.
func (d *Dispute) TransitionTo(next DisputeState) error {
	switch d.State {
	case DisputeStateOpen:
		if next != DisputeStateUnderReview && next != DisputeStateExpired {
			return ErrInvalidTransition
		}
	case DisputeStateUnderReview:
		if next != DisputeStateResolved && next != DisputeStateRejected && next != DisputeStateExpired {
			return ErrInvalidTransition
		}
	case DisputeStateResolved, DisputeStateRejected, DisputeStateExpired:
		return ErrInvalidTransition // terminal states
	}
	d.State = next
	d.UpdatedAt = time.Now()
	if next.Valid() {
		now := time.Now()
		d.ResolvedAt = &now
	}
	return nil
}

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("dispute: invalid state transition")

// ErrDisputeNotFound is returned when a dispute ID is not found.
var ErrDisputeNotFound = errors.New("dispute: not found")

// ReasonCode classifies the dispute reason.
type ReasonCode string

const (
	ReasonQuality        ReasonCode = "quality"         // Output was poor or incorrect
	ReasonNeverReceived  ReasonCode = "never_received" // No output received
	ReasonChargedIncorrectly ReasonCode = "charged_incorrectly" // Wrong amount charged
	ReasonUnauthorized   ReasonCode = "unauthorized"    // Unauthorized charge
	ReasonDuplicate      ReasonCode = "duplicate"       // Duplicate charge
	ReasonOther         ReasonCode = "other"           // Other reason
)

// OpenDisputeRequest is the body for POST /v1/disputes.
type OpenDisputeRequest struct {
	ReceiptID string `json:"receipt_id" binding:"required"`
	Reason   string `json:"reason" binding:"required"`
	AmountCents int64 `json:"amount_cents" binding:"required,min=1"`
	Evidence []string `json:"evidence"`
}

// ResolveDisputeRequest is the body for POST /v1/disputes/:id/resolve.
type ResolveDisputeRequest struct {
	Resolution string `json:"resolution" binding:"required"`
	// Action: "refund" | "reject"
	Action string `json:"action" binding:"required,oneof=refund reject"`
}

// DisputeStore is the persistence layer for disputes.
type DisputeStore interface {
	// Open creates a new dispute in the "open" state.
	Open(d *Dispute) error

	// ByID returns a dispute by ID.
	ByID(id string) (*Dispute, error)

	// ByAPIKey returns all disputes for an API key.
	ByAPIKey(apiKey string) ([]*Dispute, error)

	// UpdateState transitions a dispute to a new state.
	UpdateState(id string, state DisputeState) error

	// SetResolution sets the resolution note and transitions to terminal state.
	SetResolution(id string, resolution string, state DisputeState) error

	// ExpireOld opens and transitions disputes older than maxAge to expired.
	ExpireOld(maxAge time.Duration) (int, error)

	// List returns all disputes, optionally filtered by state.
	List(states ...DisputeState) ([]*Dispute, error)
}

// InMemoryDisputeStore holds disputes in process memory.
type InMemoryDisputeStore struct {
	mu       sync.RWMutex
	disputes map[string]*Dispute
}

// NewInMemoryDisputeStore creates an empty in-memory dispute store.
func NewInMemoryDisputeStore() *InMemoryDisputeStore {
	return &InMemoryDisputeStore{disputes: make(map[string]*Dispute)}
}

// Open creates a new dispute.
func (s *InMemoryDisputeStore) Open(d *Dispute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disputes[d.ID] = d
	return nil
}

// ByID returns a dispute by ID.
func (s *InMemoryDisputeStore) ByID(id string) (*Dispute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if d, ok := s.disputes[id]; ok {
		return d, nil
	}
	return nil, ErrDisputeNotFound
}

// ByAPIKey returns all disputes for an API key.
func (s *InMemoryDisputeStore) ByAPIKey(apiKey string) ([]*Dispute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Dispute
	for _, d := range s.disputes {
		if d.APIKey == apiKey {
			out = append(out, d)
		}
	}
	return out, nil
}

// UpdateState transitions a dispute to a new state.
func (s *InMemoryDisputeStore) UpdateState(id string, state DisputeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.disputes[id]
	if !ok {
		return ErrDisputeNotFound
	}
	if err := d.TransitionTo(state); err != nil {
		return err
	}
	s.disputes[id] = d
	return nil
}

// SetResolution sets the resolution and transitions to a terminal state.
func (s *InMemoryDisputeStore) SetResolution(id string, resolution string, state DisputeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.disputes[id]
	if !ok {
		return ErrDisputeNotFound
	}
	if err := d.TransitionTo(state); err != nil {
		return err
	}
	d.Resolution = resolution
	s.disputes[id] = d
	return nil
}

// ExpireOld expires disputes older than maxAge.
func (s *InMemoryDisputeStore) ExpireOld(maxAge time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	count := 0
	for _, d := range s.disputes {
		if d.State == DisputeStateOpen && d.CreatedAt.Before(cutoff) {
			d.TransitionTo(DisputeStateExpired)
			count++
		}
	}
	return count, nil
}

// List returns all disputes, optionally filtered by state.
func (s *InMemoryDisputeStore) List(states ...DisputeState) ([]*Dispute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(states) == 0 {
		out := make([]*Dispute, 0, len(s.disputes))
		for _, d := range s.disputes {
			out = append(out, d)
		}
		return out, nil
	}
	stateSet := make(map[DisputeState]bool)
	for _, st := range states {
		stateSet[st] = true
	}
	var out []*Dispute
	for _, d := range s.disputes {
		if stateSet[d.State] {
			out = append(out, d)
		}
	}
	return out, nil
}

// ExpiryService runs a background goroutine that expires old open disputes.
type ExpiryService struct {
	store   DisputeStore
	maxAge  time.Duration
	interval time.Duration
	stop    chan struct{}
}

// NewExpiryService creates an expiry service.
// Disputes open longer than maxAge are automatically transitioned to expired.
func NewExpiryService(store DisputeStore, maxAge, interval time.Duration) *ExpiryService {
	return &ExpiryService{
		store:   store,
		maxAge:  maxAge,
		interval: interval,
		stop:    make(chan struct{}),
	}
}

// Start begins the expiry goroutine.
func (s *ExpiryService) Start() {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = s.store.ExpireOld(s.maxAge)
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop stops the expiry goroutine.
func (s *ExpiryService) Stop() {
	close(s.stop)
}
