package gemini

import (
	"encoding/json"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// usage is one turn's tokens plus Model for prices.go and the log line.
type usage struct {
	Input, AudioIn, TextIn, Output, AudioOut, TextOut int
	Model                                             string
	USD                                               float64 // estimate from prices.go; 0 when the model is not priced
}

type modalityCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

// rawUsage is usageMetadata (11-01 实测 shape, on the turnComplete message).
type rawUsage struct {
	PromptTokenCount      int             `json:"promptTokenCount"`
	ResponseTokenCount    int             `json:"responseTokenCount"`
	ThoughtsTokenCount    int             `json:"thoughtsTokenCount"`
	PromptTokensDetails   []modalityCount `json:"promptTokensDetails"`
	ResponseTokensDetails []modalityCount `json:"responseTokensDetails"`
}

func split(d []modalityCount) (audio, text int) {
	for _, m := range d {
		switch m.Modality {
		case "AUDIO":
			audio += m.TokenCount
		case "TEXT":
			text += m.TokenCount
		}
	}
	return
}

// toUsage counts thinking tokens as text output: Gemini bills them at the output price and
// leaves them out of responseTokenCount.
func (r rawUsage) toUsage(model string) usage {
	u := usage{Input: r.PromptTokenCount, Output: r.ResponseTokenCount + r.ThoughtsTokenCount, Model: model}
	u.AudioIn, u.TextIn = split(r.PromptTokensDetails)
	u.AudioOut, u.TextOut = split(r.ResponseTokensDetails)
	u.TextOut += r.ThoughtsTokenCount
	u.USD, _ = cost(u)
	return u
}

func (u usage) public() voice.Usage {
	_, priced := prices[u.Model]
	return voice.Usage{Input: u.Input, AudioIn: u.AudioIn, TextIn: u.TextIn,
		Output: u.Output, AudioOut: u.AudioOut, TextOut: u.TextOut, USD: u.USD, Unpriced: !priced}
}

// usageLine is the openai provider's usage.log line format; cache hits are not counted, so those stay 0.
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
	b, _ := json.Marshal(usageLine{Time: time.Now().Format(time.RFC3339), Model: "gemini/" + u.Model, Input: u.Input,
		AudioIn: u.AudioIn, TextIn: u.TextIn, Output: u.Output, AudioOut: u.AudioOut, TextOut: u.TextOut, USD: u.USD, Unpriced: !priced})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("gemini: usage log write failed", "err", err)
	}
}
