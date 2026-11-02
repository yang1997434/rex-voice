package gemini

// USD per 1M tokens, paid tier (https://ai.google.dev/gemini-api/docs/pricing ,
// 2026-11-01 调研 docs/research/2026-11-01-realtime-providers.md), check against the site before relying on it.
// The free tier is not deducted here.
type price struct{ AudioIn, AudioOut, TextIn, TextOut float64 }

var prices = map[string]price{
	"gemini-3.8-live": {AudioIn: 3, AudioOut: 12, TextIn: 0.75, TextOut: 4.5},
}

// cost returns the estimated USD of one turn, and false when the model has no price entry.
func cost(u usage) (float64, bool) {
	p, ok := prices[u.Model]
	if !ok {
		return 0, false
	}
	return (float64(u.AudioIn)*p.AudioIn + float64(u.TextIn)*p.TextIn + float64(u.AudioOut)*p.AudioOut + float64(u.TextOut)*p.TextOut) / 1e6, true
}
