package gemini

import (
	"math"
	"testing"
)

func TestCost(t *testing.T) {
	// 1M of each at 3 + 0.75 + 12 + 4.5 = 20.25 USD.
	u := usage{Model: testModel, AudioIn: 1e6, TextIn: 1e6, AudioOut: 1e6, TextOut: 1e6}
	if usd, ok := cost(u); !ok || math.Abs(usd-20.25) > 1e-9 {
		t.Errorf("cost = %v, %v", usd, ok)
	}
	u.Model = "gemini-unknown"
	if usd, ok := cost(u); ok || usd != 0 {
		t.Errorf("unpriced model: cost = %v, %v", usd, ok)
	}
}
