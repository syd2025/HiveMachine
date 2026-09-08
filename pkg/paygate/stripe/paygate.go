package stripe

import (
	"fmt"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
)

// Paygate processes Stripe payments
type Paygate struct {
	endpointSecret string
}

// NewPaygate creates a new Stripe paygate
func NewPaygate(endpointSecret string) *Paygate {
	return &Paygate{endpointSecret: endpointSecret}
}

// ProcessWebhook handles Stripe webhook events
func (p *Paygate) ProcessWebhook(payload []byte, sig string) error {
	event, err := webhook.ConstructEvent(payload, sig, p.endpointSecret)
	if err != nil {
		return fmt.Errorf("webhook verification failed: %w", err)
	}

	switch event.Type {
	case "payment_intent.succeeded":
		return p.handlePaymentSuccess(event)
	case "payment_intent.payment_failed":
		return p.handlePaymentFailure(event)
	default:
		// Ignore unhandled events
	}
	return nil
}

func (p *Paygate) handlePaymentSuccess(event stripe.Event) error {
	// TODO: Update user balance in database
	fmt.Printf("Payment succeeded: %s\n", event.ID)
	return nil
}

func (p *Paygate) handlePaymentFailure(event stripe.Event) error {
	// TODO: Handle failed payment
	fmt.Printf("Payment failed: %s\n", event.ID)
	return nil
}
