package pricing

import (
	"testing"
	"time"
)

func TestQuota_SetAndCheck(t *testing.T) {
	s := NewInMemoryQuotaStore()
	s.Set("key1", 1000, 24*time.Hour)

	allowed, remaining := s.Check("key1", 500)
	if !allowed {
		t.Error("should be allowed")
	}
	if remaining != 500 {
		t.Errorf("remaining = %d, want 500", remaining)
	}
}

func TestQuota_Exceeded(t *testing.T) {
	s := NewInMemoryQuotaStore()
	s.Set("key1", 100, 24*time.Hour)

	allowed, _ := s.Check("key1", 150)
	if allowed {
		t.Error("should be rejected: exceeds quota")
	}
}

func TestQuota_Deduct(t *testing.T) {
	s := NewInMemoryQuotaStore()
	s.Set("key1", 1000, 24*time.Hour)

	s.Deduct("key1", 300)

	_, remaining := s.Check("key1", 100)
	if remaining != 600 {
		t.Errorf("remaining after deduct = %d, want 600", remaining)
	}
}

func TestQuota_Reset(t *testing.T) {
	s := NewInMemoryQuotaStore()
	s.Set("key1", 100, 1*time.Millisecond)

	s.Deduct("key1", 50)

	// Wait for period to expire.
	time.Sleep(5 * time.Millisecond)

	remaining := s.Remaining("key1")
	if remaining != 100 {
		t.Errorf("after reset: remaining = %d, want 100", remaining)
	}
}

func TestQuota_NoQuotaConfigured(t *testing.T) {
	s := NewInMemoryQuotaStore()

	// No quota set → allow.
	allowed, remaining := s.Check("unknown-key", 99999)
	if !allowed {
		t.Error("should allow when no quota is configured")
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

func TestQuota_Get(t *testing.T) {
	s := NewInMemoryQuotaStore()
	s.Set("key1", 5000, 24*time.Hour)

	q := s.Get("key1")
	if q.Limit != 5000 || q.Remaining != 5000 {
		t.Errorf("Get = %+v, want limit=5000 remaining=5000", q)
	}
}
