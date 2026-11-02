// fakephone plays the iPhone side of docs/protocol.md against a running rex-voiced, for bring-up
// without a phone: salt exchange, hello, call.start, then one of
//
//	-mode echo   send 5 s of 440 Hz sine and compare the RMS of what comes back (rex-voiced serve -echo)
//	-mode wav    send a 24 kHz mono PCM16 WAV, then keep the mic open with silence until the answer ends
//	-mode idle   send no audio and wait for hang_up (expects reason idle after 90 s)
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/yang1997434/rex-voice/internal/pair"
)

var t0 = time.Now()

func logf(format string, a ...any) {
	fmt.Printf("[%6.2fs] %s\n", time.Since(t0).Seconds(), fmt.Sprintf(format, a...))
}

type frame struct {
	data []byte
	typ  byte
}

var codec = websocket.Codec{Unmarshal: func(d []byte, typ byte, v any) error {
	f := v.(*frame)
	f.data, f.typ = d, typ
	return nil
}}

func main() {
	home, _ := os.UserHomeDir()
	pairFile := flag.String("pair", filepath.Join(home, "Library", "Application Support", "rex-voice", "pair.json"), "pair.json written by rex-voiced pair")
	relay := flag.String("relay", "ws://127.0.0.1:8790/_voice/ws", "relay WebSocket URL")
	mode := flag.String("mode", "echo", "echo | wav | idle")
	wav := flag.String("wav", "", "WAV file for -mode wav (24 kHz mono PCM16)")
	out := flag.String("out", "", "write received audio as raw PCM16 24 kHz here")
	deviceID := flag.String("device-id", "fakephone-0001", "device_id sent in hello")
	wait := flag.Duration("wait", 60*time.Second, "how long to wait for the answer / hang_up")
	flag.Parse()
	if err := run(*pairFile, *relay, *mode, *wav, *out, *deviceID, *wait); err != nil {
		logf("FAIL: %v", err)
		os.Exit(1)
	}
}

type phone struct {
	ws      *websocket.Conn
	tx      *pair.Cipher
	mu      sync.Mutex
	got     []byte // received audio
	gotN    int
	ctl     chan map[string]any
	readErr chan error
}

func run(pairFile, relay, mode, wavPath, out, deviceID string, wait time.Duration) error {
	p, err := pair.Load(pairFile)
	if err != nil {
		return err
	}
	var send []byte
	switch mode {
	case "echo":
		send = sine(5 * time.Second)
	case "wav":
		if send, err = readWAV(wavPath); err != nil {
			return err
		}
	case "idle":
	default:
		return fmt.Errorf("unknown -mode %q", mode)
	}

	wc, err := websocket.NewConfig(relay+"?role=phone&id="+base64.RawURLEncoding.EncodeToString(p.ID[:]), "http://localhost")
	if err != nil {
		return err
	}
	wc.Header.Set("User-Agent", "RexVoice-iOS/fakephone")
	ws, err := wc.DialContext(context.Background())
	if err != nil {
		return err
	}
	defer ws.Close()
	ph := &phone{ws: ws, ctl: make(chan map[string]any, 100), readErr: make(chan error, 1)}

	var salt [16]byte
	rand.Read(salt[:])
	mac, err := waitMacSalt(ws)
	if err != nil {
		return err
	}
	logf("mac salt %x", mac[:4])
	ph.tx = pair.NewCipher(p.Key, pair.PhoneToMac, salt, mac)
	rx := pair.NewCipher(p.Key, pair.MacToPhone, salt, mac)
	if err := websocket.Message.Send(ws, pair.SaltFrame(salt)); err != nil {
		return err
	}
	go ph.read(rx, mac)

	ph.sendCtl(map[string]any{"t": "hello", "app": "fakephone", "device": "Mac", "device_id": deviceID})
	ph.sendCtl(map[string]any{"t": "call.start"})
	if _, err := ph.until("status", "listening", 20*time.Second); err != nil {
		return err
	}

	for off := 0; off < len(send); off += 960 { // paced like a microphone: one 20 ms frame per 20 ms
		ph.send(pair.TypeAudio, send[off:min(off+960, len(send))])
		time.Sleep(20 * time.Millisecond)
	}
	logf("sent %d ms of audio", len(send)/48)

	switch mode {
	case "echo":
		time.Sleep(1500 * time.Millisecond)
		ph.mu.Lock()
		got, n := ph.got, ph.gotN
		ph.mu.Unlock()
		logf("echo: sent %d frames rms=%.1f, got %d frames rms=%.1f", (len(send)+959)/960, rms(send), n, rms(got))
		if n == 0 || math.Abs(rms(got)-rms(send)) > rms(send)*0.05 {
			return errors.New("echo did not come back intact")
		}
	case "wav":
		// Server VAD only ends the turn on audio it receives, so keep streaming silence like an open mic.
		stop, stopped := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(stopped)
			for t := time.NewTicker(20 * time.Millisecond); ; {
				select {
				case <-stop:
					t.Stop()
					return
				case <-t.C:
					ph.send(pair.TypeAudio, make([]byte, 960))
				}
			}
		}()
		_, err := ph.until("status", "speaking", wait)
		if err == nil {
			_, err = ph.until("status", "listening", wait)
		}
		close(stop)
		<-stopped
		if err != nil {
			return err
		}
		ph.mu.Lock()
		logf("answer audio: %d frames, %d ms", ph.gotN, len(ph.got)/48)
		ph.mu.Unlock()
	case "idle":
		m, err := ph.until("hang_up", "", wait)
		if err != nil {
			return err
		}
		if m["reason"] != "idle" {
			return fmt.Errorf("hang_up reason %v, want idle", m["reason"])
		}
		return writeOut(ph, out)
	}
	ph.sendCtl(map[string]any{"t": "call.end"})
	if _, err := ph.until("hang_up", "", 10*time.Second); err != nil {
		return err
	}
	return writeOut(ph, out)
}

