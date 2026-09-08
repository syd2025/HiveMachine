package pricing

import (
	"testing"
)

func TestComputeCost_GPT4o(t *testing.T) {
	// 1000 prompt + 500 completion → 1000*0.005/1000 + 500*0.015/1000 = 0.005 + 0.0075 = 0.0125
	cost := ComputeCost(1000, 500, "gpt-4o")
	if cost != 0.0125 {
		t.Errorf("cost = %f, want 0.0125", cost)
	}
}

func TestComputeCost_UnknownModel(t *testing.T) {
	// Falls back to mayhem/default: prompt 0.001, completion 0.001
	cost := ComputeCost(1000, 500, "unknown-model")
	// 1500 * 0.001 / 1000 = 0.0015
	if cost != 0.0015 {
		t.Errorf("cost = %f, want 0.0015", cost)
	}
}

func TestComputeCost_LlamaFree(t *testing.T) {
	// 100 prompt + 50 completion → both 0.0002/1K
	// 150 * 0.0002/1000 = 0.00003
	cost := ComputeCost(100, 50, "llama-3")
	if cost != 0.00003 {
		t.Errorf("cost = %f, want 0.00003", cost)
	}
}

func TestComputeCostWithPrice_Custom(t *testing.T) {
	p := ModelPrice{
		Model:               "custom",
		PromptPricePer1K:      0.01,
		CompletionPricePer1K: 0.03,
	}
	cost := ComputeCostWithPrice(1000, 1000, p)
	// 1 * 0.01 + 1 * 0.03 = 0.04
	if cost != 0.04 {
		t.Errorf("cost = %f, want 0.04", cost)
	}
}

func TestInMemoryStore_GetSet(t *testing.T) {
	s := NewInMemoryStore()
	p, ok := s.Get("gpt-4o")
	if !ok {
		t.Fatal("gpt-4o should exist in default prices")
	}
	if p.PromptPricePer1K != 0.005 {
		t.Errorf("prompt price = %f, want 0.005", p.PromptPricePer1K)
	}

	s.Set("custom-model", ModelPrice{Model: "custom-model", PromptPricePer1K: 0.01, CompletionPricePer1K: 0.02})
	p, ok = s.Get("custom-model")
	if !ok || p.CompletionPricePer1K != 0.02 {
		t.Errorf("custom model: got %+v", p)
	}
}
