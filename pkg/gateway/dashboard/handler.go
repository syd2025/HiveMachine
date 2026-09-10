// Package dashboard provides HTTP handlers for dashboards.
package dashboard

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

//go:embed templates/*
var templateFS embed.FS

// Server handles dashboard HTTP requests.
type Server struct {
	userData      func(apiKey string) *UserDashboardData
	providerData  func(providerID string) *ProviderDashboardData
	networkData  func() *NetworkDashboardData
	templates    *template.Template
}

// NewServer creates a new dashboard server.
func NewServer(
	userData func(apiKey string) *UserDashboardData,
	providerData func(providerID string) *ProviderDashboardData,
	networkData func() *NetworkDashboardData,
) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		// Fall back to embedded templates
		tmpl = template.New("dashboard")
	}

	return &Server{
		userData:     userData,
		providerData: providerData,
		networkData: networkData,
		templates:   tmpl,
	}, nil
}

// Handler returns HTTP handlers for dashboard routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mayhem/dashboard", s.userDashboard)
	mux.HandleFunc("/mayhem/dashboard/provider", s.providerDashboard)
	mux.HandleFunc("/mayhem/dashboard/network", s.networkDashboard)
	mux.HandleFunc("/mayhem/api/dashboard/user", s.userAPI)
	mux.HandleFunc("/mayhem/api/dashboard/provider", s.providerAPI)
	mux.HandleFunc("/mayhem/api/dashboard/network", s.networkAPI)
	return mux
}

