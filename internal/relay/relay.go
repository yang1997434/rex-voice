// Package relay pairs a mac and a phone WebSocket by id and forwards
// binary frames between them verbatim. It never decrypts or stores frames.
package relay

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const MaxFrameBytes = 64 << 10

var (
	idRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	tokenRe = regexp.MustCompile(`^[0-9A-Fa-f]{32,200}$`) // goes into the APNs URL path
)

type conn struct {
	ws       *websocket.Conn
	role, id string
	statusMu sync.Mutex // serializes peer-status sends so the last one reflects the latest state
}

// Hub holds at most one connection per (id, role).
type Hub struct {
	mu           sync.Mutex
	conns        map[string]*conn  // key: id + "|" + role
	tokens       map[string]string // id → phone's APNs device token; memory only, the phone resends on connect
	writeTimeout time.Duration

	Notifier Notifier // nil = notify frames are dropped; set before serving
}

func NewHub() *Hub {
	return &Hub{conns: map[string]*conn{}, tokens: map[string]string{}, writeTimeout: 10 * time.Second}
}

func key(id, role string) string { return id + "|" + role }

func other(role string) string {
	if role == "mac" {
		return "phone"
	}
	return "mac"
}

func (h *Hub) peerOf(c *conn) *conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[key(c.id, other(c.role))]
}

// ServeHTTP serves /_voice/healthz and /_voice/ws.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_voice/healthz":
		w.Write([]byte("ok"))
	case "/_voice/ws":
		role, id := r.URL.Query().Get("role"), r.URL.Query().Get("id")
		if (role != "mac" && role != "phone") || !idRe.MatchString(id) {
			http.Error(w, "bad role or id", http.StatusBadRequest)
			return
		}
		// websocket.Server with nil Handshake skips the Origin check; native clients send none.
		websocket.Server{Handler: func(ws *websocket.Conn) { h.serve(&conn{ws: ws, role: role, id: id}) }}.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Hub) serve(c *conn) {
	c.ws.MaxPayloadBytes = MaxFrameBytes
	short := c.id[:6]

	// The first thing a client reads must be its peer notice: send it before publishing c in the map,
	// or the peer's forwarded frames could arrive ahead of it. A peer that connects in between is caught
	// by the re-lookup after publishing (both sides then get a fresh notice; duplicates are harmless).
	h.mu.Lock()
	first := h.conns[key(c.id, other(c.role))]
	h.mu.Unlock()
	h.sendPeer(c, first != nil)

	h.mu.Lock()
	old := h.conns[key(c.id, c.role)]
	h.conns[key(c.id, c.role)] = c
	peer := h.conns[key(c.id, other(c.role))]
	h.mu.Unlock()

	log.Printf("open role=%s id=%s replaced=%t peer=%t", c.role, short, old != nil, peer != nil)
	if old != nil {
		kill(old.ws)
	}
	if peer != nil {
		h.notify(peer)
		if peer != first {
			h.notify(c) // the peer arrived after c's first notice
		}
	}

	reason := h.pump(c)

	h.mu.Lock()
	mine := h.conns[key(c.id, c.role)] == c
	if mine {
		delete(h.conns, key(c.id, c.role))
		peer = h.conns[key(c.id, other(c.role))]
	}
	h.mu.Unlock()
	kill(c.ws)
	log.Printf("close role=%s id=%s reason=%q", c.role, short, reason)
	if mine && peer != nil {
		h.notify(peer)
	}
}

type frame struct {
	data []byte
	typ  byte
}

var frameCodec = websocket.Codec{
	Unmarshal: func(data []byte, typ byte, v any) error {
		f := v.(*frame)
		f.data, f.typ = data, typ
		return nil
	},
}

// pump reads frames from c until error, forwarding binary frames to the current peer
// and handling the relay's own text frames (push_token, notify).
func (h *Hub) pump(c *conn) string {
	for {
		var f frame
		if err := frameCodec.Receive(c.ws, &f); err != nil {
			return err.Error()
		}
		if f.typ == websocket.TextFrame {
			h.control(c, f.data)
			continue
		}
		if f.typ != websocket.BinaryFrame {
			continue
		}
		if p := h.peerOf(c); p != nil {
			h.send(p, f.data)
		}
	}
}

// control handles a client text frame: phone sends push_token, mac sends notify; anything else is ignored.
func (h *Hub) control(c *conn, data []byte) {
	var m struct {
		T, Token, Title, Body, Block string
		Options                      []string
	}
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("bad text frame role=%s id=%s err=%v", c.role, c.id[:6], err)
		return
	}
	switch {
	case m.T == "push_token" && c.role == "phone":
		if !tokenRe.MatchString(m.Token) {
			log.Printf("bad push token id=%s", c.id[:6])
			return
		}
		h.mu.Lock()
		h.tokens[c.id] = m.Token
		h.mu.Unlock()
		log.Printf("push token id=%s", c.id[:6])
	case m.T == "notify" && c.role == "mac":
		h.mu.Lock()
		tok := h.tokens[c.id]
		h.mu.Unlock()
		if h.Notifier == nil || tok == "" {
			log.Printf("notify dropped id=%s notifier=%t token=%t", c.id[:6], h.Notifier != nil, tok != "")
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.Notifier.Push(ctx, tok, Alert{Title: m.Title, Body: m.Body, Block: m.Block, Options: m.Options}); err != nil {
				log.Printf("push fail id=%s err=%v", c.id[:6], err)
				return
			}
			log.Printf("push ok id=%s", c.id[:6])
		}()
	}
}

// notify tells c whether its peer is currently connected.
func (h *Hub) notify(c *conn) { h.sendPeer(c, h.peerOf(c) != nil) }

func (h *Hub) sendPeer(c *conn, present bool) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	msg := `{"t":"peer","present":false}`
	if present {
		msg = `{"t":"peer","present":true}`
	}
	h.send(c, msg)
}

// send writes one frame ([]byte → binary, string → text); on failure it kills c only.
func (h *Hub) send(c *conn, v any) {
	c.ws.SetWriteDeadline(time.Now().Add(h.writeTimeout))
	err := websocket.Message.Send(c.ws, v)
	c.ws.SetWriteDeadline(time.Time{}) // pongs are written from inside Receive and would hit a stale deadline
	if err != nil {
		log.Printf("write fail role=%s id=%s err=%v", c.role, c.id[:6], err)
		kill(c.ws)
	}
}

// kill unblocks any pending read/write on ws, then closes it.
func kill(ws *websocket.Conn) {
	ws.SetDeadline(time.Now())
	ws.Close()
}
