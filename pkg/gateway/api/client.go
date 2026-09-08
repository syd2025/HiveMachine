package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client is a lightweight HTTP client for the gateway API.
// Used by the CLI; the gateway server itself uses grpc.Client.
type Client struct {
	addr   string
	client *http.Client
}

// Config holds client configuration.
type Config struct {
	Addr    string // gateway address, e.g. "127.0.0.1:11435"
	Timeout time.Duration
}

// DefaultConfig is the CLI default.
var DefaultConfig = Config{
	Addr:    "127.0.0.1:11435",
	Timeout: 10 * time.Second,
}

// NewClient creates a client targeting the given gateway address.
func NewClient(addr string) *Client {
	return &Client{
		addr: addr,
		client: &http.Client{
			Timeout: DefaultConfig.Timeout,
		},
	}
}

// Health checks if the gateway is reachable.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.addr+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Model represents an OpenAI-compatible model entry.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ListModelsResponse mirrors the OpenAI /v1/models response.
type ListModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// ListModels queries GET /v1/models from the gateway.
func (c *Client) ListModels(ctx context.Context) (*ListModelsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.addr+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/models: HTTP %d", resp.StatusCode)
	}
	var result ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Provider represents a provider entry from /providers.
type Provider struct {
	ID      string  `json:"id"`
	Endpoint string `json:"endpoint"`
	Status  string `json:"status"`
	Reputation float64 `json:"reputation"`
	Models  []string `json:"models"`
}
// ListProvidersResponse is the response from /providers.
type ListProvidersResponse struct {
	Data []Provider `json:"data"`
}

// ListProviders queries GET /providers from the gateway.
func (c *Client) ListProviders(ctx context.Context) (*ListProvidersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.addr+"/providers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /providers: HTTP %d", resp.StatusCode)
	}
	var result ListProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BalanceResponse is the gateway's balance state for an API key.
type BalanceResponse struct {
	APIKey       string `json:"api_key"`
	BalanceCents  int64  `json:"balance_cents"`
	Currency      string `json:"currency"` // always "USD"
}

// TopUpResponse is the response after adding credits.
type TopUpResponse struct {
	APIKey        string `json:"api_key"`
	BalanceCents  int64  `json:"balance_cents"`
	AddedCents   int64  `json:"added_cents"`
}

func (c *Client) Balance(ctx context.Context, apiKey string) (*BalanceResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+c.addr+"/v1/balance?api_key="+apiKey, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/balance: HTTP %d", resp.StatusCode)
	}
	var result BalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TopUp adds credits to an account via POST /v1/balance/topup.
func (c *Client) TopUp(ctx context.Context, apiKey string, cents int64) (*TopUpResponse, error) {
	body := fmt.Sprintf(`{"api_key":"%s","cents":%d}`, apiKey, cents)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://"+c.addr+"/v1/balance/topup", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("POST /v1/balance/topup: HTTP %d", resp.StatusCode)
	}
	var result TopUpResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
