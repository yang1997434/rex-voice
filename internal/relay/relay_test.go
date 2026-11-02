package relay

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestInviteRequired(t *testing.T) {
	h, s := newRelay(t)
	h.Invites = []string{"testcode1", "testcode2"}
	for _, q := range []string{"", "&invite=", "&invite=wrongcode", "&invite=testcode", "&invite=testcode1x"} {
		resp, err := http.Get(s.URL + "/_voice/ws?role=mac&id=" + testID + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%q: got %d, want 403", q, resp.StatusCode)
		}
	}
	mac := dial(t, s, "mac", testID+"&invite=testcode2")
	expectPeer(t, mac, false)
}

func TestNoInvitesConfiguredLetsAnyoneIn(t *testing.T) {
	_, s := newRelay(t)
	expectPeer(t, dial(t, s, "mac", testID), false)
	expectPeer(t, dial(t, s, "phone", testID+"&invite=whatever1"), true)
}

func TestParseInvites(t *testing.T) {
	got, err := ParseInvites(" testcode1, ,test-code_2 ")
	if err != nil || len(got) != 2 || got[0] != "testcode1" || got[1] != "test-code_2" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, err := ParseInvites(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %q %v", got, err)
	}
	for _, bad := range []string{"short", "testcode1,has space1", "bad/slash00"} {
		if _, err := ParseInvites(bad); err == nil {
			t.Errorf("%q accepted", bad)
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

// pushFunc is a Notifier made of a function.
type pushFunc func(ctx context.Context, token string, a Alert) error

func (f pushFunc) Push(ctx context.Context, token string, a Alert) error { return f(ctx, token, a) }

// Binary frames the relay reads while there is no peer are dropped, not queued for a later peer.
// sendBin returning only means the frames are in the socket, so the relay may still be reading them
// when the phone is published; it then forwards them, and the phone's first frame was "lost" (a ~1%
// flake under -race, 2026-11-02). The notify after them is a barrier: the relay reads a connection in
// order, so once it has pushed, every lost frame has been read and dropped.
func TestDropWithoutPeer(t *testing.T) {
	h, s := newRelay(t)
	pushed := make(chan struct{}, 1)
	h.Notifier = pushFunc(func(context.Context, string, Alert) error { pushed <- struct{}{}; return nil })
	h.mu.Lock()
	h.tokens[testID] = "tok"
	h.mu.Unlock()
	mac := dial(t, s, "mac", testID)
	expectPeer(t, mac, false)
	for i := 0; i < 200; i++ {
		sendBin(t, mac, []byte("lost"))
	}
	sendText(t, mac, `{"t":"notify","title":"barrier"}`)
	select {
	case <-pushed:
	case <-time.After(3 * time.Second):
		t.Fatal("relay never handled the notify after the lost frames")
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

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// writeP8 writes a fresh P-256 key as a PEM PKCS#8 file, the format Apple's .p8 uses.
func writeP8(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, k
}

type apnsHit struct {
	path   string
	header http.Header
	body   []byte
}

// fakeAPNs records every request and answers with status and body.
func fakeAPNs(t *testing.T, status int, body string) (*httptest.Server, chan apnsHit) {
	t.Helper()
	hits := make(chan apnsHit, 8)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hits <- apnsHit{r.URL.Path, r.Header.Clone(), b}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s, hits
}

func newTestAPNS(t *testing.T, url string) *APNS {
	t.Helper()
	keyPath, _ := writeP8(t)
	n, err := NewAPNS(keyPath, "KEY123", "KAA6GJ59XS", url)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func sendText(t *testing.T, ws *websocket.Conn, s string) {
	t.Helper()
	if err := websocket.Message.Send(ws, s); err != nil {
		t.Fatalf("send: %v", err)
	}
}

// syncPump proves every frame `from` sent before it has been handled by the relay's pump.
func syncPump(t *testing.T, from, to *websocket.Conn) {
	t.Helper()
	sendBin(t, from, []byte{9})
	expectBinary(t, to, []byte{9})
}

func tokenOf(h *Hub, id string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tokens[id]
}

func TestPushTokenStored(t *testing.T) {
	h, s := newRelay(t)
	mac, phone := pair(t, s)
	sendText(t, phone, `{"t":"push_token","token":"not-hex"}`)
	sendText(t, phone, `{bad json`)
	syncPump(t, phone, mac)
	if got := tokenOf(h, testID); got != "" {
		t.Fatalf("bad token stored: %q", got)
	}
	sendText(t, mac, `{"t":"push_token","token":"`+testToken+`"}`) // only the phone may register
	syncPump(t, mac, phone)
	if got := tokenOf(h, testID); got != "" {
		t.Fatalf("mac token stored: %q", got)
	}
	sendText(t, phone, `{"t":"push_token","token":"`+testToken+`"}`)
	syncPump(t, phone, mac)
	if got := tokenOf(h, testID); got != testToken {
		t.Fatalf("token = %q", got)
	}
	phone.Close()
	expectPeer(t, mac, false)
	if got := tokenOf(h, testID); got != testToken {
		t.Fatalf("token lost after phone left: %q", got)
	}
}

func TestNotifyPushesToAPNs(t *testing.T) {
	h, s := newRelay(t)
	fake, hits := fakeAPNs(t, 200, "")
	h.Notifier = newTestAPNS(t, fake.URL)
	mac, phone := pair(t, s)
	sendText(t, phone, `{"t":"push_token","token":"`+testToken+`"}`)
	syncPump(t, phone, mac)
	sendText(t, mac, `{"t":"notify","title":"Claude 卡住了","body":"窗格 2 在等你"}`)

	var hit apnsHit
	select {
	case hit = <-hits:
	case <-time.After(3 * time.Second):
		t.Fatal("no APNs request")
	}
	if hit.path != "/3/device/"+testToken {
		t.Fatalf("path = %q", hit.path)
	}
	for k, want := range map[string]string{"apns-topic": "com.maruiao.rexvoice", "apns-push-type": "alert", "apns-priority": "10"} {
		if got := hit.header.Get(k); got != want {
			t.Fatalf("%s = %q, want %q", k, got, want)
		}
	}
	if a := hit.header.Get("authorization"); !strings.HasPrefix(a, "bearer ") || strings.Count(a, ".") != 2 {
		t.Fatalf("authorization = %q", a)
	}
	var body struct {
		Aps struct {
			Alert struct{ Title, Body string }
			Sound string
		}
	}
	if err := json.Unmarshal(hit.body, &body); err != nil {
		t.Fatalf("body %s: %v", hit.body, err)
	}
	if body.Aps.Alert.Title != "Claude 卡住了" || body.Aps.Alert.Body != "窗格 2 在等你" || body.Aps.Sound != "default" {
		t.Fatalf("body = %s", hit.body)
	}
	if strings.Contains(string(hit.body), "category") || strings.Contains(string(hit.body), "block") {
		t.Fatalf("notify without block got actions: %s", hit.body)
	}
}

// A notify with block becomes an actionable push: category claude-ask, block and options as custom keys.
func TestNotifyWithBlockIsActionable(t *testing.T) {
	h, s := newRelay(t)
	fake, hits := fakeAPNs(t, 200, "")
	h.Notifier = newTestAPNS(t, fake.URL)
	mac, phone := pair(t, s)
	sendText(t, phone, `{"t":"push_token","token":"`+testToken+`"}`)
	syncPump(t, phone, mac)
	sendText(t, mac, `{"t":"notify","title":"geo 的 Claude 在等你","body":"午饭吃什么？","block":"block:01a1","options":["面条","米饭"]}`)
	var hit apnsHit
	select {
	case hit = <-hits:
	case <-time.After(3 * time.Second):
		t.Fatal("no APNs request")
	}
	var body struct {
		Aps struct {
			Alert          struct{ Title, Body string }
			Category       string
			MutableContent *int `json:"mutable-content"`
		}
		Block   string
		Options []string
	}
	if err := json.Unmarshal(hit.body, &body); err != nil {
		t.Fatalf("body %s: %v", hit.body, err)
	}
	if body.Aps.Category != "claude-ask" || body.Aps.MutableContent == nil || *body.Aps.MutableContent != 0 ||
		body.Block != "block:01a1" || len(body.Options) != 2 || body.Options[1] != "米饭" || body.Aps.Alert.Body != "午饭吃什么？" {
		t.Fatalf("body = %s", hit.body)
	}
}

func TestNotifyWithoutTokenSkips(t *testing.T) {
	h, s := newRelay(t)
	fake, hits := fakeAPNs(t, 200, "")
	h.Notifier = newTestAPNS(t, fake.URL)
	mac, phone := pair(t, s)
	sendText(t, mac, `{"t":"notify","title":"x","body":"y"}`) // no token yet
	syncPump(t, mac, phone)
	sendText(t, phone, `{"t":"push_token","token":"`+testToken+`"}`)
	sendText(t, phone, `{"t":"notify","title":"x","body":"y"}`) // phone may not notify
	syncPump(t, phone, mac)
	select {
	case hit := <-hits:
		t.Fatalf("unexpected APNs request %s", hit.path)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifyWithoutNotifierSkips(t *testing.T) {
	h, s := newRelay(t) // Notifier nil: token kept, notify dropped, connection survives
	mac, phone := pair(t, s)
	sendText(t, phone, `{"t":"push_token","token":"`+testToken+`"}`)
	syncPump(t, phone, mac)
	sendText(t, mac, `{"t":"notify","title":"x","body":"y"}`)
	syncPump(t, mac, phone)
	if tokenOf(h, testID) != testToken {
		t.Fatal("token not stored")
	}
}

func TestAPNsErrorCarriesReason(t *testing.T) {
	fake, _ := fakeAPNs(t, 400, `{"reason":"BadDeviceToken"}`)
	err := newTestAPNS(t, fake.URL).Push(t.Context(), testToken, Alert{Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "BadDeviceToken") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPNsJWT(t *testing.T) {
	keyPath, k := writeP8(t)
	n, err := NewAPNS(keyPath, "KEY123", "KAA6GJ59XS", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	n.now = func() time.Time { return now }
	tok, err := n.token()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	dec := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("decode %q: %v", s, err)
		}
		return b
	}
	var hdr struct{ Alg, Kid string }
	var claims struct {
		Iss string
		Iat int64
	}
	json.Unmarshal(dec(parts[0]), &hdr)
	json.Unmarshal(dec(parts[1]), &claims)
	if hdr.Alg != "ES256" || hdr.Kid != "KEY123" || claims.Iss != "KAA6GJ59XS" || claims.Iat != now.Unix() {
		t.Fatalf("hdr=%+v claims=%+v", hdr, claims)
	}
	sig := dec(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&k.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("signature does not verify")
	}

	now = now.Add(49 * time.Minute)
	if again, _ := n.token(); again != tok {
		t.Fatal("jwt re-signed before 50 min")
	}
	now = now.Add(2 * time.Minute)
	if again, _ := n.token(); again == tok {
		t.Fatal("jwt not refreshed after 50 min")
	}

	bad := filepath.Join(t.TempDir(), "bad.p8")
	os.WriteFile(bad, []byte("not a key"), 0o600)
	if _, err := NewAPNS(bad, "K", "T", ""); err == nil {
		t.Fatal("bad key accepted")
	}
}
