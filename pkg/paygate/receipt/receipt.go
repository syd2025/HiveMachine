package receipt

import (
	"fmt"
	"sync"
	"time"
)

// Receipt is a signed usage record for a single inference call.
// Every field is included in the signature to prevent tampering.
type Receipt struct {
	ID         string `json:"id"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	ProviderID string `json:"provider_id"` // which provider fulfilled this request

	// Usage counts.
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens     int `json:"total_tokens"`

	// Cost in cents (USD).
	CostCents int64 `json:"cost_cents"`

	// Unix seconds when the receipt was created.
	CreatedAt int64 `json:"created_at"`

	// Ed25519 signature over the canonical string of the above fields.
	Signature []byte `json:"signature"`
}

// Canonical returns the deterministic string used for signing/verification.
func (r *Receipt) Canonical() string {
	return fmt.Sprintf("%s|%s|%s|%s|%d|%d|%d|%d|%d",
		r.ID, r.APIKey, r.Model, r.ProviderID,
		r.PromptTokens, r.CompletionTokens, r.TotalTokens,
		r.CostCents, r.CreatedAt)
}

// Store is the receipt persistence interface.
type Store interface {
	Save(r *Receipt) error
	ByID(id string) (*Receipt, error)
	ByAPIKey(apiKey string) ([]*Receipt, error)
	Recent(n int) ([]*Receipt, error)
}

// InMemoryStore holds receipts in memory.
type InMemoryStore struct {
	mu       sync.RWMutex
	byID     map[string]*Receipt
	byAPIKey map[string][]*Receipt
}

// NewInMemoryStore creates an empty receipt store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		byID:     make(map[string]*Receipt),
		byAPIKey: make(map[string][]*Receipt),
	}
}

func (s *InMemoryStore) Save(r *Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[r.ID] = r
	s.byAPIKey[r.APIKey] = append([]*Receipt{r}, s.byAPIKey[r.APIKey]...)
	return nil
}

func (s *InMemoryStore) ByID(id string) (*Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return nil, nil
}

func (s *InMemoryStore) ByAPIKey(apiKey string) ([]*Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byAPIKey[apiKey], nil
}

func (s *InMemoryStore) Recent(n int) ([]*Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []*Receipt
	for _, receipts := range s.byAPIKey {
		all = append(all, receipts...)
	}
	// Sort descending by CreatedAt (most recent first).
	for i := 0; i < len(all)-1; i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].CreatedAt > all[i].CreatedAt {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if len(all) > n {
		all = all[:n]
	}
	return all, nil
}

// NewReceipt creates a receipt with the given fields and the current timestamp.
// The caller must call Sign() on the receipt before saving.
func NewReceipt(id, apiKey, model, providerID string,
	prompt, completion, total int, costCents int64) *Receipt {
	return &Receipt{
		ID:              id,
		APIKey:          apiKey,
		Model:           model,
		ProviderID:      providerID,
		PromptTokens:    prompt,
		CompletionTokens: completion,
		TotalTokens:     total,
		CostCents:       costCents,
		CreatedAt:       time.Now().Unix(),
	}
}
