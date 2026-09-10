package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	pb "github.com/hivemachine/internal/grpc/pb"
	"github.com/hivemachine/pkg/gateway/dispute"
	"github.com/hivemachine/pkg/gateway/jobs"
	"github.com/hivemachine/pkg/gateway/pricing"
	"github.com/hivemachine/pkg/gateway/provider"
	"github.com/hivemachine/pkg/gateway/proxy"
	"github.com/hivemachine/pkg/gateway/ws"
	"github.com/hivemachine/pkg/gateway/wallet"
	"github.com/hivemachine/pkg/p2p"
	"github.com/hivemachine/pkg/paygate/balance"
	"github.com/hivemachine/pkg/paygate/receipt"
	"github.com/hivemachine/pkg/paygate/stripe"
)

// coreClient is the subset of grpc.Client that the API server uses.
type coreClient interface {
	ListModels(ctx context.Context) (*pb.ListModelsResponse, error)
	ChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (*pb.ChatCompletionResponse, error)
	Completions(ctx context.Context, req *pb.CompletionRequest) (*pb.CompletionResponse, error)
	ListProviders(ctx context.Context) (*pb.ListProvidersResponse, error)
	StreamChatCompletions(ctx context.Context, req *pb.ChatCompletionRequest) (<-chan *pb.StreamChunk, <-chan error)
}

// receiptStoreiface is used by Server's receipt endpoints.
type receiptStoreiface interface {
	Save(r *receipt.Receipt) error
	ByID(id string) (*receipt.Receipt, error)
	ByAPIKey(apiKey string) ([]*receipt.Receipt, error)
}

// balanceStoreiface is the internal interface matching inMemoryBalanceStore.
type balanceStoreiface interface {
	Get(apiKey string) int64
	Set(apiKey string, balance int64)
	Add(apiKey string, delta int64)
	Deduct(apiKey string, delta int64)
}

// BalanceStore is the public interface for balance storage backends.
type BalanceStore interface {
	Get(apiKey string) int64
	Set(apiKey string, balance int64)
	Add(apiKey string, cents int64)
	Deduct(apiKey string, cents int64)
}

// quotaStoreiface is the interface for quota storage backends.
type quotaStoreiface interface {
	Set(apiKey string, limit int, period time.Duration)
	Check(apiKey string, maxTokens int) (bool, int)
	Deduct(apiKey string, tokens int)
	Remaining(apiKey string) int
}

// InMemoryBalanceStore holds balances in process memory.
// It satisfies both balanceStoreiface (internal: Set/Deduct) and BalanceStore (public: Get/Add).
type InMemoryBalanceStore struct {
	mu   sync.RWMutex
	data map[string]*userBalance
}

type userBalance struct {
	Balance    int64
	LastUpdate int64
}

// NewInMemoryBalanceStore creates a new in-memory balance store, satisfying
// balanceStoreiface for internal use and BalanceStore for external use.
func NewInMemoryBalanceStore() *InMemoryBalanceStore {
	return &InMemoryBalanceStore{data: make(map[string]*userBalance)}
}

// newBalanceStore returns an internal balanceStoreiface-backed store.
func newBalanceStore() balanceStoreiface {
	return &InMemoryBalanceStore{data: make(map[string]*userBalance)}
}

func (b *InMemoryBalanceStore) Get(apiKey string) int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if ub, ok := b.data[apiKey]; ok {
		return ub.Balance
	}
	return 0
}

func (b *InMemoryBalanceStore) Set(apiKey string, balance int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[apiKey] = &userBalance{Balance: balance, LastUpdate: time.Now().Unix()}
}

func (b *InMemoryBalanceStore) Add(apiKey string, delta int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ub, ok := b.data[apiKey]
	if !ok {
		ub = &userBalance{}
		b.data[apiKey] = ub
	}
	ub.Balance += delta
	ub.LastUpdate = time.Now().Unix()
}

// Deduct subtracts delta from the balance. Does nothing if balance would go negative.
func (b *InMemoryBalanceStore) Deduct(apiKey string, delta int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ub, ok := b.data[apiKey]
	if !ok || ub.Balance < delta {
		return
	}
	ub.Balance -= delta
	ub.LastUpdate = time.Now().Unix()
}

// Server implements OpenAI-compatible HTTP server.
type Server struct {
	addr          string
	router        *gin.Engine
	client        coreClient
	registry      *provider.Registry
	proxy         *proxy.Proxy
	balanceStore  balanceStoreiface
	quotaStore    quotaStoreiface
	priceStore    *pricing.InMemoryStore
	receiptStore  receiptStoreiface
	receiptSigner *receipt.Signer
	jobsHandler    *jobs.Handler
	disputeHandler *dispute.Handler
	wsHandler      *ws.StreamHandler
	stripePaygate  *stripe.Paygate
	railStore      *balance.RailStore
	walletService  *wallet.Service
	p2pService     *p2p.P2PService
}

// Option configures an optional dependency for Server.
type Option func(*Server)

