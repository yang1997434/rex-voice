package moss

import (
	"encoding/json"
	"time"
)

// usageLine is one line per session: MOSS-Live is free for now and its price list is unknown.
type usageLine struct {
	Time     string  `json:"time"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Seconds  float64 `json:"seconds"`
	Unpriced bool    `json:"unpriced"`
}

func (s *Session) writeUsage(seconds float64) {
	if s.cfg.UsageLog == nil {
		return
	}
	b, _ := json.Marshal(usageLine{Time: time.Now().Format(time.RFC3339), Provider: "moss", Model: s.cfg.Model,
		Seconds: seconds, Unpriced: true})
	if _, err := s.cfg.UsageLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("moss: usage log write failed", "err", err)
	}
}
