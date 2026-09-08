package balance

import (
	"testing"
)

func TestBalance_SetAndGet(t *testing.T) {
	s := NewInMemoryStore()
	s.SetBalance("key1", 5000) // $50.00

	acc := s.Get("key1")
	if acc.BalanceCents != 5000 {
		t.Errorf("balance = %d, want 5000", acc.BalanceCents)
	}
}

func TestBalance_Deduct(t *testing.T) {
	s := NewInMemoryStore()
	s.SetBalance("key1", 1000)

	newBalance, err := s.Deduct("key1", 300)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if newBalance != 700 {
		t.Errorf("newBalance = %d, want 700", newBalance)
	}
}

func TestBalance_Deduct_Insufficient(t *testing.T) {
	s := NewInMemoryStore()
	s.SetBalance("key1", 100)

	_, err := s.Deduct("key1", 200)
	if err != ErrInsufficientBalance {
		t.Errorf("got err = %v, want ErrInsufficientBalance", err)
	}
}

func TestBalance_Deduct_UnknownKey(t *testing.T) {
	s := NewInMemoryStore()
	_, err := s.Deduct("unknown-key", 1)
	if err != ErrInsufficientBalance {
		t.Errorf("got err = %v, want ErrInsufficientBalance", err)
	}
}

func TestBalance_AddCredits(t *testing.T) {
	s := NewInMemoryStore()
	s.SetBalance("key1", 100)
	s.AddCredits("key1", 50)

	acc := s.Get("key1")
	if acc.BalanceCents != 150 {
		t.Errorf("balance = %d, want 150", acc.BalanceCents)
	}
}

func TestBalance_AddCredits_NewAccount(t *testing.T) {
	s := NewInMemoryStore()
	s.AddCredits("new-key", 500)

	acc := s.Get("new-key")
	if acc.BalanceCents != 500 {
		t.Errorf("balance = %d, want 500", acc.BalanceCents)
	}
}

func TestBalance_HasSufficient(t *testing.T) {
	s := NewInMemoryStore()
	s.SetBalance("key1", 1000)

	if !s.HasSufficientBalance("key1", 500) {
		t.Error("should have sufficient balance")
	}
	if s.HasSufficientBalance("key1", 1001) {
		t.Error("should not have sufficient balance")
	}
	if s.HasSufficientBalance("unknown", 1) {
		t.Error("unknown key should not have balance")
	}
}
