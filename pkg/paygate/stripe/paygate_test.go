package stripe

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v76"
)

// mockBalanceStore implements BalanceStore for testing.
type mockBalanceStore struct {
	balances map[string]int64
}

func (m *mockBalanceStore) Get(apiKey string) int64 { return m.balances[apiKey] }
func (m *mockBalanceStore) Add(apiKey string, cents int64) {
	m.balances[apiKey] += cents
}

func TestPaygate_New(t *testing.T) {
	p := NewPaygate("sk_test_xxx", "whsec_test", nil)
	if p == nil {
		t.Fatal("NewPaygate returned nil")
	}
	if p.SecretKey() != "sk_test_xxx" {
		t.Errorf("SecretKey = %q, want %q", p.SecretKey(), "sk_test_xxx")
	}
	if p.EndpointSecret() != "whsec_test" {
		t.Errorf("EndpointSecret = %q, want %q", p.EndpointSecret(), "whsec_test")
	}
}

func TestPaygate_ProcessWebhook_InvalidSignature(t *testing.T) {
	p := NewPaygate("sk_test_xxx", "whsec_test", nil)
	err := p.ProcessWebhook([]byte(`{"type": "payment_intent.succeeded"}`), "invalid_signature")
	if err == nil {
		t.Error("Expected error for invalid signature")
	}
}

func TestPaygate_ProcessWebhook_UnknownEventType(t *testing.T) {
	// Empty secret skips signature verification.
	p := NewPaygate("sk_test_xxx", "", nil)
	err := p.ProcessWebhook([]byte(`{"type": "unknown.event.type"}`), "")
	if err != nil {
		t.Errorf("unknown event type: %v", err)
	}
}

func TestPaygate_ProcessWebhook_CheckoutCompleted_CreditsBalance(t *testing.T) {
	store := &mockBalanceStore{balances: make(map[string]int64)}
	p := NewPaygate("sk_test_xxx", "", store)

	event := stripe.Event{
		ID:   "evt_test",
		Type: "checkout.session.completed",
		Data: &stripe.EventData{
			Raw: []byte(`{
				"metadata": {
					"api_key": "user-key-123",
					"amount_cents": "500"
				},
				"payment_status": "paid"
			}`),
		},
		Created: time.Now().Unix(),
	}
	payload, _ := json.Marshal(event)
	err := p.ProcessWebhook(payload, "")
	if err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}
	if got := store.Get("user-key-123"); got != 500 {
		t.Errorf("balance = %d, want 500", got)
	}
}

func TestPaygate_ProcessWebhook_PaymentIntentSucceeded_CreditsBalance(t *testing.T) {
	store := &mockBalanceStore{balances: make(map[string]int64)}
	p := NewPaygate("sk_test_xxx", "", store)

	event := stripe.Event{
		ID:   "evt_test",
		Type: "payment_intent.succeeded",
		Data: &stripe.EventData{
			Raw: []byte(`{
				"metadata": {
					"api_key": "user-key-456",
					"amount_cents": "1000"
				}
			}`),
		},
		Created: time.Now().Unix(),
	}
	payload, _ := json.Marshal(event)
	err := p.ProcessWebhook(payload, "")
	if err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}
	if got := store.Get("user-key-456"); got != 1000 {
		t.Errorf("balance = %d, want 1000", got)
	}
}

func TestPaygate_HandlePaymentFailure_NoPanic(t *testing.T) {
	p := NewPaygate("sk_test_xxx", "whsec_test", nil)
	err := p.handlePaymentFailure(stripe.Event{ID: "evt_test"})
	if err != nil {
		t.Errorf("handlePaymentFailure should not error: %v", err)
	}
}
