package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hivemachine/pkg/gateway/api"
)

func TestPayBalanceCmd_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/balance" {
			t.Errorf("expected /v1/balance, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"api_key":       "test-key",
			"balance_cents": 5000,
			"currency":      "USD",
		})
	}))
	defer server.Close()

	orig := payClient
	payClient = func(addr string) *api.Client {
		return api.NewClient(server.Listener.Addr().String())
	}
	defer func() { payClient = orig }()

	cmd := payBalanceCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"test-key"})
	if err := runPayBalance(cmd, []string{"test-key"}); err != nil {
		t.Fatalf("runPayBalance failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "5000") {
		t.Errorf("expected balance 5000 in output, got: %s", output)
	}
	if !strings.Contains(output, "USD") {
		t.Errorf("expected USD in output, got: %s", output)
	}
}

func TestPayTopupCmd_Success(t *testing.T) {
	var calledAPIKey string
	var calledCents int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/balance/topup" {
			t.Errorf("expected /v1/balance/topup, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req struct {
			APIKey string `json:"api_key"`
			Cents  int64  `json:"cents"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		calledAPIKey = req.APIKey
		calledCents = req.Cents
		json.NewEncoder(w).Encode(map[string]any{
			"api_key":        req.APIKey,
			"balance_cents":  6000,
			"added_cents":   req.Cents,
		})
	}))
	defer server.Close()

	orig := payClient
	payClient = func(addr string) *api.Client {
		return api.NewClient(server.Listener.Addr().String())
	}
	defer func() { payClient = orig }()

	cmd := payTopupCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"my-key", "1000"})
	if err := runPayTopup(cmd, []string{"my-key", "1000"}); err != nil {
		t.Fatalf("runPayTopup failed: %v", err)
	}

	if calledAPIKey != "my-key" {
		t.Errorf("expected api_key 'my-key', got %q", calledAPIKey)
	}
	if calledCents != 1000 {
		t.Errorf("expected cents 1000, got %d", calledCents)
	}

	output := buf.String()
	if !strings.Contains(output, "1000") {
		t.Errorf("expected added amount 1000 in output, got: %s", output)
	}
}

func TestPayTopupCmd_InvalidCents(t *testing.T) {
	cmd := payTopupCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"my-key", "not-a-number"})
	err := runPayTopup(cmd, []string{"my-key", "not-a-number"})
	if err == nil {
		t.Error("expected error for non-numeric cents")
	}
}

func TestPayTopupCmd_NegativeCents(t *testing.T) {
	cmd := payTopupCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"my-key", "-5"})
	err := runPayTopup(cmd, []string{"my-key", "-5"})
	if err == nil {
		t.Error("expected error for negative cents")
	}
}

func TestPayTopupCmd_ZeroCents(t *testing.T) {
	cmd := payTopupCmd
	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"my-key", "0"})
	err := runPayTopup(cmd, []string{"my-key", "0"})
	if err == nil {
		t.Error("expected error for zero cents")
	}
}
