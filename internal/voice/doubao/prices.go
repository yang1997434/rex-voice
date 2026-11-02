package doubao

import (
	"encoding/json"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// 元/百万 token，豆包语音计费页（2026-11-01 调研）；是否覆盖 Realtime 3.0 未核实。
var price = struct{ AudioIn, TextIn, Cached, AudioOut, TextOut float64 }{80, 10, 5, 300, 80}

const cnyPerUSD = 7.2

// cost is the USD estimate of one response. Cache hits are assumed spread like the input.
func cost(u voice.Usage) float64 {
	cachedAudio := 0
	if u.Input > 0 {
		cachedAudio = u.Cached * u.AudioIn / u.Input
	}
	cachedText := u.Cached - cachedAudio
	yuan := float64(u.AudioIn-cachedAudio)*price.AudioIn + float64(u.TextIn-cachedText)*price.TextIn +
		float64(u.Cached)*price.Cached + float64(u.AudioOut)*price.AudioOut + float64(u.TextOut)*price.TextOut
	return yuan / 1e6 / cnyPerUSD
}

// rawUsage is response.done's response.usage. input_tokens / output_tokens / total_tokens are seen in a
// third-party live probe; the *_token_details names are guessed from OpenAI's and not verified.
type rawUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputDetails struct {
		Cached int `json:"cached_tokens"`
		Audio  int `json:"audio_tokens"`
		Text   int `json:"text_tokens"`
	} `json:"input_token_details"`
	OutputDetails struct {
		Audio int `json:"audio_tokens"`
		Text  int `json:"text_tokens"`
	} `json:"output_token_details"`
}

// toUsage prices a response only when the audio/text split is there: audio costs 8x text in and
// almost 4x out, so totals alone would be a guess. A missing usage is Unpriced too.
func (r *rawUsage) toUsage() voice.Usage {
	if r == nil {
		return voice.Usage{Unpriced: true}
	}
	d, o := r.InputDetails, r.OutputDetails
	u := voice.Usage{Input: r.InputTokens, Cached: d.Cached, AudioIn: d.Audio, TextIn: d.Text,
		Output: r.OutputTokens, AudioOut: o.Audio, TextOut: o.Text}
	if (u.Input > 0 && u.AudioIn+u.TextIn == 0) || (u.Output > 0 && u.AudioOut+u.TextOut == 0) {
		u.Unpriced = true
		return u
	}
	u.USD = cost(u)
	return u
}

type usageLine struct {
	Time     string  `json:"time"`
	Model    string  `json:"model"`
	Input    int     `json:"input"`
	Cached   int     `json:"cached"`
	AudioIn  int     `json:"audio_in"`
	TextIn   int     `json:"text_in"`
	Output   int     `json:"output"`
	AudioOut int     `json:"audio_out"`
	TextOut  int     `json:"text_out"`
	USD      float64 `json:"usd"`
	Unpriced bool    `json:"unpriced,omitempty"`
}

func (s *Session) writeUsage(u voice.Usage) {
	if s.cfg.UsageLog == nil {
		return
	}
	b, _ := json.Marshal(usageLine{time.Now().Format(time.RFC3339), "doubao/" + s.cfg.Model, u.Input, u.Cached,
		u.AudioIn, u.TextIn, u.Output, u.AudioOut, u.TextOut, u.USD, u.Unpriced})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("doubao: usage log write failed", "err", err)
	}
}
