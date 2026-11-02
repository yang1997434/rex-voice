package openai

import (
	"encoding/json"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// usage is one response's tokens; it keeps CachedAudio and Model for prices.go and the log line.
type usage struct {
	Input, Cached, AudioIn, TextIn, Output, AudioOut, TextOut int
	CachedAudio                                               int // part of Cached that is audio
	Model                                                     string
	USD                                                       float64 // estimate from prices.go; 0 when the model is not priced
}

// rawUsage is response.done's response.usage.
type rawUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputDetails struct {
		Cached        int `json:"cached_tokens"`
		Audio         int `json:"audio_tokens"`
		Text          int `json:"text_tokens"`
		CachedDetails *struct {
			Audio int `json:"audio_tokens"`
		} `json:"cached_tokens_details"`
	} `json:"input_token_details"`
	OutputDetails struct {
		Audio int `json:"audio_tokens"`
		Text  int `json:"text_tokens"`
	} `json:"output_token_details"`
}

func (u usage) public() voice.Usage {
	_, priced := prices[u.Model]
	return voice.Usage{Input: u.Input, Cached: u.Cached, AudioIn: u.AudioIn, TextIn: u.TextIn,
		Output: u.Output, AudioOut: u.AudioOut, TextOut: u.TextOut, USD: u.USD, Unpriced: !priced}
}

func (r rawUsage) toUsage(model string) usage {
	d := r.InputDetails
	u := usage{Input: r.InputTokens, Cached: d.Cached, AudioIn: d.Audio, TextIn: d.Text,
		Output: r.OutputTokens, AudioOut: r.OutputDetails.Audio, TextOut: r.OutputDetails.Text, Model: model}
	switch {
	case d.CachedDetails != nil:
		u.CachedAudio = d.CachedDetails.Audio
	case u.Input > 0: // no split given: assume cache hits are spread like the input
		u.CachedAudio = u.Cached * u.AudioIn / u.Input
	}
	u.USD, _ = cost(u)
	return u
}

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
	b, _ := json.Marshal(usageLine{time.Now().Format(time.RFC3339), u.Model, u.Input, u.Cached, u.CachedAudio,
		u.AudioIn, u.TextIn, u.Output, u.AudioOut, u.TextOut, u.USD, !priced})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("openai: usage log write failed", "err", err)
	}
}