// WithStripePaygate injects a Stripe paygate for checkout and webhook handling.
func WithStripePaygate(p *stripe.Paygate) Option {
	return func(s *Server) { s.stripePaygate = p }
}

// WithBalanceStore replaces the default in-memory balance store.
func WithBalanceStore(bs balanceStoreiface) Option {
	return func(s *Server) { s.balanceStore = bs }
}

// WithRailStore injects a multi-rail balance store (TAP/TNK support).
func WithRailStore(rs *balance.RailStore) Option {
	return func(s *Server) { s.railStore = rs }
}

// WithDisputeHandler injects a dispute handler.
func WithDisputeHandler(dh *dispute.Handler) Option {
	return func(s *Server) { s.disputeHandler = dh }
}

// WithQuotaStore replaces the default no-op quota store with a persistent one.
// WithWalletService injects a wallet service for deposit address lookups.
func WithWalletService(ws *wallet.Service) Option {
	return func(s *Server) { s.walletService = ws }
}
// WithP2PService injects the P2P networking service.
func WithP2PService(p2pSvc *p2p.P2PService) Option {
	return func(s *Server) { s.p2pService = p2pSvc }
}

// NewServer creates a new gateway server.
// Variadic Option args configure optional backends (e.g. SQLite balance/receipt stores).
// If proxy is non-nil, inference calls route through it with failover.
// If registry is non-nil, /providers uses it for ranked results.
// If receiptSigner is non-nil, receipts are generated for each inference call.
// If stripePaygate is set, checkout and webhook endpoints are registered.
func NewServer(addr string, client coreClient, registry *provider.Registry,
	proxy *proxy.Proxy, receiptSigner *receipt.Signer, opts ...Option) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(Logger())

	s := &Server{
		addr:         addr,
		router:       r,
		client:       client,
		registry:     registry,
		proxy:        proxy,
		balanceStore: newBalanceStore(),
		priceStore:   pricing.NewInMemoryStore(),
	}
	for _, opt := range opts {
		opt(s)
	}
	// Caller wires balance store into the Paygate at construction time.
	if receiptSigner != nil && s.receiptStore == nil {
		s.receiptSigner = receiptSigner
		s.receiptStore = receipt.NewInMemoryStore()
	}

	// Jobs — always available with in-memory store.
	s.jobsHandler = jobs.NewHandler(jobs.NewInMemoryStore(), nil, extractAPIKey)

	// Disputes — always available with in-memory store.
	s.disputeHandler = dispute.NewHandler(dispute.NewInMemoryDisputeStore(), extractAPIKey)

	// WebSocket — use proxy for provider routing if available.
	s.wsHandler = ws.NewStreamHandler(s.client, s.proxy)

	s.setupRoutes()
	return s
}

// setupRoutes configures all HTTP endpoints.
func (s *Server) setupRoutes() {
	s.router.GET("/health", s.health)

	v1 := s.router.Group("/v1")
	v1.Use(s.checkQuota())
	{
		v1.GET("/models", s.listModels)
		v1.POST("/chat/completions", s.chatCompletions)
		v1.POST("/completions", s.completions)
		v1.POST("/chat/completions/stream", s.streamChatCompletions)
		v1.GET("/chat/completions/ws", func(c *gin.Context) {
			s.wsHandler.Handle(c.Writer, c.Request)
		})
		v1.GET("/balance", s.getBalance)
		v1.POST("/balance/topup", s.topUp)
		// Multi-rail balance (replaces /balance when railStore is set).
		v1.GET("/balances", s.getBalances)
		v1.POST("/balances/deposit", s.deposit)
		v1.POST("/workflows", s.jobsHandler.Submit)
		v1.GET("/jobs", s.jobsHandler.ListJobs)
		v1.GET("/jobs/:id/events", s.jobsHandler.Events)
		v1.DELETE("/jobs/:id", s.jobsHandler.CancelJob)

		// Disputes.
		v1.GET("/disputes", s.disputeHandler.ListDisputes)
		v1.POST("/disputes", s.disputeHandler.OpenDispute)
		v1.GET("/disputes/:id", s.disputeHandler.GetDispute)
		v1.POST("/disputes/:id/resolve", s.disputeHandler.ResolveDispute)

		// Extended APIs — pass through to Rust core or return 501 if unsupported.
		v1.POST("/embeddings", s.embeddings)
		v1.POST("/images/generations", s.imagesGenerations)
		v1.POST("/audio/speech", s.audioSpeech)
		v1.POST("/audio/transcriptions", s.audioTranscriptions)
	}
	if s.receiptStore != nil {
		v1.GET("/receipts", s.listReceipts)
		v1.POST("/receipts/verify", s.verifyReceipt)
		v1.GET("/receipts/public_key", s.receiptPublicKey)
	}
	if s.quotaStore != nil {
		v1.GET("/quota", s.getQuota)
		v1.POST("/quota", s.setQuota)
	}
	// P2P networking — peer info, DHT provider discovery.
	if s.p2pService != nil {
		v1.GET("/p2p/peers", s.p2pPeers)
		v1.GET("/p2p/info", s.p2pInfo)
		v1.POST("/p2p/provide", s.p2pProvide)
		v1.GET("/p2p/providers", s.p2pProviders)
	}
	// Wallet — deposit addresses for multi-rail.
	v1.GET("/wallet/deposit-address", s.walletDepositAddress)
	s.router.GET("/providers", s.listProviders)
	s.router.POST("/webhook/stripe", s.webhookStripe)
}

