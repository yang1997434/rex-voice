package call

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// callLine is one calls.log line (protocol section 3).
type callLine struct {
	Time     string  `json:"time"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Seconds  float64 `json:"seconds"`
	USD      float64 `json:"usd"`
}

// logCall appends the ending call to CallsLog; call it before the session is closed.
func (s *server) logCall(c *callState) {
	if s.cfg.CallsLog == nil {
		return
	}
	model := c.vc.Model
	if model == "" {
		model, _ = voice.Defaults(c.vc.Provider)
	}
	now := timeNow()
	b, _ := json.Marshal(callLine{Time: now.Format(time.RFC3339), Provider: string(c.vc.Provider), Model: model,
		Seconds: now.Sub(c.start).Seconds(), USD: c.costBase + c.sess.Cost()})
	if _, err := s.cfg.CallsLog.Write(append(b, '\n')); err != nil {
		s.log.Warn("calls log write failed", "err", err)
	}
}

type statsBucket struct {
	Calls   int     `json:"calls"`
	Seconds float64 `json:"seconds"`
	USD     float64 `json:"usd"`
}

func (b *statsBucket) add(l callLine) { b.Calls++; b.Seconds += l.Seconds; b.USD += l.USD }

// sendStats sums calls.log into today (local calendar day), the last 7 and 30 days, and per provider
// over 30 days. A missing file is all zeros; bad lines are skipped with one warning.
func (s *server) sendStats() {
	var today, week, month statsBucket
	byProvider := map[string]*statsBucket{}
	now := timeNow()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	bad := 0
	if f, ok := s.cfg.CallsLog.(interface{ Name() string }); ok {
		r, err := os.Open(f.Name())
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			s.log.Warn("calls log not readable, stats are zero", "err", err)
		default:
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				var l callLine
				if json.Unmarshal(sc.Bytes(), &l) != nil {
					bad++
					continue
				}
				t, err := time.Parse(time.RFC3339, l.Time)
				if err != nil {
					bad++
					continue
				}
				if t.Before(now.AddDate(0, 0, -30)) {
					continue
				}
				month.add(l)
				if byProvider[l.Provider] == nil {
					byProvider[l.Provider] = &statsBucket{}
				}
				byProvider[l.Provider].add(l)
				if !t.Before(now.AddDate(0, 0, -7)) {
					week.add(l)
				}
				if !t.Before(midnight) {
					today.add(l)
				}
			}
			if err := sc.Err(); err != nil {
				s.log.Warn("calls log read failed, stats are partial", "err", err)
			}
			r.Close()
		}
	}
	if bad > 0 {
		s.log.Warn("calls log: bad lines skipped", "lines", bad)
	}
	s.sendCtl(map[string]any{"t": "stats", "today": today, "week": week, "month": month, "by_provider": byProvider})
}
