//go:build manual

// Real-provider voice preview through the in-process relay and fake phone (costs a few cents per run):
//
//	OPENAI_API_KEY=… DASHSCOPE_API_KEY=… go test -tags manual -run TestPreviewReal -v ./internal/call/
package call

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/relay"
	"github.com/yang1997434/rex-voice/internal/voice"
	_ "github.com/yang1997434/rex-voice/internal/voice/qwen"
)

func TestPreviewReal(t *testing.T) {
	for _, tc := range []struct {
		provider, model, voice, env string
		extra                       map[string]string
	}{
		{"openai", "", "cedar", "OPENAI_API_KEY", nil},
		{"qwen", "qwen-audio-3.1-realtime-plus", "longanqian", "DASHSCOPE_API_KEY", map[string]string{"region": "intl"}},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			key := os.Getenv(tc.env)
			if key == "" {
				t.Skip(tc.env + " not set")
			}
			logs, usage := &syncBuf{}, &syncBuf{}
			e := startWith(t, false, "", relay.NewHub(), func(c *Config) {
				c.Logger = slog.New(slog.NewTextHandler(logs, nil))
				c.Voice = voice.Config{Provider: voice.Provider(tc.provider), UsageLog: usage, Extra: tc.extra}
			})
			ph := e.ready(t)
			ph.ctl(t, map[string]any{"t": "voice.preview", "provider": tc.provider, "model": tc.model, "voice": tc.voice, "api_key": key})
			var states []string
			audio := 0
			deadline := time.After(previewTimeout + 5*time.Second)
		loop:
			for {
				select {
				case f, ok := <-ph.frames:
					if !ok {
						t.Fatal("phone connection closed")
					}
					if f.typ != websocket.BinaryFrame || f.data[0] == pair.TypeKeepalive || f.data[0] == pair.TypeSalt {
						continue
					}
					typ, _, plain, err := ph.rx.Open(f.data)
					if err != nil {
						t.Fatal(err)
					}
					if typ == pair.TypeAudio {
						audio += len(plain)
						continue
					}
					var m map[string]any
					json.Unmarshal(plain, &m)
					if m["t"] != "preview" {
						continue
					}
					states = append(states, m["state"].(string))
					if m["state"] == "error" {
						t.Fatalf("preview error: %v", m["msg"])
					}
					if m["state"] == "done" {
						break loop
					}
				case <-deadline:
					t.Fatalf("no done; states %v", states)
				}
			}
			secs := float64(audio) / 48000
			t.Logf("states %v, audio %.2f s", states, secs)
			t.Logf("usage: %s", strings.TrimSpace(usage.String()))
			for _, l := range strings.Split(logs.String(), "\n") {
				if strings.Contains(l, "preview") {
					t.Log(l)
				}
			}
			if strings.Join(states, ",") != "connecting,playing,done" || secs < 1 {
				t.Fatalf("states %v, %.2f s of audio", states, secs)
			}
			if strings.Contains(logs.String(), key) || strings.Contains(usage.String(), key) {
				t.Fatal("api key in the logs")
			}
		})
	}
}
