package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

// BalanceStore credits and queries user balances.
type BalanceStore interface {
	// Get returns the current balance in cents for an API key.
	Get(apiKey string) int64
	// Add adds cents to the account, creating it if necessary.
	Add(apiKey string, cents int64)
}

// ErrMissingAPIKey is returned when a checkout session has no API key in metadata.
var ErrMissingAPIKey = errors.New("payment metadata missing api_key")

// ErrMissingAmount is returned when a checkout session has no amount.
var ErrMissingAmount = errors.New("payment metadata missing amount_cents")

// Paygate processes Stripe payments and manages checkout sessions.
type Paygate struct {
	secretKey      string
	endpointSecret string
	balanceStore   BalanceStore
	webhookTimeout time.Duration
}

// NewPaygate creates a new Stripe paygate.
// balanceStore may be nil if only checkout (not webhook) is used.
func NewPaygate(secretKey, endpointSecret string, balanceStore BalanceStore) *Paygate {
	return &Paygate{
		secretKey:      secretKey,
		endpointSecret: endpointSecret,
		balanceStore:   balanceStore,
		webhookTimeout: 10 * time.Second,
	}
}

// SecretKey returns the configured Stripe secret key (for use when constructing a new Paygate with a wired balance store).
func (p *Paygate) SecretKey() string { return p.secretKey }

// EndpointSecret returns the configured Stripe webhook endpoint secret.
func (p *Paygate) EndpointSecret() string { return p.endpointSecret }

// CreateCheckoutSessionRequest describes a checkout session creation.
type CreateCheckoutSessionRequest struct {
	APIKey    string
	AmountCents int64 // amount in cents
	Currency  string // ISO 4217 currency code, defaults to "usd"
	SuccessURL string
	CancelURL  string
	// Metadata is extra key-value pairs appended to the Stripe session metadata.
	Metadata map[string]string
}

// CreateCheckoutSessionResponse is the result of session creation.
type CreateCheckoutSessionResponse struct {
	SessionID string
	URL       string
}

// CreateCheckoutSession creates a Stripe Checkout Session and returns its URL.
// The API key and amount are stored in Stripe metadata so the webhook
// can credit the correct account on payment_intent.succeeded.
func (p *Paygate) CreateCheckoutSession(ctx context.Context, req CreateCheckoutSessionRequest) (*CreateCheckoutSessionResponse, error) {
	if req.APIKey == "" {
		return nil, errors.New("api_key is required")
	}
	if req.AmountCents <= 0 {
		return nil, errors.New("amount_cents must be positive")
	}
	if req.Currency == "" {
		req.Currency = "usd"
	}

	stripe.Key = p.secretKey

	currencyLower := req.Currency
	metadata := map[string]string{
		"api_key":       req.APIKey,
		"amount_cents":  fmt.Sprintf("%d", req.AmountCents),
		"currency":      currencyLower,
	}
	for k, v := range req.Metadata {
		metadata[k] = v
	}

	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(currencyLower),
					UnitAmount: stripe.Int64(req.AmountCents),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String("HiveMachine Credits"),
						Description: stripe.String(fmt.Sprintf("%d credits", req.AmountCents)),
					},
				},
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: metadata,
		SuccessURL: stripe.String(req.SuccessURL),
		CancelURL:  stripe.String(req.CancelURL),
	}

	sess, err := session.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe checkout session create: %w", err)
	}
	return &CreateCheckoutSessionResponse{SessionID: sess.ID, URL: sess.URL}, nil
}

// ProcessWebhook handles Stripe webhook events.
// It verifies the signature, then credits balance on payment_intent.succeeded.
// ProcessWebhook handles Stripe webhook events.
// It verifies the signature unless endpointSecret is empty (dev/test mode).
func (p *Paygate) ProcessWebhook(payload []byte, sig string) error {
	var event stripe.Event
	var err error
	if p.endpointSecret == "" {
		// Dev/test mode: skip signature verification.
		err = json.Unmarshal(payload, &event)
	} else {
		event, err = webhook.ConstructEvent(payload, sig, p.endpointSecret)
	}
	if err != nil {
		return fmt.Errorf("webhook processing: %w", err)
	}
	return p.handleEvent(event)
}

// handleEvent routes an already-verified Stripe event to the appropriate handler.
func (p *Paygate) handleEvent(event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		return p.handleCheckoutCompleted(event)
	case "payment_intent.succeeded":
		return p.handlePaymentSuccess(event)
	case "payment_intent.payment_failed":
		return p.handlePaymentFailure(event)
	default:
		return nil // ignore unhandled events
	}
}

// handleCheckoutCompleted is the primary flow: extract api_key + amount from
// the Checkout Session metadata and credit the balance.
func (p *Paygate) handleCheckoutCompleted(event stripe.Event) error {
	if p.balanceStore == nil {
		return nil
	}
	var sess struct {
		Metadata struct {
			APIKey      string `json:"api_key"`
			AmountCents string `json:"amount_cents"`
		} `json:"metadata"`
		PaymentStatus string `json:"payment_status"`
	}
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return fmt.Errorf("parse checkout.session.completed: %w", err)
	}
	if sess.PaymentStatus != "paid" {
		return nil
	}
	if sess.Metadata.APIKey == "" {
		return ErrMissingAPIKey
	}
	amount, err := parseCents(sess.Metadata.AmountCents)
	if err != nil {
		return fmt.Errorf("parse amount_cents: %w", err)
	}
	p.balanceStore.Add(sess.Metadata.APIKey, amount)
	return nil
}

func (p *Paygate) handlePaymentSuccess(event stripe.Event) error {
	if p.balanceStore == nil {
		return nil
	}
	var sess struct {
		Metadata struct {
			APIKey      string `json:"api_key"`
			AmountCents string `json:"amount_cents"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return fmt.Errorf("parse payment_intent.succeeded: %w", err)
	}
	if sess.Metadata.APIKey == "" {
		return ErrMissingAPIKey
	}
	amount, err := parseCents(sess.Metadata.AmountCents)
	if err != nil {
		return fmt.Errorf("parse amount_cents: %w", err)
	}
	p.balanceStore.Add(sess.Metadata.APIKey, amount)
	return nil
}

func (p *Paygate) handlePaymentFailure(event stripe.Event) error {
	// Log but don't surface as error — Stripe will retry.
	fmt.Printf("[stripe] payment failed: %s\n", event.ID)
	return nil
}

func parseCents(s string) (int64, error) {
	if s == "" {
		return 0, ErrMissingAmount
	}
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
