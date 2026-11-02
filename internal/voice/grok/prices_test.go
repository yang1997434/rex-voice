package grok

import (
	"math"
	"testing"
)

func TestCost(t *testing.T) {
	// 90 s at $0.08/min plus 3 text items at $0.004.
	if usd, ok := cost(testModel, 90, 3); !ok || math.Abs(usd-(0.12+0.012)) > 1e-12 {
		t.Errorf("cost = %v, %v", usd, ok)
	}
	if usd, ok := cost("grok-unknown", 90, 3); ok || usd != 0 {
		t.Errorf("unpriced model: cost = %v, %v", usd, ok)
	}
}
