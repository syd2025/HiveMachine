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
// WalletDepositAddressResponse is the response from GET /v1/wallet/deposit-address.
type WalletDepositAddressResponse struct {
	Rail   string `json:"rail"`
	Address string `json:"address"`
}

// WalletDepositAddress queries the deposit address for a given rail.
func (c *Client) WalletDepositAddress(ctx context.Context, apiKey, rail string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+c.addr+"/v1/wallet/deposit-address?rail="+rail+"&api_key="+apiKey, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /v1/wallet/deposit-address: HTTP %d", resp.StatusCode)
	}
	var result WalletDepositAddressResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Address, nil
}
// BalancesResponse is the response from GET /v1/balances.
type BalancesResponse struct {
	APIKey     string           `json:"api_key"`
	Balances   map[string]int64 `json:"balances"`
	TotalCents int64            `json:"total_cents"`
}

// Balances queries all rail balances for an API key.
func (c *Client) Balances(ctx context.Context, apiKey string) (*BalancesResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+c.addr+"/v1/balances?api_key="+apiKey, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/balances: HTTP %d", resp.StatusCode)
	}
	var result BalancesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DepositResponse is the response from POST /v1/balances/deposit.
type DepositResponse struct {
	DepositID   string `json:"deposit_id"`
	URL         string `json:"url"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

// Deposit initiates a deposit on a given rail and returns the destination address/URL.
func (c *Client) Deposit(ctx context.Context, apiKey, rail string, amountCents int64) (*DepositResponse, error) {
	body := fmt.Sprintf(`{"api_key":"%s","rail":"%s","amount_cents":%d,"currency":"usd"}`,
		apiKey, rail, amountCents)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://"+c.addr+"/v1/balances/deposit", strings.NewReader(body))
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
		return nil, fmt.Errorf("POST /v1/balances/deposit: HTTP %d", resp.StatusCode)
	}
	var result DepositResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}
// P2PInfoResponse is the response from GET /v1/p2p/info.
type P2PInfoResponse struct {
	PeerID string   `json:"peer_id"`
	Addrs  []string `json:"addrs"`
}

// P2PInfo returns the local P2P node info.
func (c *Client) P2PInfo(ctx context.Context) (*P2PInfoResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.addr+"/v1/p2p/info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/p2p/info: HTTP %d", resp.StatusCode)
	}
	var result P2PInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// P2PPeersResponse is the response from GET /v1/p2p/peers.
type P2PPeersResponse struct {
	Peers []struct {
		ID string `json:"id"`
	} `json:"peers"`
}

// P2PPeers returns all connected P2P peers.
func (c *Client) P2PPeers(ctx context.Context) (*P2PPeersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+c.addr+"/v1/p2p/peers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/p2p/peers: HTTP %d", resp.StatusCode)
	}
	var result P2PPeersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// P2PProvide announces a key via DHT.
func (c *Client) P2PProvide(ctx context.Context, key string) error {
	body := fmt.Sprintf(`{"key":"%s"}`, key)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://"+c.addr+"/v1/p2p/provide", strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /v1/p2p/provide: HTTP %d", resp.StatusCode)
	}
	return nil
}

// P2PProvider describes a provider returned by DHT lookup.
type P2PProvider struct {
	PeerID string   `json:"peer_id"`
	Addrs  []string `json:"addresses"`
}

// P2PProviders searches DHT for providers of a key.
func (c *Client) P2PProviders(ctx context.Context, key string) ([]P2PProvider, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"http://"+c.addr+"/v1/p2p/providers?key="+key, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/p2p/providers: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Providers []P2PProvider `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Providers, nil
}
