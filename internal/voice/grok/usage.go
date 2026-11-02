package grok

import (
	"encoding/json"
	"time"
)

// usageLine is the openai provider's usage.log line plus seconds. xAI bills per minute and reports no
// token split, so one line is written per session at Close; input / output are the summed response.done tokens.
type usageLine struct {
	Time        string  `json:"time"`
	Model       string  `json:"model"`
	Seconds     float64 `json:"seconds"`
	Input       int     `json:"input"`
	Cached      int     `json:"cached"`
	CachedAudio int     `json:"cached_audio"`
	AudioIn     int     `json:"audio_in"`
	TextIn      int     `json:"text_in"`
	Output      int     `json:"output"`
	AudioOut    int     `json:"audio_out"`
	TextOut     int     `json:"text_out"`
	USD         float64 `json:"usd"`
	Unpriced    bool    `json:"unpriced,omitempty"`
}

func (s *Session) writeUsage(seconds float64, in, out, texts int) {
	if s.cfg.UsageLog == nil {
		return
	}
	usd, priced := cost(s.cfg.Model, seconds, texts)
	b, _ := json.Marshal(usageLine{Time: time.Now().Format(time.RFC3339), Model: s.cfg.Model, Seconds: seconds,
		Input: in, Output: out, USD: usd, Unpriced: !priced})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("grok: usage log write failed", "err", err)
	}
}
