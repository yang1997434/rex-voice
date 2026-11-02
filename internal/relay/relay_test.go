package relay

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

const testID = "abcdef0123456789"

func newRelay(t *testing.T) (*Hub, *httptest.Server) {
	t.Helper()
	h := NewHub()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return h, s
}

func dial(t *testing.T, s *httptest.Server, role, id string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(s.URL, "http") + "/_voice/ws?role=" + role + "&id=" + id
	ws, err := websocket.Dial(u, "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial %s: %v", role, err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func recv(t *testing.T, ws *websocket.Conn) frame {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var f frame
	if err := frameCodec.Receive(ws, &f); err != nil {
		t.Fatalf("receive: %v", err)
	}
	return f
}

func expectPeer(t *testing.T, ws *websocket.Conn, present bool) {
	t.Helper()
	want := `{"t":"peer","present":false}`
	if present {
		want = `{"t":"peer","present":true}`
	}
	f := recv(t, ws)
	if f.typ != websocket.TextFrame || string(f.data) != want {
		t.Fatalf("got type=%d %q, want text %s", f.typ, f.data, want)
	}
}

func expectBinary(t *testing.T, ws *websocket.Conn, want []byte) {
	t.Helper()
	f := recv(t, ws)
	if f.typ != websocket.BinaryFrame || !bytes.Equal(f.data, want) {
		t.Fatalf("got type=%d len=%d, want binary len=%d", f.typ, len(f.data), len(want))
	}
}

func sendBin(t *testing.T, ws *websocket.Conn, b []byte) {
	t.Helper()
	if err := websocket.Message.Send(ws, b); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func expectClosed(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var f frame
	err := frameCodec.Receive(ws, &f)
	if err == nil {
		t.Fatalf("expected closed connection, got frame type=%d %q", f.typ, f.data)
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("expected closed connection, got read timeout")
	}
}

// pair connects mac then phone and consumes the initial peer frames.
func pair(t *testing.T, s *httptest.Server) (mac, phone *websocket.Conn) {
	t.Helper()
	mac = dial(t, s, "mac", testID)
	expectPeer(t, mac, false)
	phone = dial(t, s, "phone", testID)
	expectPeer(t, phone, true)
	expectPeer(t, mac, true)
	return mac, phone
}

func TestHealthz(t *testing.T) {
	_, s := newRelay(t)
	resp, err := http.Get(s.URL + "/_voice/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != 200 || buf.String() != "ok" {
		t.Fatalf("got %d %q", resp.StatusCode, buf.String())
	}
}

func TestBadRoleOrID(t *testing.T) {
	_, s := newRelay(t)
	for _, q := range []string{
		"role=tablet&id=" + testID,
		"id=" + testID,
		"role=mac",
		"role=mac&id=short",
		"role=mac&id=" + strings.Repeat("a", 65),
		"role=mac&id=bad%2Fslash00",
	} {
		resp, err := http.Get(s.URL + "/_voice/ws?" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestUpgradeWithoutOrigin(t *testing.T) {
	_, s := newRelay(t)
	c, err := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("GET /_voice/ws?role=phone&id=" + testID + " HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"))
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.Contains(line, " 101 ") {
		t.Fatalf("status line %q err=%v, want 101", line, err)
	}
}

func TestForwardBothWays(t *testing.T) {
	_, s := newRelay(t)
	mac, phone := pair(t, s)
	websocket.Message.Send(mac, "client text is ignored")
	sendBin(t, mac, []byte("from mac"))
	expectBinary(t, phone, []byte("from mac"))
	sendBin(t, phone, []byte("from phone"))
	expectBinary(t, mac, []byte("from phone"))
	big := bytes.Repeat([]byte{7}, MaxFrameBytes) // exactly at the limit still passes
	sendBin(t, phone, big)
	expectBinary(t, mac, big)
}

func TestDropWithoutPeer(t *testing.T) {
	_, s := newRelay(t)
	mac := dial(t, s, "mac", testID)
	expectPeer(t, mac, false)
	for i := 0; i < 200; i++ {
		sendBin(t, mac, []byte("lost"))
	}
	phone := dial(t, s, "phone", testID)
	expectPeer(t, phone, true)
	expectPeer(t, mac, true)
	sendBin(t, mac, []byte("kept"))
	expectBinary(t, phone, []byte("kept"))
}

func TestPeerLeaveKeepsConnection(t *testing.T) {
	_, s := newRelay(t)
	mac, phone := pair(t, s)
	phone.Close()
	expectPeer(t, mac, false)
	phone2 := dial(t, s, "phone", testID)
	expectPeer(t, phone2, true)
	expectPeer(t, mac, true)
	sendBin(t, phone2, []byte("back"))
	expectBinary(t, mac, []byte("back"))
}

func TestSameRoleReplacesOld(t *testing.T) {
	_, s := newRelay(t)
	mac1, phone := pair(t, s)
	mac2 := dial(t, s, "mac", testID)
	expectPeer(t, mac2, true)
	expectClosed(t, mac1)

	sendBin(t, mac2, []byte("new mac"))
	for {
		f := recv(t, phone)
		if f.typ == websocket.BinaryFrame {
			if string(f.data) != "new mac" {
				t.Fatalf("phone got %q", f.data)
			}
			break
		}
		if string(f.data) != `{"t":"peer","present":true}` {
			t.Fatalf("phone got %q after replace, want no present:false", f.data)
		}
	}
	sendBin(t, phone, []byte("to new mac"))
	expectBinary(t, mac2, []byte("to new mac"))
}

func TestOversizeFrameCloses(t *testing.T) {
	_, s := newRelay(t)
	mac, phone := pair(t, s)
	sendBin(t, mac, make([]byte, MaxFrameBytes+1))
	expectClosed(t, mac)
	expectPeer(t, phone, false)
}

func TestSlowPeerOnlyClosesPeer(t *testing.T) {
	h, s := newRelay(t)
	h.writeTimeout = 200 * time.Millisecond // set before any connection reads it
	mac, phone := pair(t, s)
	_ = phone // never reads, so the relay's writes to it stall

	done := make(chan struct{})
	defer close(done)
	go func() {
		chunk := make([]byte, MaxFrameBytes)
		for {
			select {
			case <-done:
				return
			default:
			}
			if websocket.Message.Send(mac, chunk) != nil {
				return
			}
		}
	}()
	expectPeer(t, mac, false)

	phone2 := dial(t, s, "phone", testID)
	expectPeer(t, phone2, true)
	f := recv(t, phone2) // mac is still alive: its flood now reaches the new phone
	if f.typ != websocket.BinaryFrame {
		t.Fatalf("phone2 got type=%d %q", f.typ, f.data)
	}
}

// A client ping after the relay's last write must not kill the connection: the pong is written
// from inside Receive, outside send's deadline.
func TestPingAfterQuietSpell(t *testing.T) {
	h, s := newRelay(t)
	h.writeTimeout = 50 * time.Millisecond
	mac := dial(t, s, "mac", testID)
	expectPeer(t, mac, false)
	phone := dial(t, s, "phone", testID)
	expectPeer(t, phone, true)
	expectPeer(t, mac, true)
	time.Sleep(120 * time.Millisecond) // let the deadline from the last peer notice expire
	w, err := mac.NewFrameWriter(websocket.PingFrame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hi")); err != nil { // Write sends the frame; Close is a no-op
		t.Fatal(err)
	}
	// The pong's flush error is swallowed by x/net/websocket but sticks in its bufio.Writer,
	// so the symptom is the next relay write to mac failing.
	sendBin(t, phone, []byte{4, 5, 6})
	expectBinary(t, mac, []byte{4, 5, 6})
}