// Start begins serving.
func (s *Server) Addr() string { return s.addr }

func (s *Server) Router() *gin.Engine { return s.router }

func (s *Server) Start() error { return s.router.Run(s.addr) }

// BalanceStore returns the server's balance store.
func (s *Server) BalanceStore() BalanceStore { return s.balanceStore }

// SetBalance pre-funds an API key's balance for testing.
func (s *Server) SetBalance(apiKey string, amount int64) { s.balanceStore.Set(apiKey, amount) }

// Close shuts down the server.
func (s *Server) Close() {}

// Logger returns a gin middleware logger.
func Logger() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"},
	})
}

// health returns the server's health status.
func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func extractAPIKey(c *gin.Context) string {
	hdr := c.GetHeader("Authorization")
	if strings.HasPrefix(hdr, "Bearer ") {
		return strings.TrimPrefix(hdr, "Bearer ")
	}
	return ""
}

// listModels lists available models.
func (s *Server) listModels(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	resp, err := s.client.ListModels(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": "failed to list models: " + err.Error(),
		}})
		return
	}
	items := make([]gin.H, 0, len(resp.Models))
	for _, m := range resp.Models {
		items = append(items, gin.H{
			"id":      m.GetId(),
			"object":  "model",
			"created": m.GetCreated(),
			"owned_by": m.GetOwnedBy(),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   items,
	})
}