func (s *Server) userDashboard(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("Authorization")
	if apiKey != "" {
		apiKey = apiKey[7:] // Remove "Bearer "
	}

	data := &UserDashboardData{
		UpdatedAt: time.Now(),
	}
	if s.userData != nil {
		data = s.userData(apiKey)
	}

	// Render simple HTML
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>User Dashboard - HiveMachine</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .card { background: white; border-radius: 8px; padding: 20px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        .balance { font-size: 2em; color: #007bff; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; }
        .metric { text-align: center; }
        .metric-value { font-size: 1.5em; font-weight: bold; color: #333; }
        .metric-label { color: #666; font-size: 0.9em; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #eee; }
        th { background: #f8f8f8; font-weight: 600; }
    </style>
</head>
<body>
    <div class="container">
        <h1>User Dashboard</h1>

        <div class="card">
            <h2>Balance</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">Fiat (USD)</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%.6f</div>
                    <div class="metric-label">TAP (ETH)</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%.6f</div>
                    <div class="metric-label">TNK</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Usage Today</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">%s</div>
                    <div class="metric-label">Tokens</div>
                </div>
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">Spent</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Requests</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>API Tokens</h2>
            <table>
                <tr><th>Name</th><th>Key</th><th>Budget</th><th>Spent Today</th><th>Status</th></tr>
    `, data.Balance.Fiat/100, float64(data.Balance.TAP)/1e18, float64(data.Balance.TNK)/1e18,
		FormatTokens(data.Usage.TotalTokensToday), data.Usage.TotalSpendToday/100, data.Usage.RequestsToday)

	for _, token := range data.Tokens {
		status := "Active"
		if !token.Active {
			status = "Inactive"
		}
		fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>$%.2f</td><td>$%.2f</td><td>%s</td></tr>`,
			token.Name, token.Key, token.Budget/100, token.SpentToday/100, status)
	}

	fmt.Fprintf(w, `
            </table>
        </div>

        <div class="card">
            <h2>Recent Sessions</h2>
            <table>
                <tr><th>Time</th><th>Model</th><th>Provider</th><th>Tokens</th><th>Cost</th><th>Status</th></tr>
    `)

	for _, session := range data.History {
		timeStr := session.StartedAt.Format("15:04")
		fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s...</td><td>%s</td><td>$%.4f</td><td>%s</td></tr>`,
			timeStr, session.Model, session.ProviderID[:8],
			FormatTokens(int64(session.InputTokens+session.OutputTokens)),
			session.CostCents/100, session.Status)
	}

	fmt.Fprintf(w, `
            </table>
        </div>
    </div>
</body>
</html>`)
}

func (s *Server) providerDashboard(w http.ResponseWriter, r *http.Request) {
	providerID := r.Header.Get("X-Provider-ID")

	data := &ProviderDashboardData{
		UpdatedAt: time.Now(),
	}
	if s.providerData != nil {
		data = s.providerData(providerID)
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Provider Dashboard - HiveMachine</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .card { background: white; border-radius: 8px; padding: 20px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; }
        .metric { text-align: center; }
        .metric-value { font-size: 1.5em; font-weight: bold; color: #333; }
        .metric-label { color: #666; font-size: 0.9em; }
        .status-online { color: #28a745; }
        .status-offline { color: #dc3545; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #eee; }
        th { background: #f8f8f8; font-weight: 600; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Provider Dashboard</h1>

        <div class="card">
            <h2>Reputation</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">%.0f</div>
                    <div class="metric-label">Score</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Total Sessions</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%.1f%%</div>
                    <div class="metric-label">Success Rate</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%dms</div>
                    <div class="metric-label">Avg Latency</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Earnings</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">Today</div>
                </div>
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">This Week</div>
                </div>
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">This Month</div>
                </div>
                <div class="metric">
                    <div class="metric-value">$%.2f</div>
                    <div class="metric-label">Pending</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Capacity</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">%s</div>
                    <div class="metric-label">GPU</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d / %d MB</div>
                    <div class="metric-label">VRAM</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Concurrency</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Models Loaded</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Routes</h2>
            <table>
                <tr><th>Model</th><th>Status</th><th>Min Ask</th><th>Price</th><th>Capacity</th></tr>
    `, data.Reputation.Score, data.Reputation.TotalSessions, data.Reputation.SuccessfulRate,
		data.Reputation.AvgLatencyMs, data.Earnings.Today/100, data.Earnings.ThisWeek/100,
		data.Earnings.ThisMonth/100, data.Earnings.PendingPayout/100,
		data.Capacity.GPUModel, data.Capacity.VRAMUsedMB, data.Capacity.VRAMTotalMB,
		data.Capacity.CurrentLoad, data.Capacity.ModelsLoaded)

	for _, route := range data.Routes {
		status := `<span class="status-online">Online</span>`
		if !route.Active {
			status = `<span class="status-offline">Offline</span>`
		}
		fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>$%.4f</td><td>$%.4f</td><td>%d</td></tr>`,
			route.Model, status, float64(route.MinAskCPM)/1e6, float64(route.CurrentPrice)/1e6, route.Capacity)
	}

	fmt.Fprintf(w, `
            </table>
        </div>
    </div>
</body>
</html>`)
}

func (s *Server) networkDashboard(w http.ResponseWriter, r *http.Request) {
	data := &NetworkDashboardData{
		UpdatedAt: time.Now(),
	}
	if s.networkData != nil {
		data = s.networkData()
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>Network Explorer - HiveMachine</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; }
        .card { background: white; border-radius: 8px; padding: 20px; margin-bottom: 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
        h1 { color: #333; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; }
        .metric { text-align: center; }
        .metric-value { font-size: 1.5em; font-weight: bold; color: #333; }
        .metric-label { color: #666; font-size: 0.9em; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 12px; text-align: left; border-bottom: 1px solid #eee; }
        th { background: #f8f8f8; font-weight: 600; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Network Explorer</h1>

        <div class="card">
            <h2>Network Statistics</h2>
            <div class="grid">
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Total Providers</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Active Providers</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Total Models</div>
                </div>
                <div class="metric">
                    <div class="metric-value">%d</div>
                    <div class="metric-label">Active Routes</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Markets</h2>
            <table>
                <tr><th>Model</th><th>Tier</th><th>Price CPM</th><th>Utilization</th><th>Providers</th></tr>
    `, data.NetworkStats.TotalProviders, data.NetworkStats.ActiveProviders,
		data.NetworkStats.TotalModels, data.NetworkStats.ActiveRoutes)

	for _, market := range data.Markets {
		fmt.Fprintf(w, `<tr><td>%s</td><td>%d</td><td>$%.4f</td><td>%.1f%%</td><td>%d</td></tr>`,
			market.Model, market.Tier, float64(market.PriceCPM)/1e6,
			market.Utilization, market.ProviderCount)
	}

	fmt.Fprintf(w, `
            </table>
        </div>
    </div>
</body>
</html>`)
}

func (s *Server) userAPI(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("Authorization")
	if apiKey != "" {
		apiKey = apiKey[7:]
	}

	data := &UserDashboardData{}
	if s.userData != nil {
		data = s.userData(apiKey)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"balance": {"fiat": %.2f, "tap": %d, "tnk": %d}, "usage": {"tokens_today": %d, "spend_today": %.2f}}`,
		data.Balance.Fiat/100, data.Balance.TAP, data.Balance.TNK,
		data.Usage.TotalTokensToday, data.Usage.TotalSpendToday/100)
}

func (s *Server) providerAPI(w http.ResponseWriter, r *http.Request) {
	providerID := r.Header.Get("X-Provider-ID")

	data := &ProviderDashboardData{}
	if s.providerData != nil {
		data = s.providerData(providerID)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"reputation": {"score": %.0f, "sessions": %d}, "earnings": {"today": %.2f, "pending": %.2f}}`,
		data.Reputation.Score, data.Reputation.TotalSessions,
		data.Earnings.Today/100, data.Earnings.PendingPayout/100)
}

func (s *Server) networkAPI(w http.ResponseWriter, r *http.Request) {
	data := &NetworkDashboardData{}
	if s.networkData != nil {
		data = s.networkData()
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"providers": {"total": %d, "active": %d}, "routes": {"total": %d, "active": %d}}`,
		data.NetworkStats.TotalProviders, data.NetworkStats.ActiveProviders,
		data.NetworkStats.TotalRoutes, data.NetworkStats.ActiveRoutes)
}
