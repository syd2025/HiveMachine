package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestExtractAPIKey(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer sk-test-key-123")

	key := ExtractAPIKey(c)
	if key != "sk-test-key-123" {
		t.Errorf("key = %q, want sk-test-key-123", key)
	}
}

func TestExtractAPIKey_Missing(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	if ExtractAPIKey(c) != "" {
		t.Error("expected empty key")
	}
}

func TestExtractAPIKey_WrongScheme(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Basic abc123")

	if ExtractAPIKey(c) != "" {
		t.Error("expected empty key for Basic auth")
	}
}

func TestInMemoryTracker_Add(t *testing.T) {
	tr := NewInMemoryTracker()
	tr.Add("key1", 10, 20)

	if total := tr.Total("key1"); total != 30 {
		t.Errorf("total = %d, want 30", total)
	}
}

func TestInMemoryTracker_MultipleAdds(t *testing.T) {
	tr := NewInMemoryTracker()
	tr.Add("key1", 10, 20)
	tr.Add("key1", 5, 15)

	if total := tr.Total("key1"); total != 50 {
		t.Errorf("total = %d, want 50", total)
	}
	prompt, comp := tr.Breakdown("key1")
	if prompt != 15 || comp != 35 {
		t.Errorf("prompt=%d comp=%d, want 15 and 35", prompt, comp)
	}
}

func TestInMemoryTracker_UnknownKey(t *testing.T) {
	tr := NewInMemoryTracker()
	if tr.Total("unknown") != 0 {
		t.Error("unknown key should return 0")
	}
}

func TestInMemoryTracker_MultipleKeys(t *testing.T) {
	tr := NewInMemoryTracker()
	tr.Add("key1", 10, 20)
	tr.Add("key2", 5, 5)

	if tr.Total("key1") != 30 || tr.Total("key2") != 10 {
		t.Error("separate keys should have separate totals")
	}
}

func TestHandler_RecordUsage_Noop(t *testing.T) {
	h := New()
	// Should not panic with nil UsageFunc.
	h.RecordUsage(context.Background(), Usage{APIKey: "k", TotalTokens: 100})
}

func TestHandler_RecordUsage_WithFunc(t *testing.T) {
	h := New()
	var recorded Usage
	h.UsageFunc = func(_ context.Context, u Usage) {
		recorded = u
	}
	h.RecordUsage(context.Background(), Usage{APIKey: "sk-key", PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15})
	if recorded.APIKey != "sk-key" || recorded.TotalTokens != 15 {
		t.Errorf("recorded = %+v", recorded)
	}
}

func TestExtractUsageFromResponse(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("X-Usage-Prompt-Tokens", "10")
	c.Request.Header.Set("X-Usage-Completion-Tokens", "20")
	c.Request.Header.Set("X-Usage-Total-Tokens", "30")

	p, comp, total := ExtractUsageFromResponse(c)
	if p != 10 || comp != 20 || total != 30 {
		t.Errorf("p=%d comp=%d total=%d, want 10,20,30", p, comp, total)
	}
}

func TestExtractUsageFromResponse_Missing(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	p, comp, total := ExtractUsageFromResponse(c)
	if p != 0 || comp != 0 || total != 0 {
		t.Errorf("all should be 0, got p=%d comp=%d total=%d", p, comp, total)
	}
}