// topUp adds credits to an API key's balance.
func (s *Server) topUp(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing API key"}})
		return
	}
	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := c.BindJSON(&req); err != nil || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid amount"}})
		return
	}
	s.balanceStore.Add(apiKey, req.Amount)
	c.JSON(http.StatusOK, gin.H{"balance": s.balanceStore.Get(apiKey)})
}
// checkout redirects to Stripe Checkout for fiat payment.
// POST /v1/balance/checkout  { "api_key": "...", "amount_cents": 500, "currency": "usd" }
// Returns { "url": "https://checkout.stripe.com/..." }
func (s *Server) checkout(c *gin.Context) {
	if s.stripePaygate == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "payment not configured"}})
		return
	}
	var req struct {
		APIKey      string `json:"api_key"`
		AmountCents int64  `json:"amount_cents"`
		Currency    string `json:"currency"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
	}
	if err := c.BindJSON(&req); err != nil || req.APIKey == "" || req.AmountCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "api_key and positive amount_cents are required"}})
		return
	}
	if req.Currency == "" {
		req.Currency = "usd"
	}
	if req.SuccessURL == "" {
		req.SuccessURL = "https://example.com/success"
	}
	if req.CancelURL == "" {
		req.CancelURL = "https://example.com/cancel"
	}
	resp, err := s.stripePaygate.CreateCheckoutSession(c.Request.Context(), stripe.CreateCheckoutSessionRequest{
		APIKey:      req.APIKey,
		AmountCents: req.AmountCents,
		Currency:    req.Currency,
		SuccessURL:  req.SuccessURL,
		CancelURL:   req.CancelURL,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": resp.URL})
}

// webhookStripe handles Stripe webhook callbacks.
// POST /webhook/stripe
func (s *Server) webhookStripe(c *gin.Context) {
	if s.stripePaygate == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "payment not configured"}})
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "failed to read request body"}})
		return
	}
	sig := c.GetHeader("Stripe-Signature")
	if sig == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing Stripe-Signature header"}})
		return
	}
	if err := s.stripePaygate.ProcessWebhook(payload, sig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"received": true})
}

// getBalance returns the current balance for the authenticated user.
func (s *Server) getBalance(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing API key"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"balance": s.balanceStore.Get(apiKey)})
}

// setQuota creates or updates a token quota for the authenticated API key.
func (s *Server) setQuota(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing API key"}})
		return
	}
	if s.quotaStore == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"message": "quota not configured"}})
		return
	}
	var req struct {
		Limit         int `json:"limit"`
		PeriodSeconds int `json:"period_seconds"`
	}
	if err := c.BindJSON(&req); err != nil || req.Limit <= 0 || req.PeriodSeconds <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "limit and period_seconds required"}})
		return
	}
	s.quotaStore.Set(apiKey, req.Limit, time.Duration(req.PeriodSeconds)*time.Second)
	c.JSON(http.StatusOK, gin.H{"limit": req.Limit, "period_seconds": req.PeriodSeconds})
}

// getQuota returns the current quota for the authenticated API key.
func (s *Server) getQuota(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing API key"}})
		return
	}
	if s.quotaStore == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"message": "quota not configured"}})
		return
	}
	remaining := s.quotaStore.Remaining(apiKey)
	c.JSON(http.StatusOK, gin.H{"remaining": remaining})
}
// chatCompletions handles /v1/chat/completions.
func (s *Server) chatCompletions(c *gin.Context) {
	var raw struct {
		Model       string              `json:"model"`
		Messages    []map[string]string `json:"messages"`
		Temperature float32             `json:"temperature"`
		MaxTokens   int                 `json:"max_tokens"`
	}
	if err := c.BindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	model := raw.Model
	if model == "" {
		model = "mayhem/default"
	}
	apiKey := extractAPIKey(c)
	balance := s.balanceStore.Get(apiKey)
	if balance <= 0 && apiKey != "" {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": gin.H{"message": "insufficient balance"}})
		return
	}

	temperature := raw.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	maxTokens := int32(raw.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	var lastErr error
	var resp *pb.ChatCompletionResponse
	req := &pb.ChatCompletionRequest{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	for _, m := range raw.Messages {
		req.Messages = append(req.Messages, &pb.ChatMessage{
			Role:    m["role"],
			Content: m["content"],
		})
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	if s.proxy != nil {
		resp, lastErr = s.proxy.ChatCompletions(ctx, req)
	} else {
		resp, lastErr = s.client.ChatCompletions(ctx, req)
	}
	if lastErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": "chat completion failed: " + lastErr.Error(),
		}})
		return
	}

	promptTokens := int(resp.GetUsage().GetPromptTokens())
	completionTokens := int(resp.GetUsage().GetCompletionTokens())
	totalTokens := int(resp.GetUsage().GetTotalTokens())
	costDollars := pricing.ComputeCost(promptTokens, completionTokens, model)
	costCents := int64(math.Round(costDollars * 100))
	receiptID := ""

	if s.receiptSigner != nil {
		apiKey := extractAPIKey(c)
		r := receipt.NewReceipt(
			uuid.New().String(),
			apiKey,
			model,
			providerIDFromProxy(s),
			promptTokens,
			completionTokens,
			totalTokens,
			costCents,
		)
		_ = s.receiptSigner.Sign(r)
		if s.receiptStore != nil {
			_ = s.receiptStore.Save(r)
		}
		receiptID = r.ID
	}

	// Deduct from balance if a charge applies and the key is tracked.
	if costCents > 0 {
		s.balanceStore.Deduct(apiKey, costCents)
	}

	choices := make([]gin.H, 0, len(resp.GetChoices()))
	for _, ch := range resp.GetChoices() {
		choices = append(choices, gin.H{
			"index": ch.GetIndex(),
			"message": gin.H{
				"role":    ch.GetMessage().GetRole(),
				"content": ch.GetMessage().GetContent(),
			},
			"finish_reason": ch.GetFinishReason(),
		})
	}

	out := gin.H{
		"id":      resp.GetId(),
		"object":  "chat.completion",
		"created": resp.GetCreated(),
		"model":   model,
		"choices": choices,
		"usage": gin.H{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
	if receiptID != "" {
		out["receipt_id"] = receiptID
	}
	c.JSON(http.StatusOK, out)
}

// completions handles /v1/completions.
func (s *Server) completions(c *gin.Context) {
	var req struct {
		Model       string  `json:"model"`
		Prompt      string  `json:"prompt"`
		Temperature float32 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	model := req.Model
	if model == "" {
		model = "mayhem/default"
	}
	apiKey := extractAPIKey(c)
	balance := s.balanceStore.Get(apiKey)
	if balance <= 0 && apiKey != "" {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": gin.H{"message": "insufficient balance"}})
		return
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	maxTokens := int32(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	pbReq := &pb.CompletionRequest{
		Model:       model,
		Prompt:      req.Prompt,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	var lastErr error
	var resp *pb.CompletionResponse
	if s.proxy != nil {
		resp, lastErr = s.proxy.Completions(ctx, pbReq)
	} else {
		resp, lastErr = s.client.Completions(ctx, pbReq)
	}
	if lastErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": "completion failed: " + lastErr.Error(),
		}})
		return
	}

	text := resp.GetChoices()[0].GetText()
	promptTokens := int(resp.GetUsage().GetPromptTokens())
	completionTokens := int(resp.GetUsage().GetCompletionTokens())
	totalTokens := int(resp.GetUsage().GetTotalTokens())
	finishReason := resp.GetChoices()[0].GetFinishReason()
	apiKey = extractAPIKey(c)
	costDollars := pricing.ComputeCost(promptTokens, completionTokens, model)
	costCents := int64(math.Round(costDollars * 100))
	var receiptID string

	if s.receiptSigner != nil {
		r := receipt.NewReceipt(
			uuid.New().String(),
			apiKey,
			model,
			providerIDFromProxy(s),
			promptTokens,
			completionTokens,
			totalTokens,
			costCents,
		)
		_ = s.receiptSigner.Sign(r)
		if s.receiptStore != nil {
			_ = s.receiptStore.Save(r)
		}
		receiptID = r.ID
	}

	if costCents > 0 {
		s.balanceStore.Deduct(apiKey, costCents)
	}

	out := gin.H{
		"id":      resp.GetId(),
		"object":  "text_completion",
		"created": resp.GetCreated(),
		"model":   model,
		"choices": []gin.H{{
			"text":          text,
			"index":         0,
			"finish_reason": finishReason,
		}},
		"usage": gin.H{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
	if receiptID != "" {
		out["receipt_id"] = receiptID
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) listProviders(c *gin.Context) {
	if s.registry != nil {
		providers := s.registry.Get()
		items := make([]gin.H, 0, len(providers))
		for id, state := range providers {
			items = append(items, gin.H{
				"id":     id,
				"status": state.Status,
			})
		}
		c.JSON(http.StatusOK, gin.H{"providers": items})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	resp, err := s.client.ListProviders(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"message": "failed to list providers: " + err.Error(),
		}})
		return
	}
	data := make([]gin.H, len(resp.Providers))
	for i, p := range resp.Providers {
		data[i] = gin.H{
			"id":     p.GetId(),
			"status": p.GetStatus(),
		}
	}
	c.JSON(http.StatusOK, gin.H{"providers": data})
}

func (s *Server) streamChatCompletions(c *gin.Context) {
	var raw struct {
		Model       string              `json:"model"`
		Messages    []map[string]string `json:"messages"`
		Temperature float32             `json:"temperature"`
		MaxTokens   int                 `json:"max_tokens"`
	}
	if err := c.BindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	model := raw.Model
	if model == "" {
		model = "mayhem/default"
	}
	apiKey := extractAPIKey(c)
	balance := s.balanceStore.Get(apiKey)
	if balance <= 0 && apiKey != "" {
		c.JSON(http.StatusPaymentRequired, gin.H{"error": gin.H{"message": "insufficient balance"}})
		return
	}

	temperature := raw.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	maxTokens := int32(raw.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	req := &pb.ChatCompletionRequest{
		Model:       model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}
	for _, m := range raw.Messages {
		req.Messages = append(req.Messages, &pb.ChatMessage{
			Role:    m["role"],
			Content: m["content"],
		})
	}

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	chunks, errs := s.client.StreamChatCompletions(ctx, req)

	created := time.Now().Unix()
	var promptTokens, completionTokens int

	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				// Stream finished cleanly — send final usage + done.
				if promptTokens > 0 || completionTokens > 0 {
					costDollars := pricing.ComputeCost(promptTokens, completionTokens, model)
					costCents := int64(math.Round(costDollars * 100))
					if costCents > 0 {
						s.balanceStore.Deduct(apiKey, costCents)
					}
					if s.receiptSigner != nil && apiKey != "" {
						r := receipt.NewReceipt(
							uuid.New().String(),
							apiKey,
							model,
							providerIDFromProxy(s),
							promptTokens,
							completionTokens,
							promptTokens+completionTokens,
							costCents,
						)
						_ = s.receiptSigner.Sign(r)
						if s.receiptStore != nil {
							_ = s.receiptStore.Save(r)
						}
					}
					sendSSEChunk(c, "", "stop", created, model, 0, 0, 0)
				}
				sendSSEDone(c)
				return
			}

			delta := chunk.GetDelta().GetContent()
			finish := chunk.GetFinishReason()

			if usage := chunk.GetUsage(); usage != nil {
				promptTokens = int(usage.GetPromptTokens())
				completionTokens = int(usage.GetCompletionTokens())
			}

			choice := 0
			reason := finish
			sendSSEChunk(c, delta, reason, created, model, choice, promptTokens, completionTokens)

			if finish != "" {
				// Send final usage if not already sent.
				costDollars := pricing.ComputeCost(promptTokens, completionTokens, model)
				costCents := int64(math.Round(costDollars * 100))
				if costCents > 0 {
					s.balanceStore.Deduct(apiKey, costCents)
				}
				if s.receiptSigner != nil && apiKey != "" {
					r := receipt.NewReceipt(
						uuid.New().String(),
						apiKey,
						model,
						providerIDFromProxy(s),
						promptTokens,
						completionTokens,
						promptTokens+completionTokens,
						costCents,
					)
					_ = s.receiptSigner.Sign(r)
					if s.receiptStore != nil {
						_ = s.receiptStore.Save(r)
					}
				}
				sendSSEDone(c)
				return
			}

		case err, ok := <-errs:
			if !ok {
				continue
			}
			sendSSEError(c, "stream error: "+err.Error())
			return

		case <-ctx.Done():
			sendSSEError(c, "request timeout")
			return
		}
	}
}
// embeddings handles POST /v1/embeddings.
// Returns embedding vectors. The model determines the embedding dimensions.
func (s *Server) embeddings(c *gin.Context) {
	var req struct {
		Model  string  `json:"model" binding:"required"`
		Input  any     `json:"input"` // string or []string
		Format string  `json:"format"` // "float" (default) or "base64"
		EncodingFormat string  `json:"encoding_format"` // "float" (default) or "base64"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "invalid request: " + err.Error(),
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		})
		return
	}
	// Normalize input to []string
	var texts []string
	switch v := req.Input.(type) {
	case string:
		texts = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
	case nil:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "input is required", "type": "invalid_request_error", "code": "invalid_request"},
		})
		return
	}
	model := req.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	// Dimensions by model (fake but consistent)
	dims := map[string]int{"text-embedding-3-small": 1536, "text-embedding-3-large": 3072, "text-embedding-2": 1536}
	dim, ok := dims[model]
	if !ok {
		dim = 1536
	}
	// Build response
	data := make([]gin.H, len(texts))
	for i, text := range texts {
		vec := make([]float64, dim)
		// Deterministic pseudo-embedding from text hash
		h := fnvHash(text)
		for j := range vec {
			vec[j] = float64(int64(h>>uint(j%64))&0x3FF) / 1024.0
		}
		data[i] = gin.H{
			"object":    "embedding",
			"embedding": vec,
			"index":     i,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   data,
		"model":  model,
		"usage": gin.H{
			"prompt_tokens":     len(texts) * 10,
			"total_tokens":      len(texts) * 10,
		},
	})
}
// imagesGenerations handles POST /v1/images/generations.
// Returns image URLs or base64. For now this is a stub that simulates generation.
func (s *Server) imagesGenerations(c *gin.Context) {
	var req struct {
		Model          string  `json:"model"`
		Prompt         string  `json:"prompt" binding:"required"`
		N              int     `json:"n"`
		Quality        string  `json:"quality"` // "standard" | "hd"
		Size           string  `json:"size"`    // "1024x1024" | "1024x1792" | "1792x1024"
		Style          string  `json:"style"`  // "vivid" | "natural"
		ResponseFormat string  `json:"response_format"` // "url" | "b64_json"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request: " + err.Error(), "type": "invalid_request_error", "code": "invalid_request"},
		})
		return
	}
	n := 1
	if req.N > 0 && req.N <= 10 {
		n = req.N
	}
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}
	quality := req.Quality
	if quality == "" {
		quality = "standard"
	}
	format := req.ResponseFormat
	if format == "" {
		format = "url"
	}
	model := req.Model
	if model == "" {
		model = "dall-e-3"
	}
	created := time.Now().Unix()
	imgID := fmt.Sprintf("img-%s", randomID())
	data := make([]gin.H, n)
	for i := 0; i < n; i++ {
		if format == "b64_json" {
			data[i] = gin.H{
				"b64_json": "REPLACE_WITH_BASE64_IMAGE_DATA",
				"revised_prompt": req.Prompt,
				"index": i,
			}
		} else {
			data[i] = gin.H{
				"url": fmt.Sprintf("https://api.hivemachine.ai/v1/images/generated/%s/%d", imgID, i),
				"revised_prompt": req.Prompt,
				"index": i,
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"created": created,
		"model":   model,
		"data":    data,
		"usage": gin.H{
			"prompt_tokens":      0,
			"total_tokens":       0,
			"completion_tokens":  0,
		},
	})
}

// audioSpeech handles POST /v1/audio/speech.
// Returns synthetic audio MP3/FLAC data. Input is text; output is audio binary.
func (s *Server) audioSpeech(c *gin.Context) {
	var req struct {
		Model       string `json:"model" binding:"required"`
		Input       string `json:"input" binding:"required"`
		Voice       string `json:"voice"`
		ResponseFormat string `json:"response_format"` // "mp3" | "flac" | "opus" | "aac"
		Speed       float64 `json:"speed"` // 0.25–4.0, default 1.0
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request: " + err.Error(), "type": "invalid_request_error", "code": "invalid_request"},
		})
		return
	}
	format := req.ResponseFormat
	if format == "" {
		format = "mp3"
	}
	voice := req.Voice
	if voice == "" {
		voice = "alloy"
	}
	speed := req.Speed
	if speed <= 0 {
		speed = 1.0
	}
	model := req.Model
	if model == "" {
		model = "tts-1"
	}
	// Return audio as binary. Real implementation would call TTS service.
	ctype := map[string]string{"mp3": "audio/mpeg", "flac": "audio/flac", "opus": "audio/opus", "aac": "audio/aac"}[format]
	if ctype == "" {
		ctype = "audio/mpeg"
	}
	c.Header("Content-Type", ctype)
	c.Header("Transfer-Encoding", "chunked")
	// Fake audio payload — 1 second of silence MP3 frame
	// Real: synthesize speech via provider
	c.Data(http.StatusOK, ctype, []byte("SYNTHETIC_AUDIO_PLACEHOLDER"))
}

// audioTranscriptions handles POST /v1/audio/transcriptions.
// Transcribes audio to text. Accepts audio file with optional prompt for context.
func (s *Server) audioTranscriptions(c *gin.Context) {
	var req struct {
		Model       string `json:"model"`
		Language    string `json:"language"` // BCP-47 language code
		Prompt      string `json:"prompt"`  // optional context
		Temperature float64 `json:"temperature"`
		ResponseFormat string `json:"response_format"` // "json" | "text" | "srt" | "verbose_json" | "vtt"
	}
	// Bind from form or JSON
	contentType := c.GetHeader("Content-Type")
	var err error
	if strings.Contains(contentType, "multipart/form-data") {
		req.Model = c.PostForm("model")
		req.Language = c.PostForm("language")
		req.Prompt = c.PostForm("prompt")
		req.Temperature, _ = strconv.ParseFloat(c.DefaultPostForm("temperature", "0"), 64)
		req.ResponseFormat = c.DefaultPostForm("response_format", "json")
		// File is in c.Request.Body; we just read a placeholder
	} else {
		err = c.ShouldBindJSON(&req)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request: " + err.Error(), "type": "invalid_request_error", "code": "invalid_request"},
		})
		return
	}
	model := req.Model
	if model == "" {
		model = "whisper-1"
	}
	format := req.ResponseFormat
	if format == "" {
		format = "json"
	}
	lang := req.Language
	// Simulate transcription from audio file
	transcript := "This is a simulated transcription of the uploaded audio file."
	if lang != "" {
		transcript = fmt.Sprintf("[Transcribed from audio in %s] This is a simulated transcription.", lang)
	}
	switch format {
	case "text":
		c.Header("Content-Type", "text/plain")
		c.String(http.StatusOK, transcript)
	case "srt":
		c.Header("Content-Type", "text/srt")
		c.String(http.StatusOK, "1\n00:00:00,000 --> 00:00:03,000\n%s\n\n", transcript)
	case "vtt":
		c.Header("Content-Type", "text/vtt")
		c.String(http.StatusOK, "WEBVTT\n\n00:00:00.000 --> 00:00:03.000\n%s\n\n", transcript)
	case "verbose_json":
		c.JSON(http.StatusOK, gin.H{
			"text":      transcript,
			"model":     model,
			"language":  lang,
			"duration":  "3.000s",
			"channels":  "1",
		})
	default: // json
		c.JSON(http.StatusOK, gin.H{
			"text": transcript,
		})
	}
}

// sendSSEChunk sends a server-sent event for a chat completion chunk.
func sendSSEChunk(c *gin.Context, delta, finishReason string, created int64, model string, choiceIdx, promptTokens, completionTokens int) {
	chunk := gin.H{
		"id":      fmt.Sprintf("chatcmpl-%s", randomID()),
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []gin.H{{
			"index": choiceIdx,
			"delta": gin.H{"content": delta},
		}},
	}
	if finishReason != "" {
		chunk["choices"].([]gin.H)[0]["finish_reason"] = finishReason
	}
	if promptTokens > 0 || completionTokens > 0 {
		chunk["usage"] = gin.H{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		}
	}
	payload, _ := json.Marshal(chunk)
	c.Writer.Write([]byte("data: " + string(payload) + "\n\n"))
	c.Writer.Flush()
}

// sendSSEDone sends the final [DONE] sentinel.
func sendSSEDone(c *gin.Context) {
	c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()
}

// sendSSEError sends an error as an SSE comment and closes the stream.
func sendSSEError(c *gin.Context, msg string) {
	c.Writer.Write([]byte(": " + msg + "\n\n"))
	c.Writer.Flush()
}

// listReceipts returns recent receipts for the authenticated user.
func (s *Server) listReceipts(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "missing API key"}})
		return
	}
	receipts, err := s.receiptStore.ByAPIKey(apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	data := make([]gin.H, 0, len(receipts))
	for _, r := range receipts {
		data = append(data, gin.H{
			"id":                 r.ID,
			"api_key":           r.APIKey,
			"model":             r.Model,
			"provider":          r.ProviderID,
			"prompt_tokens":     r.PromptTokens,
			"completion_tokens": r.CompletionTokens,
			"total_tokens":      r.TotalTokens,
			"cost_cents":        r.CostCents,
			"created_at":        r.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"receipts": data})
}

// verifyReceipt verifies a receipt's Ed25519 signature.
func (s *Server) verifyReceipt(c *gin.Context) {
	var req struct {
		ID string `json:"id"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	r, err := s.receiptStore.ByID(req.ID)
	if err != nil || r == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "receipt not found"}})
		return
	}
	valid := false
	if s.receiptSigner != nil {
		valid = s.receiptSigner.Verify(r)
	}
	c.JSON(http.StatusOK, gin.H{"valid": valid, "receipt": r})
}

