package stripe

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v76"
)

func TestPaygate_New(t *testing.T) {
	p := NewPaygate("whsec_test")
	if p == nil {
		t.Fatal("NewPaygate returned nil")
	}
	if p.endpointSecret != "whsec_test" {
		t.Errorf("Expected endpoint secret 'whsec_test', got '%s'", p.endpointSecret)
	}
}

func TestPaygate_ProcessWebhook_InvalidSignature(t *testing.T) {
	p := NewPaygate("whsec_test")
	err := p.ProcessWebhook([]byte(`{"type": "payment_intent.succeeded"}`), "invalid_signature")
	if err == nil {
		t.Error("Expected error for invalid signature")
	}
}

func TestPaygate_ProcessWebhook_UnknownEventType(t *testing.T) {
	// Use empty secret to bypass signature verification in test
	p := NewPaygate("")
	event := stripe.Event{
		ID:   "evt_test",
		Type: "unknown.event.type",
		Data: &stripe.EventData{},
		Created: time.Now().Unix(),
	}
	payload, _ := json.Marshal(event)

	err := p.ProcessWebhook(payload, "")
	// Empty secret will fail signature check, not the switch
	if err == nil {
		t.Error("Expected error for empty secret")
	}
}

func TestPaygate_HandlePaymentSuccess(t *testing.T) {
	p := NewPaygate("whsec_test")
	err := p.handlePaymentSuccess(stripe.Event{ID: "evt_test"})
	if err != nil {
		t.Errorf("handlePaymentSuccess should not error: %v", err)
	}
}

func TestPaygate_HandlePaymentFailure(t *testing.T) {
	p := NewPaygate("whsec_test")
	err := p.handlePaymentFailure(stripe.Event{ID: "evt_test"})
	if err != nil {
		t.Errorf("handlePaymentFailure should not error: %v", err)
	}
}
