//go:build manual

// A real MOSS-Live round trip with tools (free for now): the model calls list_panes, answers from its
// result after the function_call_output, then calls hang_up itself. Every text event is logged
// verbatim except *.transcript.updated; audio frames are not.
//
//	MOSS_API_KEY=$(flow-cred MOSS_API_KEY) go test -tags manual ./internal/voice/moss/ -run Manual -v
package moss

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

// rawPrinter logs "moss: raw" records verbatim (except transcript.updated), sends, and every non-debug record.
type rawPrinter struct {
	t  *testing.T
	mu sync.Mutex
	t0 time.Time
}

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
		if a := strings.Join(attrs, " "); !strings.Contains(a, "transcript.updated") {
			p.t.Logf("[%5d ms raw] %s", ms, a)
		}
	case r.Message == "moss: send":
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

// twoPanes is list_panes' fixed answer.
const twoPanes = `[{"session":"jobs","block":"b1","label":"go test","project":"rex-voice","app":"shell","state":"working","doing":"在跑命令"},` +
	`{"session":"rex-voice","block":"b2","label":"claude","project":"rex-voice","app":"claude-code","state":"idle","doing":"空闲"}]`

func TestManualAudio(t *testing.T) {
	key := os.Getenv("MOSS_API_KEY")
	if key == "" {
		t.Skip("MOSS_API_KEY not set")
	}
	ask, bye := speech(t, "现在 Mac 上有几个窗格在跑？帮我查一下"), speech(t, "挂断吧")
	// A real registry: its argument check rejects unknown keys, so a leftover spoken_response would fail the call.
	var reg tools.Registry
	reg.Register(tools.Tool{Name: "list_panes", Description: "列出 Mac 上 Rex 的窗格；用户问窗格、Claude、任务状态时必须调用",
		Parameters: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Run:        func(context.Context, json.RawMessage) (any, error) { return json.RawMessage(twoPanes), nil }})
	hangUp := make(chan string, 1)
	reg.Register(tools.HangUpDef(func(reason string) {
		select {
		case hangUp <- reason:
		default:
		}
	}))
	called := make(chan string, 8)
	assistant := make(chan string, 16)
	turn := make(chan struct{}, 8)
	errs := make(chan error, 4)
	var audioMu sync.Mutex
	var audioBytes int
	cfg := voice.Config{Provider: voice.Moss, Key: key, Instructions: voice.Build("测试员", nil, nil), Tools: reg.Defs(),
		Logger: slog.New(&rawPrinter{t: t}), UsageLog: usageWriter{t},
		Call: func(ctx context.Context, name, args string) string {
			out := reg.Call(ctx, name, args)
			t.Logf("[tool] %s %s -> %s", name, args, out)
			if strings.HasPrefix(out, `{"error"`) {
				errs <- fmt.Errorf("tool %s failed: %s", name, out)
			}
			called <- name
			return out
		},
		Handlers: voice.Handlers{
			UserText:      func(s string) { t.Logf("[user] %s", s) },
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
	say(t, s, ask)
	spoke := time.Now()
	waitTool := func(want string) {
		t.Helper()
		for {
			select {
			case name := <-called:
				if name == want {
					return
				}
			case e := <-errs:
				t.Fatal(e)
			case <-time.After(30 * time.Second):
				t.Fatalf("no %s call", want)
			}
		}
	}
	waitTool("list_panes")
	// The filler line's transcript may land around the tool call; the answer from the result comes after it.
	var said []string
	for done := false; !done; {
		select {
		case a := <-assistant:
			said = append(said, a)
		case <-turn:
			done = true
		case e := <-errs:
			t.Fatal(e)
		case <-time.After(30 * time.Second):
			t.Fatalf("no TurnDone after the list_panes output (assistant said %q)", said)
		}
	}
	for len(assistant) > 0 {
		said = append(said, <-assistant)
	}
	if all := strings.Join(said, " "); !strings.ContainsAny(all, "两2二") && !strings.Contains(all, "jobs") && !strings.Contains(all, "rex-voice") {
		t.Errorf("answer does not use the list_panes result: %q", said)
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
	waitTool("hang_up")
	select {
	case r := <-hangUp:
		t.Logf("hang_up reason %q", r)
	default:
		t.Error("hang_up called but its Run did not")
	}
}