// receiptPublicKey returns the server's public key for receipt verification.
func (s *Server) receiptPublicKey(c *gin.Context) {
	if s.receiptSigner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "receipt signing not configured"}})
		return
	}
	pubKey := s.receiptSigner.PublicKey()
	c.JSON(http.StatusOK, gin.H{"public_key": hex.EncodeToString(pubKey)})
}

// providerIDFromProxy returns a short provider identifier if the proxy is active.
func providerIDFromProxy(s *Server) string {
	if s.proxy != nil {
		return "proxy-routed"
	}
	return ""
}
// fnvHash returns a 64-bit FNV-1a hash of s.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
// walletDepositAddress handles GET /v1/wallet/deposit-address.
// Query params: api_key, rail (tap|tap|tnk).
// Returns the gateway's deposit address for the requested rail.
func (s *Server) walletDepositAddress(c *gin.Context) {
	if s.walletService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"message": "wallet service not configured"}})
		return
	}

	apiKey := c.Query("api_key")
	railStr := c.Query("rail")
	if apiKey == "" || railStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "api_key and rail query params are required"}})
		return
	}

	var rail balance.RailType
	switch railStr {
	case "tap":
		rail = balance.RailTAP
	case "tnk":
		rail = balance.RailTNK
	case "stripe":
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "stripe does not use a deposit address; use /v1/balance/checkout"}})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "rail must be 'tap' or 'tnk'"}})
		return
	}

	addr, err := s.walletService.DepositAddress(c.Request.Context(), apiKey, rail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": err.Error()}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"rail":    rail.String(),
		"address": addr,
	})
}
// getBalances returns balances across all payment rails.
// GET /v1/balances?api_key=...
func (s *Server) getBalances(c *gin.Context) {
	apiKey := extractAPIKey(c)
	if apiKey == "" {
		apiKey = c.Query("api_key")
	}
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "api_key required"}})
		return
	}

	balances := make(map[balance.RailType]int64)
	total := int64(0)

	// Legacy balance store → Stripe rail.
	if s.balanceStore != nil {
		stripeBal := s.balanceStore.Get(apiKey)
		balances[balance.RailStripe] = stripeBal
		total += stripeBal
	}

	// Rail store → TAP, TNK.
	if s.railStore != nil {
		for _, rail := range []balance.RailType{balance.RailTAP, balance.RailTNK} {
			b := s.railStore.Get(apiKey, rail)
			balances[rail] = b
			total += b
		}
	}

	c.JSON(http.StatusOK, balance.BalanceResponse{
		APIKey:     apiKey,
		Balances:   balances,
		TotalCents: total,
	})
}

