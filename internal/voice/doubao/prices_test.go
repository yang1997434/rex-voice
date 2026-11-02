package doubao

import (
	"math"
	"testing"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func TestCost(t *testing.T) {
	// 1M of each kind, half the input cached: 0.5*80 + 0.5*10 + 1M*5 + 300 + 80 = 430 元.
	u := voice.Usage{Input: 2e6, Cached: 1e6, AudioIn: 1e6, TextIn: 1e6, AudioOut: 1e6, TextOut: 1e6}
	if got, want := cost(u), 430/7.2; math.Abs(got-want) > 1e-9 {
		t.Errorf("cost = %v, want %v", got, want)
	}
	if u := (*rawUsage)(nil).toUsage(); !u.Unpriced || u.USD != 0 {
		t.Errorf("missing usage = %+v", u)
	}
}
