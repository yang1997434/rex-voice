//go:build manual

// Real-server check, needs a Doubao voice key and macOS say:
//
//	DOUBAO_API_KEY=$(flow-cred DOUBAO_API_KEY) go test -tags manual -run Manual -v ./internal/voice/doubao
//
// 3.0 takes no text input (ErrNoTextInput), so the question is spoken by `say` and streamed in real time.
package doubao

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

// sayPCM speaks text with macOS say and returns PCM16 mono 24 kHz.
func sayPCM(t *testing.T, text string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q.wav")
	if out, err := exec.Command("say", "-v", "Tingting", "--file-format=WAVE", "--data-format=LEI16@24000", "-o", path, text).CombinedOutput(); err != nil {
		t.Fatalf("say: %v %s", err, out)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 12; i+8 <= len(b); { // walk RIFF chunks to "data"
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if string(b[i:i+4]) == "data" {
			return b[i+8 : min(i+8+n, len(b))]
		}
		i += 8 + n + n%2
	}
	t.Fatal("no data chunk in say output")
	return nil
}

func TestManualToolCall(t *testing.T) {
	key := os.Getenv("DOUBAO_API_KEY")
	if key == "" {
		t.Skip("DOUBAO_API_KEY not set")
	}
	t0 := time.Now()
	ev := func(format string, a ...any) {
		t.Logf("%6dms "+format, append([]any{time.Since(t0).Milliseconds()}, a...)...)
	}
	var usage bytes.Buffer
	turn := make(chan struct{}, 4)
	var audioBytes atomic.Int64
	cfg := voice.Config{
		Provider:     voice.Doubao,
		Key:          key,
		Instructions: "你是语音助手。用户问时间时必须调用 get_time 工具，然后用一句话回答。不要念出工具名。",
		Tools: []tools.ToolDef{{Type: "function", Name: "get_time", Description: "返回现在的本地时间",
			Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		Call: func(ctx context.Context, name, args string) string {
			ev("[tool] %s %s", name, args)
			return `{"time":"` + time.Now().Format("15:04") + `"}`
		},
		UsageLog: &usage,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Handlers: voice.Handlers{
			AudioDelta:    func(p []byte) { audioBytes.Add(int64(len(p))) },
			TextDelta:     func(d string) { ev("text.delta %q", d) },
			AssistantText: func(s string) { ev("assistant %q", s) },
			UserText:      func(s string) { ev("user %q", s) },
			SpeechStarted: func() { ev("speech started") },
			ResponseDone:  func(u voice.Usage) { ev("response.done %+v audio=%dB", u, audioBytes.Load()) },
			TurnDone:      func() { ev("turn done"); turn <- struct{}{} },
			Error:         func(err error) { ev("[error] %v", err) },
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	s, err := voice.Dial(ctx, cfg)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ev("close: %v", s.Close()) }()
	ev("connected id=%s", s.(*Session).ID())

	pcm := append(sayPCM(t, "现在几点了？"), make([]byte, 48000*2)...) // the question, then 2 s of silence
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for i := 0; i < len(pcm); i += 960 {
		<-tick.C
		if err := s.AppendAudio(pcm[i:min(i+960, len(pcm))]); err != nil {
			t.Fatal(err)
		}
	}
	ev("audio sent, muting")
	if err := s.SetMuted(true); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case <-turn:
			if s.(*Session).inflight.Load() == 0 && audioBytes.Load() > 0 {
				t.Logf("usage log:\n%s", usage.String())
				return
			}
		case <-s.Done():
			t.Fatal("session ended")
		case <-time.After(30 * time.Second):
			t.Fatal("no answer in 30 s")
		}
	}
}
