// Package dashboard provides user and provider dashboard data.
package dashboard

import (
	"fmt"
	"time"
)

// UserDashboardData holds data for the user dashboard.
type UserDashboardData struct {
	Username     string
	Balance     BalanceInfo
	Usage       UsageInfo
	Tokens      []TokenInfo
	Receipts    []ReceiptInfo
	History     []SessionHistory
	UpdatedAt   time.Time
}

// BalanceInfo holds balance information.
type BalanceInfo struct {
	Fiat float64 `json:"fiat"` // USD cents
	TAP  int64  `json:"tap"`  // wei
	TNK  int64  `json:"tnk"`  // TNK wei
}

// UsageInfo holds usage statistics.
type UsageInfo struct {
	TotalTokensToday    int64 `json:"total_tokens_today"`
	TotalTokensMonth    int64 `json:"total_tokens_month"`
	TotalSpendToday     float64 `json:"total_spend_today"`
	TotalSpendMonth     float64 `json:"total_spend_month"`
	RequestsToday       int `json:"requests_today"`
	AverageLatencyMs    int `json:"average_latency_ms"`
}

// TokenInfo holds bearer token information.
type TokenInfo struct {
	Name      string    `json:"name"`
	Key       string    `json:"key"` // masked
	Budget    float64   `json:"budget_per_day"`
	SpentToday float64   `json:"spent_today"`
	RateLimit int       `json:"rate_limit_per_minute"`
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

// ReceiptInfo holds receipt information.
type ReceiptInfo struct {
	ID          string    `json:"id"`
	ProviderID  string    `json:"provider_id"`
	Model       string    `json:"model"`
	InputTokens int       `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	CostCents   float64   `json:"cost_cents"`
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status"` // completed, disputed, refunded
}

// SessionHistory holds session history.
type SessionHistory struct {
	ID          string    `json:"id"`
	Model       string    `json:"model"`
	ProviderID  string    `json:"provider_id"`
	StartedAt   time.Time `json:"started_at"`
	DurationMs  int       `json:"duration_ms"`
	InputTokens int       `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	CostCents   float64   `json:"cost_cents"`
	Status      string    `json:"status"` // completed, failed, cancelled
}

// ProviderDashboardData holds data for the provider dashboard.
type ProviderDashboardData struct {
	ProviderID   string
	Reputation   ReputationInfo
	Capacity     CapacityInfo
	Earnings     EarningsInfo
	Routes       []RouteInfo
	Models       []ModelInfo
	UpdatedAt    time.Time
}

// ReputationInfo holds reputation information.
type ReputationInfo struct {
	Score           float64 `json:"score"` // 0-100
	TotalSessions   int64   `json:"total_sessions"`
	SuccessfulRate  float64 `json:"successful_rate"` // percentage
	AvgLatencyMs    int     `json:"avg_latency_ms"`
	UptimeHours     int     `json:"uptime_hours"`
	LastActiveAt    time.Time `json:"last_active_at"`
}

// CapacityInfo holds capacity information.
type CapacityInfo struct {
	GPUModel       string  `json:"gpu_model"`
	VRAMTotalMB    int64   `json:"vram_total_mb"`
	VRAMUsedMB     int64   `json:"vram_used_mb"`
	MaxConcurrency int     `json:"max_concurrency"`
	CurrentLoad    int     `json:"current_load"`
	ModelsLoaded   int     `json:"models_loaded"`
}

// EarningsInfo holds earnings information.
type EarningsInfo struct {
	Today          float64 `json:"today_cents"`
	ThisWeek       float64 `json:"this_week_cents"`
	ThisMonth      float64 `json:"this_month_cents"`
	PendingPayout  float64 `json:"pending_payout_cents"`
	LastPayoutAt   *time.Time `json:"last_payout_at,omitempty"`
	TotalEarned    float64 `json:"total_earned_cents"`
	Currency       string  `json:"currency"`
}

// RouteInfo holds route information.
type RouteInfo struct {
	Model       string `json:"model"`
	Active      bool   `json:"active"`
	MinAskCPM   int64  `json:"min_ask_cpm"` // cents per 1M tokens
	CurrentPrice int64  `json:"current_price_cpm"`
	OnlineCount int    `json:"online_count"`
	Capacity    int    `json:"capacity"`
}

// ModelInfo holds model information.
type ModelInfo struct {
	ModelID      string  `json:"model_id"`
	ClassType    string  `json:"class_type"`
	MemoryMB     int64   `json:"memory_mb"`
	Loaded       bool    `json:"loaded"`
	LoadTimeMs   int     `json:"load_time_ms"`
	TokensPerSec int     `json:"tokens_per_sec"`
}

// NetworkDashboardData holds data for the network explorer.
type NetworkDashboardData struct {
	NetworkStats NetworkStats `json:"network_stats"`
	Markets      []MarketInfo `json:"markets"`
	Providers    []ProviderSummary `json:"providers"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NetworkStats holds overall network statistics.
type NetworkStats struct {
	TotalProviders    int     `json:"total_providers"`
	ActiveProviders   int     `json:"active_providers"`
	TotalModels       int     `json:"total_models"`
	ActiveModels      int     `json:"active_models"`
	TotalRoutes       int     `json:"total_routes"`
	ActiveRoutes      int     `json:"active_routes"`
	TotalCapacityTPM  int64   `json:"total_capacity_tpm"` // tokens per minute
	AvgMarketPrice    float64 `json:"avg_market_price"`
}

// MarketInfo holds market information.
type MarketInfo struct {
	Model       string  `json:"model"`
	Tier        int     `json:"tier"`
	PriceCPM    int64   `json:"price_cpm"` // cents per 1M tokens
	Utilization float64 `json:"utilization"` // percentage
	ProviderCount int   `json:"provider_count"`
	RouteCount    int   `json:"route_count"`
}

// ProviderSummary holds a summary of a provider.
type ProviderSummary struct {
	ProviderID  string  `json:"provider_id"`
	Reputation  float64 `json:"reputation"`
	ModelCount  int     `json:"model_count"`
	Tier        int     `json:"tier"`
	Online      bool    `json:"online"`
}

// FormatBalance formats a balance for display.
func FormatBalance(balance float64, currency string) string {
	return fmt.Sprintf("$%.2f %s", balance/100, currency)
}

// FormatLatency formats latency for display.
func FormatLatency(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// FormatTokens formats token count for display.
func FormatTokens(count int64) string {
	if count < 1000 {
		return fmt.Sprintf("%d", count)
	}
	if count < 1000000 {
		return fmt.Sprintf("%.1fK", float64(count)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(count)/1000000)
}

// FormatDuration formats duration for display.
func FormatDuration(ms int) string {
	if ms < 60000 {
		return fmt.Sprintf("%ds", ms/1000)
	}
	if ms < 3600000 {
		return fmt.Sprintf("%dm", ms/60000)
	}
	return fmt.Sprintf("%dh", ms/3600000)
}