func writeOut(ph *phone, out string) error {
	if out == "" {
		return nil
	}
	ph.mu.Lock()
	defer ph.mu.Unlock()
	return os.WriteFile(out, ph.got, 0o600)
}

func waitMacSalt(ws *websocket.Conn) ([16]byte, error) {
	ws.SetReadDeadline(time.Now().Add(20 * time.Second))
	defer ws.SetReadDeadline(time.Time{})
	for {
		var f frame
		if err := codec.Receive(ws, &f); err != nil {
			return [16]byte{}, fmt.Errorf("waiting for mac salt: %w", err)
		}
		if f.typ == websocket.TextFrame {
			logf("relay %s", f.data)
			continue
		}
		if s, ok := pair.ParseSaltFrame(f.data); ok {
			return s, nil
		}
	}
}

func (ph *phone) read(rx *pair.Cipher, mac [16]byte) {
	for {
		var f frame
		if err := codec.Receive(ph.ws, &f); err != nil {
			ph.readErr <- err
			return
		}
		if f.typ == websocket.TextFrame {
			logf("relay %s", f.data)
			continue
		}
		switch f.data[0] {
		case pair.TypeKeepalive:
			continue
		case pair.TypeSalt:
			if s, ok := pair.ParseSaltFrame(f.data); !ok || s != mac {
				logf("new mac salt mid-call (Mac reconnected); fakephone does not re-key")
			}
			continue
		}
		typ, _, plain, err := rx.Open(f.data)
		if err != nil {
			logf("open failed: %v", err)
			continue
		}
		if typ == pair.TypeAudio {
			ph.mu.Lock()
			ph.got = append(ph.got, plain...)
			ph.gotN++
			first := ph.gotN == 1
			ph.mu.Unlock()
			if len(plain) > 960 {
				logf("BAD: audio frame %d bytes > 960", len(plain))
			}
			if first {
				logf("first audio frame (%d bytes)", len(plain))
			}
			continue
		}
		var m map[string]any
		json.Unmarshal(plain, &m)
		logf("<- %s", plain)
		ph.ctl <- m
	}
}

// until waits for a control message with t == want (and state == state when given).
func (ph *phone) until(want, state string, d time.Duration) (map[string]any, error) {
	deadline := time.After(d)
	for {
		select {
		case m := <-ph.ctl:
			if m["t"] == want && (state == "" || m["state"] == state) {
				return m, nil
			}
		case err := <-ph.readErr:
			return nil, fmt.Errorf("connection closed waiting for %s %s: %w", want, state, err)
		case <-deadline:
			return nil, fmt.Errorf("timed out waiting for %s %s", want, state)
		}
	}
}

func (ph *phone) send(typ byte, b []byte) {
	if err := websocket.Message.Send(ph.ws, ph.tx.Seal(typ, b)); err != nil {
		logf("send failed: %v", err)
	}
}

func (ph *phone) sendCtl(v map[string]any) {
	b, _ := json.Marshal(v)
	logf("-> %s", b)
	ph.send(pair.TypeControl, b)
}

func sine(d time.Duration) []byte {
	n := int(d.Seconds() * 24000)
	b := make([]byte, 2*n)
	for i := range n {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(0.3*32767*math.Sin(2*math.Pi*440*float64(i)/24000))))
	}
	return b
}

func rms(pcm []byte) float64 {
	var sum float64
	n := len(pcm) / 2
	for i := range n {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
		sum += v * v
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}

// readWAV returns the data chunk of a 24 kHz mono 16-bit PCM WAV, walking the chunks
// because afconvert may insert extra ones (e.g. FLLR) before "data".
func readWAV(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var fmtOK bool
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		body := b[off+8 : min(off+8+size, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("short fmt chunk")
			}
			ch, rate, bits := binary.LittleEndian.Uint16(body[2:]), binary.LittleEndian.Uint32(body[4:]), binary.LittleEndian.Uint16(body[14:])
			if binary.LittleEndian.Uint16(body) != 1 || ch != 1 || rate != 24000 || bits != 16 {
				return nil, fmt.Errorf("want PCM mono 24000 Hz 16-bit, got format %d ch %d rate %d bits %d", binary.LittleEndian.Uint16(body), ch, rate, bits)
			}
			fmtOK = true
		case "data":
			if !fmtOK {
				return nil, errors.New("data chunk before fmt")
			}
			logf("wav data chunk at byte %d, %d bytes", off+8, len(body))
			return body, nil
		}
		off += 8 + size + size%2
	}
	return nil, errors.New("no data chunk: " + strings.TrimSpace(path))
}
