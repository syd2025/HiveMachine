package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// checkQuota is middleware that enforces per-API-key token quotas.
// It runs before /v1/* inference endpoints.
// If no quotaStore is configured, it is a no-op.
func (s *Server) checkQuota() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.quotaStore == nil {
			c.Next()
			return
		}

		apiKey := extractAPIKey(c)
		if apiKey == "" {
			c.Next()
			return
		}

		maxTokens := 1024
		if mt := extractMaxTokens(c); mt > 0 {
			maxTokens = mt
		}

		allowed, remaining := s.quotaStore.Check(apiKey, maxTokens)
		if !allowed {
			c.Abort()
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"message": "quota exceeded",
					"type":    "quota_exceeded",
				},
				"quota_remaining": remaining,
			})
			return
		}

		// Pre-deduct to reserve quota. Actual usage may be less.
		s.quotaStore.Deduct(apiKey, maxTokens)

		c.Next()
	}
}

// extractMaxTokens reads max_tokens from the request body without consuming it,
// so the handler can still bind it.
func extractMaxTokens(c *gin.Context) int {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return 0
	}
	// Restore the body so the handler can bind it.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var raw struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0
	}
	return raw.MaxTokens
}
