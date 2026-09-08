package dispute

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler serves dispute HTTP endpoints.
type Handler struct {
	store    DisputeStore
	apiKeyFn func(*gin.Context) string
}

// NewHandler creates a dispute handler.
func NewHandler(store DisputeStore, apiKeyFn func(*gin.Context) string) *Handler {
	return &Handler{store: store, apiKeyFn: apiKeyFn}
}

// OpenDispute handles POST /v1/disputes.
func (h *Handler) OpenDispute(c *gin.Context) {
	var req OpenDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	apiKey := h.apiKeyFn(c)
	now := time.Now()
	d := &Dispute{
		ID:          "dp_" + uuid.New().String()[:8],
		APIKey:      apiKey,
		ReceiptID:   req.ReceiptID,
		Reason:      req.Reason,
		State:       DisputeStateOpen,
		AmountCents: req.AmountCents,
		CreatedAt:   now,
		UpdatedAt:   now,
		Evidence:    req.Evidence,
	}

	if err := h.store.Open(d); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to open dispute"}})
		return
	}

	c.JSON(http.StatusCreated, toResponse(d))
}

// GetDispute handles GET /v1/disputes/:id.
func (h *Handler) GetDispute(c *gin.Context) {
	id := c.Param("id")
	apiKey := h.apiKeyFn(c)

	d, err := h.store.ByID(id)
	if err == ErrDisputeNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "dispute not found"}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get dispute"}})
		return
	}

	// Users can only see their own disputes.
	if d.APIKey != apiKey {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "dispute not found"}})
		return
	}

	c.JSON(http.StatusOK, toResponse(d))
}

// ListDisputes handles GET /v1/disputes.
func (h *Handler) ListDisputes(c *gin.Context) {
	apiKey := h.apiKeyFn(c)

	disputes, err := h.store.ByAPIKey(apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to list disputes"}})
		return
	}

	out := make([]map[string]interface{}, len(disputes))
	for i, d := range disputes {
		out[i] = toResponse(d)
	}
	c.JSON(http.StatusOK, gin.H{"disputes": out})
}

// ResolveDispute handles POST /v1/disputes/:id/resolve.
func (h *Handler) ResolveDispute(c *gin.Context) {
	id := c.Param("id")
	var req ResolveDisputeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	_, err := h.store.ByID(id)
	if err == ErrDisputeNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "dispute not found"}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to get dispute"}})
		return
	}

	var nextState DisputeState
	switch req.Action {
	case "refund":
		nextState = DisputeStateResolved
	case "reject":
		nextState = DisputeStateRejected
	}

	if err := h.store.SetResolution(id, req.Resolution, nextState); err != nil {
		if err == ErrInvalidTransition {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "cannot resolve dispute in current state"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "failed to resolve dispute"}})
		return
	}

	d, _ := h.store.ByID(id)
	c.JSON(http.StatusOK, toResponse(d))
}

// toResponse converts a Dispute to a JSON-safe map.
func toResponse(d *Dispute) map[string]interface{} {
	m := map[string]interface{}{
		"id":           d.ID,
		"receipt_id":   d.ReceiptID,
		"reason":       d.Reason,
		"state":        d.State.String(),
		"amount_cents": d.AmountCents,
		"created_at":   d.CreatedAt.Format(time.RFC3339),
		"updated_at":   d.UpdatedAt.Format(time.RFC3339),
	}
	if d.ResolvedAt != nil {
		m["resolved_at"] = d.ResolvedAt.Format(time.RFC3339)
	}
	if d.Resolution != "" {
		m["resolution"] = d.Resolution
	}
	if len(d.Evidence) > 0 {
		m["evidence"] = d.Evidence
	}
	return m
}
