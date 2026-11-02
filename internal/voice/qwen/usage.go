package qwen

import (
	"encoding/json"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// usage is one response's tokens plus Model for prices.go and the log line.
type usage struct {
	Input, AudioIn, TextIn, Output, AudioOut, TextOut int
	Model                                             string
	USD                                               float64 // estimate from prices.go; 0 when the model is not priced
}

// rawUsage is response.done's response.usage (11-01 实测：字段名是 *_tokens_details，带 s).
type rawUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputDetails struct {
		Audio int `json:"audio_tokens"`
		Text  int `json:"text_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		Audio int `json:"audio_tokens"`
		Text  int `json:"text_tokens"`
	} `json:"output_tokens_details"`
}

func (r rawUsage) toUsage(model string) usage {
	u := usage{Input: r.InputTokens, AudioIn: r.InputDetails.Audio, TextIn: r.InputDetails.Text,
		Output: r.OutputTokens, AudioOut: r.OutputDetails.Audio, TextOut: r.OutputDetails.Text, Model: model}
	u.USD, _ = cost(u)
	return u
}

func (u usage) public() voice.Usage {
	_, priced := prices[u.Model]
	return voice.Usage{Input: u.Input, AudioIn: u.AudioIn, TextIn: u.TextIn,
		Output: u.Output, AudioOut: u.AudioOut, TextOut: u.TextOut, USD: u.USD, Unpriced: !priced}
}

// usageLine is the openai provider's usage.log line format; Qwen reports no cache hits, so those stay 0.
type usageLine struct {
	Time        string  `json:"time"`
	Model       string  `json:"model"`
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

func (s *Session) writeUsage(u usage) {
	if s.cfg.UsageLog == nil {
		return
	}
	_, priced := prices[u.Model]
	b, _ := json.Marshal(usageLine{Time: time.Now().Format(time.RFC3339), Model: u.Model, Input: u.Input,
		AudioIn: u.AudioIn, TextIn: u.TextIn, Output: u.Output, AudioOut: u.AudioOut, TextOut: u.TextOut, USD: u.USD, Unpriced: !priced})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("qwen: usage log write failed", "err", err)
	}
}
