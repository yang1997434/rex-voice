package qwen

import (
	"math"
	"testing"
)

func TestCost(t *testing.T) {
	// 1M of each at 6 + 1.5 + 12 + 4.5 = 24 CNY = 24 / 7.2 USD.
	u := usage{Model: testModel, AudioIn: 1e6, TextIn: 1e6, AudioOut: 1e6, TextOut: 1e6}
	if usd, ok := cost(u); !ok || math.Abs(usd-24/7.2) > 1e-9 {
		t.Errorf("cost = %v, %v", usd, ok)
	}
	u.Model = "qwen-unknown"
	if usd, ok := cost(u); ok || usd != 0 {
		t.Errorf("unpriced model: cost = %v, %v", usd, ok)
	}
}
