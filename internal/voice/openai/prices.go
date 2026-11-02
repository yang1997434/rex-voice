package openai

// USD per 1M tokens. 11-01 方案里的价，上线前对照官网核。
type price struct{ AudioIn, AudioOut, TextIn, TextOut float64 }

var prices = map[string]price{
	"gpt-realtime-2.1":      {AudioIn: 32, AudioOut: 64, TextIn: 4, TextOut: 16},
	"gpt-realtime-2.1-mini": {AudioIn: 10, AudioOut: 20, TextIn: 0.6, TextOut: 2.4},
}

const cachedFactor = 0.1 // cached input is billed at 1/10 of the input price

// cost returns the estimated USD of one response, and false when the model has no price entry.
func cost(u usage) (float64, bool) {
	p, ok := prices[u.Model]
	if !ok {
		return 0, false
	}
	cachedText := u.Cached - u.CachedAudio
	usd := float64(u.AudioIn-u.CachedAudio)*p.AudioIn + float64(u.CachedAudio)*p.AudioIn*cachedFactor +
		float64(u.TextIn-cachedText)*p.TextIn + float64(cachedText)*p.TextIn*cachedFactor +
		float64(u.AudioOut)*p.AudioOut + float64(u.TextOut)*p.TextOut
	return usd / 1e6, true
}
