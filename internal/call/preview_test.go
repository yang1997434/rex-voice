package call

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/relay"
)

func (ph *phone) expectPreview(t *testing.T, state, msg string) {
	t.Helper()
	m := ph.ctlUntil(t, "preview")
	if m["state"] != state || (msg != "" && m["msg"] != msg) {
		t.Fatalf("preview = %v, want state %s msg %q", m, state, msg)
	}
}

// Protocol section 3: an idle voice.preview dials with the asked voice, streams one answer as type 0
// frames between playing and done, and closes the session.
func TestPreviewPlaysThenDone(t *testing.T) {
	e := start(t, false, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "voice.preview", "provider": "openai", "voice": "cedar"})
	ph.expectPreview(t, "connecting", "")
	ai := e.ai.accept(t)
	sess := ai.next(t, "session.update")["session"].(map[string]any)
	if v := sess["audio"].(map[string]any)["output"].(map[string]any)["voice"]; v != "cedar" {
		t.Fatalf("voice = %v", v)
	}
	if _, ok := sess["tools"]; ok || sess["instructions"] != previewInstructions {
		t.Fatalf("session = %v", sess)
	}
	if b, _ := json.Marshal(ai.next(t, "conversation.item.create")); !bytes.Contains(b, []byte("你好，我是你的语音助手")) {
		t.Fatalf("preview text = %s", b)
	}
	ai.next(t, "response.create")
	out := pattern(2000, 0)
	ai.push(t, delta("it1", out))
	ai.push(t, responseDone)
	ph.expectPreview(t, "playing", "")
	var got []byte
	for len(got) < len(out) {
		typ, plain := ph.next(t)
		if typ != pair.TypeAudio {
			t.Fatalf("control frame in the middle of preview audio: %s", plain)
		}
		got = append(got, plain...)
	}
	if !bytes.Equal(got, out) {
		t.Fatal("preview audio differs")
	}
	ph.expectPreview(t, "done", "")
	ai.waitClosed(t)
}

// Mid-call a preview is refused and the call carries on with its own session.
func TestPreviewRefusedInCall(t *testing.T) {
	e := start(t, false, "")
	ph, ai := e.inCall(t)
	ph.ctl(t, map[string]any{"t": "voice.preview", "voice": "cedar"})
	ph.expectPreview(t, "error", "通话中不能试听")
	select {
	case <-e.ai.conns:
		t.Fatal("dialed a preview session mid-call")
	case <-time.After(200 * time.Millisecond):
	}
	ph.audio(t, pattern(960, 5))
	ai.next(t, "input_audio_buffer.append")
}

// call.start cancels a running preview and then starts the call as usual.
func TestCallStartCancelsPreview(t *testing.T) {
	e := start(t, false, "")
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "voice.preview"})
	ph.expectPreview(t, "connecting", "")
	pv := e.ai.accept(t)
	pv.next(t, "conversation.item.create")
	ph.ctl(t, map[string]any{"t": "call.start"})
	ph.expectPreview(t, "error", "已取消")
	pv.waitClosed(t)
	e.ai.accept(t)
	ph.expectStatus(t, "connecting", "listening")
}

func TestPreviewDialFails(t *testing.T) {
	e := startWith(t, false, "", relay.NewHub(), func(c *Config) { c.Voice.Extra = map[string]string{"url": "ws://127.0.0.1:1"} })
	ph := e.ready(t)
	ph.ctl(t, map[string]any{"t": "voice.preview"})
	ph.expectPreview(t, "connecting", "")
	if m := ph.ctlUntil(t, "preview"); m["state"] != "error" || m["msg"] == "" {
		t.Fatalf("preview = %v", m)
	}
}
