//go:build manual

// A real MOSS-Live round trip (free for now). It logs the first raw text events so the wire field
// names (type / event / action, what is under data) can be checked against client.go's guesses.
//
//	MOSS_API_KEY=$(flow-cred MOSS_API_KEY) go test -tags manual ./internal/voice/moss/ -run Manual -v
package moss

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/voice"
)

// rawPrinter logs the first maxRaw "moss: raw" records verbatim and every non-debug record.
type rawPrinter struct {
	t  *testing.T
	mu sync.Mutex
	n  int
	t0 time.Time
}

const maxRaw = 40

func (p *rawPrinter) Enabled(context.Context, slog.Level) bool { return true }
func (p *rawPrinter) WithAttrs([]slog.Attr) slog.Handler       { return p }
func (p *rawPrinter) WithGroup(string) slog.Handler            { return p }
func (p *rawPrinter) Handle(_ context.Context, r slog.Record) error {
	var attrs []string
	r.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a.String()); return true })
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.t0.IsZero() {
		p.t0 = time.Now()
	}
	ms := time.Since(p.t0).Milliseconds()
	switch {
	case r.Message == "moss: raw":
		if p.n++; p.n <= maxRaw {
			p.t.Logf("[%5d ms raw %d] %s", ms, p.n, strings.Join(attrs, " "))
		}
	case r.Message == "moss: send" || r.Message == "moss: audio":
		p.t.Logf("[%5d ms] %s %s", ms, r.Message, strings.Join(attrs, " "))
	case r.Level > slog.LevelDebug:
		p.t.Logf("[%5d ms log] %s %s", ms, r.Message, strings.Join(attrs, " "))
	}
	return nil
}

type usageWriter struct{ t *testing.T }

func (w usageWriter) Write(b []byte) (int, error) {
	w.t.Logf("[usage.log] %s", strings.TrimSpace(string(b)))
	return len(b), nil
}

// speech is `say -v Tingting` text as PCM16 mono 24 kHz.
func speech(t *testing.T, text string) []byte {
	dir := t.TempDir()
	aiff, wav := filepath.Join(dir, "q.aiff"), filepath.Join(dir, "q.wav")
	if out, err := exec.Command("say", "-v", "Tingting", "-o", aiff, text).CombinedOutput(); err != nil {
		t.Skipf("say: %v %s", err, out)
	}
	if out, err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16@24000", "-c", "1", aiff, wav).CombinedOutput(); err != nil {
		t.Skipf("afconvert: %v %s", err, out)
	}
	b, err := os.ReadFile(wav)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(b), "data")
	if i < 0 {
		t.Fatal("no data chunk")
	}
	pcm := b[i+8:]
	return pcm[:len(pcm)/2*2]
}

// say feeds pcm in 960-byte (20 ms) frames at real-time pace, then hands over to the silence filler
// the way the session layer does when the phone goes quiet.
func say(t *testing.T, s voice.Session, pcm []byte) {
	if err := s.SetMuted(false); err != nil {
		t.Fatal(err)
	}
	for k := 0; k < len(pcm); k += 960 {
		if err := s.AppendAudio(pcm[k:min(k+960, len(pcm))]); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := s.SetMuted(true); err != nil {
		t.Fatal(err)
	}
}

func TestManualAudio(t *testing.T) {
	key := os.Getenv("MOSS_API_KEY")
	if key == "" {
		t.Skip("MOSS_API_KEY not set")
	}
	hello, bye := speech(t, "你好，介绍一下你自己"), speech(t, "挂断吧")
	user := make(chan string, 4)
	assistant := make(chan string, 4)
	turn := make(chan struct{}, 4)
	hangUp := make(chan string, 1)
	errs := make(chan error, 1)
	var audioMu sync.Mutex
	var audioBytes int
	cfg := voice.Config{Provider: voice.Moss, Key: key, Instructions: voice.Build("测试员", nil, nil),
		Logger: slog.New(&rawPrinter{t: t}), UsageLog: usageWriter{t},
		Call: func(_ context.Context, name, args string) string {
			t.Logf("[tool] %s %s", name, args)
			switch name {
			case "hang_up":
				hangUp <- args
				return `{"ok":true}`
			case "list_panes":
				return `[{"session":"jobs","block":"b1","title":"go test","state":"idle"}]`
			}
			return `[]`
		},
		Handlers: voice.Handlers{
			UserText:      func(s string) { t.Logf("[user] %s", s); user <- s },
			AssistantText: func(s string) { t.Logf("[assistant] %s", s); assistant <- s },
			AudioDelta: func(b []byte) {
				audioMu.Lock()
				audioBytes += len(b)
				audioMu.Unlock()
			},
			SpeechStarted: func() { t.Log("[speech_started]") },
			TurnDone:      func() { t.Log("[turn_done]"); turn <- struct{}{} },
			Error:         func(e error) { t.Logf("[error] %v", e); errs <- e },
		}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := voice.Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetMuted(true) // silence while nobody talks
	time.Sleep(500 * time.Millisecond)
	say(t, s, hello)
	spoke := time.Now()
	for _, w := range []struct {
		c    <-chan string
		what string
	}{{user, "UserText"}, {assistant, "AssistantText"}} {
		select {
		case <-w.c:
		case e := <-errs:
			t.Fatal(e)
		case <-time.After(30 * time.Second):
			t.Fatalf("no %s", w.what)
		}
	}
	select {
	case <-turn:
	case e := <-errs:
		t.Fatal(e)
	case <-time.After(30 * time.Second):
		t.Fatal("no TurnDone")
	}
	audioMu.Lock()
	n := audioBytes
	audioMu.Unlock()
	fmt.Fprintf(os.Stderr, "answer audio %d bytes (%.1f s at 24k), turn done %v after speech end\n",
		n, float64(n)/48000, time.Since(spoke).Round(time.Millisecond))
	if n == 0 {
		t.Error("no answer audio")
	}
	say(t, s, bye)
	select {
	case a := <-hangUp:
		if a != hangUpReason {
			t.Errorf("hang_up args %s", a)
		}
	case e := <-errs:
		t.Fatal(e)
	case <-time.After(30 * time.Second):
		t.Fatal("no hang_up after 挂断吧")
	}
}