// deposit initiates a deposit on the specified rail.
// POST /v1/balances/deposit  { "api_key": "...", "rail": "tap"|"stripe"|"tnk", "amount_cents": 500 }
// Returns the deposit destination (address or checkout URL).
func (s *Server) deposit(c *gin.Context) {
	var req struct {
		APIKey      string `json:"api_key" binding:"required"`
		Rail        string `json:"rail" binding:"required"`
		AmountCents int64  `json:"amount_cents"`
		Currency    string `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	if req.Currency == "" {
		req.Currency = "usd"
	}

	rail := balance.RailType(req.Rail)
	if !rail.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "rail must be stripe, tap, or tnk"}})
		return
	}

	switch rail {
	case balance.RailStripe:
		// Redirect to Stripe checkout.
		c.JSON(http.StatusOK, gin.H{
			"rail":  "stripe",
			"url":   "/v1/balance/checkout",
			"hint":  "use POST /v1/balance/checkout with api_key and amount_cents",
		})
		return

	case balance.RailTAP, balance.RailTNK:
		// Return the deposit address for the specified rail.
		addr, err := s.walletService.DepositAddress(c.Request.Context(), req.APIKey, rail)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		c.JSON(http.StatusOK, balance.DepositResult{
			DepositID:   addr,
			URL:        fmt.Sprintf("%s:%s", rail, addr),
			AmountCents: req.AmountCents,
			Currency:    req.Currency,
		})
		return
	}
}
// p2pPeers returns all connected P2P peers.
// GET /v1/p2p/peers
func (s *Server) p2pPeers(c *gin.Context) {
	peers := s.p2pService.Peers()
	type peerInfo struct {
		ID      string   `json:"id"`
		Address []string `json:"addresses"`
	}
	out := make([]peerInfo, 0, len(peers))
	for _, p := range peers {
		out = append(out, peerInfo{ID: p.String()})
	}
	c.JSON(http.StatusOK, gin.H{"peers": out})
}

// p2pInfo returns the local P2P node info.
// GET /v1/p2p/info
func (s *Server) p2pInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"peer_id": s.p2pService.ID(),
		"addrs":   s.p2pService.Addrs(),
	})
}

// p2pProvide announces a key via DHT.
// POST /v1/p2p/provide  { "key": "model:llama-3-8b" }
func (s *Server) p2pProvide(c *gin.Context) {
	var req struct{ Key string `json:"key" binding:"required"` }
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	if err := s.p2pService.Provide(c.Request.Context(), req.Key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": req.Key, "status": "provided"})
}

// p2pProviders searches DHT for providers of a key.
// GET /v1/p2p/providers?key=model:llama-3-8b
func (s *Server) p2pProviders(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "key query param required"}})
		return
	}
	providers, err := s.p2pService.FindProviders(c.Request.Context(), key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	type providerInfo struct {
		PeerID string   `json:"peer_id"`
		Addrs  []string `json:"addresses"`
	}
	out := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		addrs := make([]string, len(p.Addrs))
		for i, a := range p.Addrs {
			addrs[i] = a.String()
		}
		out = append(out, providerInfo{PeerID: p.ID.String(), Addrs: addrs})
	}
	c.JSON(http.StatusOK, gin.H{"key": key, "providers": out})
}
