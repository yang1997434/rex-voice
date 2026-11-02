//go:build manual

// Real Qwen-Omni Realtime round trips; costs a few hundred tokens each.
//
//	DASHSCOPE_API_KEY=$(flow-cred QWEN_API_KEY) go test -tags manual ./internal/voice/qwen/ -run Manual -v
package qwen

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

// eventPrinter prints the type of every "qwen: event" debug record, collapsing runs of the same type.
type eventPrinter struct {
	t    *testing.T
	last string
	n    int
}

func (p *eventPrinter) Enabled(context.Context, slog.Level) bool { return true }
func (p *eventPrinter) WithAttrs([]slog.Attr) slog.Handler       { return p }
func (p *eventPrinter) WithGroup(string) slog.Handler            { return p }
func (p *eventPrinter) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "qwen: event" {
		p.t.Logf("[log] %s", r.Message)
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "type" {
			return true
		}
		if typ := a.Value.String(); typ == p.last {
			p.n++
		} else {
			p.flush()
			p.last, p.n = typ, 1
		}
		return false
	})
	return nil
}

func (p *eventPrinter) flush() {
	if p.n > 0 {
		p.t.Logf("[event] %s x%d", p.last, p.n)
	}
}

type usageWriter struct{ t *testing.T }

func (w usageWriter) Write(b []byte) (int, error) {
	w.t.Logf("[usage.log] %s", strings.TrimSpace(string(b)))
	return len(b), nil
}

func manualCfg(t *testing.T, p *eventPrinter, h voice.Handlers) voice.Config {
	key := os.Getenv("DASHSCOPE_API_KEY")
	if key == "" {
		t.Skip("DASHSCOPE_API_KEY not set")
	}
	return voice.Config{Provider: voice.Qwen, Key: key, Model: os.Getenv("QWEN_MODEL"), Instructions: "你是电话助手，用中文一句话回答。问时间就调 get_time。",
		Logger: slog.New(p), UsageLog: usageWriter{t}, Handlers: h}
}

func TestManualTextAndTool(t *testing.T) {
	p := &eventPrinter{t: t}
	turn := make(chan struct{}, 4)
	errs := make(chan error, 1)
	var answers []string
	cfg := manualCfg(t, p, voice.Handlers{
		AssistantText: func(s string) { answers = append(answers, s); t.Logf("[assistant] %s", s) },
		TurnDone:      func() { turn <- struct{}{} },
		Error:         func(e error) { errs <- e },
	})
	cfg.TextOnly = true
	called := false
	cfg.Tools = []tools.ToolDef{{Type: "function", Name: "get_time", Description: "取当前北京时间",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}
	cfg.Call = func(_ context.Context, name, args string) string {
		called = true
		t.Logf("[tool] %s %s", name, args)
		return `{"time":"` + time.Now().Format("15:04") + `"}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := voice.Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{"你好，一句话介绍自己", "现在几点？"} {
		if err := s.SendText(q); err != nil {
			t.Fatal(err)
		}
		select {
		case <-turn:
		case e := <-errs:
			t.Fatal(e)
		case <-time.After(30 * time.Second):
			t.Fatal("no TurnDone")
		}
		p.flush()
		p.last, p.n = "", 0
	}
	if !called || len(answers) != 2 {
		t.Errorf("tool called %v, answers %q", called, answers)
	}
}

// TestManualAudio speaks a macOS `say` sentence through AppendAudio (24k in, 16k on the wire) and waits
// for the transcript and a spoken answer.
func TestManualAudio(t *testing.T) {
	p := &eventPrinter{t: t}
	dir := t.TempDir()
	aiff, wav := filepath.Join(dir, "q.aiff"), filepath.Join(dir, "q.wav")
	if out, err := exec.Command("say", "-v", "Tingting", "-o", aiff, "你好，请用一句话介绍你自己").CombinedOutput(); err != nil {
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
	user := make(chan string, 1)
	turn := make(chan struct{}, 1)
	errs := make(chan error, 1)
	var audioBytes int
	cfg := manualCfg(t, p, voice.Handlers{
		UserText:      func(s string) { t.Logf("[user] %s", s); user <- s },
		AssistantText: func(s string) { t.Logf("[assistant] %s", s) },
		AudioDelta:    func(b []byte) { audioBytes += len(b) },
		SpeechStarted: func() { t.Log("[speech_started]") },
		TurnDone:      func() { turn <- struct{}{} },
		Error:         func(e error) { errs <- e },
	})
	cfg.Transcribe = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := voice.Dial(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	silence := make([]byte, 960)
	frames := func(n int, src []byte) {
		for k := 0; k < n; k++ {
			f := silence
			if src != nil {
				if k*960 >= len(src) {
					return
				}
				f = src[k*960 : min(len(src), (k+1)*960)]
				if len(f)%2 != 0 {
					f = f[:len(f)-1]
				}
			}
			if err := s.AppendAudio(f); err != nil {
				t.Error(err)
				return
			}
			time.Sleep(20 * time.Millisecond) // real-time pace
		}
	}
	frames(25, nil)
	frames(len(pcm)/960+1, pcm)
	go frames(150, nil) // 3 s of silence so the VAD sees the end of speech
	var got string
	select {
	case got = <-user:
	case e := <-errs:
		t.Fatal(e)
	case <-time.After(30 * time.Second):
		t.Fatal("no transcript")
	}
	select {
	case <-turn:
	case e := <-errs:
		t.Fatal(e)
	case <-time.After(30 * time.Second):
		t.Fatal("no TurnDone")
	}
	p.flush()
	fmt.Fprintf(os.Stderr, "transcript %q, answer audio %d bytes (%.1f s at 24k)\n", got, audioBytes, float64(audioBytes)/48000)
	if audioBytes == 0 {
		t.Error("no answer audio")
	}
}
