package call

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/relay"
)

// pinNow fixes timeNow for one test; call it before start so Run never sees the restore.
func pinNow(t *testing.T, at time.Time) {
	old := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = old })
}

// doneEv is an OpenAI response.done with text-only usage; gpt-realtime-2.1 bills $4 / $16 per 1M.
func doneEv(id string, in, out int) string {
	b, _ := json.Marshal(map[string]any{"type": "response.done", "response": map[string]any{"id": id, "usage": map[string]any{
		"input_tokens": in, "output_tokens": out,
		"input_token_details": map[string]any{"text_tokens": in}, "output_token_details": map[string]any{"text_tokens": out}}}})
	return string(b)
}

func usdOf(in, out int) float64 { return (float64(in)*4 + float64(out)*16) / 1e6 }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func callsLog(t *testing.T, seed string) (*os.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.log")
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, path
}

// Each response.done sends the call's running cost; an unchanged total is not resent.
func TestCostFrameAfterResponseDone(t *testing.T) {
	pinNow(t, time.Now()) // seconds stay put, so only usd can change the frame
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	ai.push(t, doneEv("r1", 1000, 200))
	m := ph.ctlUntil(t, "cost")
	if usd, _ := m["usd"].(float64); !near(usd, usdOf(1000, 200)) {
		t.Fatalf("cost = %v, want usd %v", m, usdOf(1000, 200))
	}
	if _, ok := m["seconds"].(float64); !ok {
		t.Fatalf("cost without seconds: %v", m)
	}
	ai.push(t, doneEv("r2", 0, 0)) // adds nothing
	ai.push(t, `{"type":"response.output_audio_transcript.done","transcript":"好"}`)
	for {
		typ, plain := ph.next(t)
		if typ != pair.TypeControl {
			continue
		}
		var c map[string]any
		json.Unmarshal(plain, &c)
		if c["t"] == "cost" {
			t.Fatalf("unchanged cost resent: %s", plain)
		}
		if c["t"] == "transcript" {
			break
		}
	}
}

// An ended call appends one calls.log line with its provider, model, length and cost.
func TestCallsLogLineAtEnd(t *testing.T) {
	f, path := callsLog(t, "")
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) { c.CallsLog = f })
	ph, ai := e.inCall(t)
	ai.push(t, doneEv("r1", 1000, 200))
	ph.ctlUntil(t, "cost")
	ph.ctl(t, map[string]any{"t": "call.end"})
	ph.ctlUntil(t, "hang_up") // written before hang_up goes out
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var l callLine
	if err := json.Unmarshal(b, &l); err != nil || bytes.Count(b, []byte("\n")) != 1 {
		t.Fatalf("calls.log %q: %v", b, err)
	}
	if _, err := time.Parse(time.RFC3339, l.Time); err != nil || l.Provider != "openai" || l.Model != "gpt-realtime-2.1" ||
		l.Seconds <= 0 || !near(l.USD, usdOf(1000, 200)) {
		t.Errorf("calls.log line = %s", b)
	}
}

// hello is followed by stats summed from calls.log: today, 7 and 30 days, per provider; bad lines are skipped with a warning.
func TestStatsAfterHello(t *testing.T) {
	at := time.Date(2026, 11, 4, 15, 0, 0, 0, time.Local)
	pinNow(t, at)
	line := func(ago time.Duration, provider string, secs, usd float64) string {
		b, _ := json.Marshal(callLine{Time: at.Add(-ago).Format(time.RFC3339), Provider: provider, Model: "m", Seconds: secs, USD: usd})
		return string(b) + "\n"
	}
	f, _ := callsLog(t, line(3*24*time.Hour, "grok", 60, 0.08)+"not json\n"+line(20*24*time.Hour, "qwen", 30, 0.01)+
		line(40*24*time.Hour, "qwen", 99, 9))
	logs := &syncBuf{}
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
		c.CallsLog = f
		c.Logger = slog.New(slog.NewTextHandler(logs, nil))
	})
	ph, _ := e.inCall(t)
	ph.ctl(t, map[string]any{"t": "call.end"})
	ph.ctlUntil(t, "hang_up")
	ph.ws.Close()

	ph2 := e.dialPhone(t)
	ph2.handshake(t)
	ph2.hello(t, "phone-1")
	ph2.expectStatus(t, "ended")
	m := ph2.ctlUntil(t, "stats")
	calls := func(path ...string) float64 {
		var v any = m
		for _, k := range path {
			v = v.(map[string]any)[k]
		}
		return v.(map[string]any)["calls"].(float64)
	}
	if calls("today") != 1 || calls("week") != 2 || calls("month") != 3 ||
		calls("by_provider", "openai") != 1 || calls("by_provider", "grok") != 1 || calls("by_provider", "qwen") != 1 {
		t.Fatalf("stats = %v", m)
	}
	if usd := m["month"].(map[string]any)["usd"].(float64); !near(usd, 0.09) {
		t.Errorf("month usd = %v, want 0.09", usd)
	}
	if !strings.Contains(logs.String(), "bad lines skipped") {
		t.Errorf("no warning for the bad line:\n%s", logs.String())
	}
}

// A response whose input reached rotateInput tokens rotates the session at the next listening gap,
// and the old session's cost stays in the call's total.
func TestRotateOnBigInput(t *testing.T) {
	pinNow(t, time.Now())
	e := start(t, false, "")
	ph, ai1 := e.inCall(t)
	ai1.push(t, doneEv("r1", 7000, 0))
	ph.ctlUntil(t, "cost")
	ai2 := e.ai.accept(t)
	ai2.next(t, "session.update")
	ai1.waitClosed(t)
	ai2.push(t, doneEv("r2", 1000, 200))
	if usd, _ := ph.ctlUntil(t, "cost")["usd"].(float64); !near(usd, usdOf(7000, 0)+usdOf(1000, 200)) {
		t.Fatalf("cost after rotation = %v, want %v", usd, usdOf(7000, 0)+usdOf(1000, 200))
	}
}
