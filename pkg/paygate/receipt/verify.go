package receipt

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Handler exposes receipt verification as an HTTP endpoint.
type Handler struct {
	store  Store
	signer *Signer
}

// NewHandler creates a receipt verification handler.
func NewHandler(store Store, signer *Signer) *Handler {
	return &Handler{store: store, signer: signer}
}

// VerificationResult describes the outcome of receipt verification.
type VerificationResult struct {
	ReceiptID string   `json:"receipt_id"`
	Valid    bool     `json:"valid"`
	Message  string   `json:"message,omitempty"`
	Receipt  *Receipt `json:"receipt,omitempty"`
}

// Verify handles POST /v1/receipts/verify with JSON body {"receipt": {...}}.
func (h *Handler) Verify(c *gin.Context) {
	var req struct {
		Receipt *Receipt `json:"receipt"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.Receipt == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'receipt' field"})
		return
	}
	c.JSON(http.StatusOK, h.VerifyReceipt(req.Receipt))
}

// VerifyReceipt checks a receipt's signature and returns the result.
func (h *Handler) VerifyReceipt(r *Receipt) VerificationResult {
	if r.Signature == nil {
		return VerificationResult{ReceiptID: r.ID, Valid: false, Message: "missing signature"}
	}
	if !h.signer.Verify(r) {
		return VerificationResult{ReceiptID: r.ID, Valid: false, Message: "invalid signature"}
	}
	if h.store != nil {
		stored, err := h.store.ByID(r.ID)
		if err != nil || stored == nil {
			return VerificationResult{ReceiptID: r.ID, Valid: false, Message: "receipt not found in store"}
		}
	}
	return VerificationResult{ReceiptID: r.ID, Valid: true, Message: "signature valid", Receipt: r}
}

// receiptSummary is a public-safe receipt view (no private key material).
type receiptSummary struct {
	ID               string `json:"id"`
	Model            string `json:"model"`
	ProviderID       string `json:"provider_id"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens     int    `json:"total_tokens"`
	CostCents       int64  `json:"cost_cents"`
	CreatedAt       int64  `json:"created_at"`
	SignatureB64     string `json:"signature_b64,omitempty"`
}

// List handles GET /v1/receipts?api_key=...&limit=20.
func (h *Handler) List(c *gin.Context) {
	apiKey := strings.TrimSpace(c.Query("api_key"))
	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var receipts []*Receipt
	var err error
	if apiKey != "" {
		receipts, err = h.store.ByAPIKey(apiKey)
	} else {
		receipts, err = h.store.Recent(limit)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(receipts) > limit {
		receipts = receipts[:limit]
	}

	summaries := make([]receiptSummary, len(receipts))
	for i, r := range receipts {
		summaries[i] = receiptSummary{
			ID:               r.ID,
			Model:            r.Model,
			ProviderID:       r.ProviderID,
			PromptTokens:     r.PromptTokens,
			CompletionTokens:   r.CompletionTokens,
			TotalTokens:      r.TotalTokens,
			CostCents:        r.CostCents,
			CreatedAt:        r.CreatedAt,
		}
		if len(r.Signature) > 0 {
			summaries[i].SignatureB64 = base64.StdEncoding.EncodeToString(r.Signature)
		}
	}
	c.JSON(http.StatusOK, gin.H{"receipts": summaries})
}

// PublicKey handles GET /v1/receipts/public_key.
func (h *Handler) PublicKey(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"public_key": h.signer.PublicKeyB64()})
}

// MarshalJSON implements json.Marshaler for Receipt, encoding signature as base64.
func (r *Receipt) MarshalJSON() ([]byte, error) {
	type alias Receipt // avoid recursion
	aux := struct {
		*alias
		SignatureB64 string `json:"signature_b64,omitempty"`
	}{
		alias: (*alias)(r),
	}
	if len(r.Signature) > 0 {
		aux.SignatureB64 = base64.StdEncoding.EncodeToString(r.Signature)
	}
	return json.Marshal(aux)
}
