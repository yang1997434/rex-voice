package grok

// USD per minute of connection, model included (docs/research/2026-11-01-realtime-providers.md, xAI docs
// 2026-11-01); with server_vad xAI bills the whole connection, silence too. 上线前对照官网核。
var perMinute = map[string]float64{
	"grok-voice-3.0":    0.08,
	"grok-voice-latest": 0.08,
}

const perText = 0.004 // each client text conversation.item.create; function_call_output is free

// cost returns the estimated USD of one session, and false when the model has no price entry.
func cost(model string, seconds float64, texts int) (float64, bool) {
	p, ok := perMinute[model]
	if !ok {
		return 0, false
	}
	return seconds/60*p + float64(texts)*perText, true
}
