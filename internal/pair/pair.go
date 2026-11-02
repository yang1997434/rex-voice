// Package pair holds the phone-Mac pairing secret (id + key), its QR code,
// and the per-direction frame cipher described in docs/protocol.md sections 1 and 3.
package pair

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rsc.io/qr"
)

type Pairing struct {
	ID       [16]byte
	Key      [32]byte
	PairedAt time.Time // zero = no phone has completed a handshake yet
	DeviceID string    // device_id from the first accepted hello; empty until then
}

var b64 = base64.RawURLEncoding

func Generate() (Pairing, error) {
	var p Pairing
	if _, err := rand.Read(p.ID[:]); err != nil {
		return p, err
	}
	if _, err := rand.Read(p.Key[:]); err != nil {
		return p, err
	}
	return p, nil
}

func (p *Pairing) MarkPaired() { p.PairedAt = time.Now() }

type fileFormat struct {
	ID       string    `json:"id"`
	Key      string    `json:"key"`
	PairedAt time.Time `json:"paired_at,omitzero"`
	DeviceID string    `json:"device_id,omitempty"`
}

// Load returns the os.Stat error unchanged when the file is missing (errors.Is(err, os.ErrNotExist)),
// and refuses files whose permissions are not exactly 0600.
func Load(path string) (Pairing, error) {
	var p Pairing
	fi, err := os.Stat(path)
	if err != nil {
		return p, err
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		return p, fmt.Errorf("%s: permissions %#o, want 0600", path, perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	var f fileFormat
	if err := json.Unmarshal(raw, &f); err != nil {
		return p, fmt.Errorf("%s: %w", path, err)
	}
	if err := decodeInto(p.ID[:], f.ID); err != nil {
		return p, fmt.Errorf("%s: id: %w", path, err)
	}
	if err := decodeInto(p.Key[:], f.Key); err != nil {
		return p, fmt.Errorf("%s: key: %w", path, err)
	}
	p.PairedAt, p.DeviceID = f.PairedAt, f.DeviceID
	return p, nil
}

func decodeInto(dst []byte, s string) error {
	b, err := b64.DecodeString(s)
	if err != nil {
		return err
	}
	if len(b) != len(dst) {
		return fmt.Errorf("got %d bytes, want %d", len(b), len(dst))
	}
	copy(dst, b)
	return nil
}

// Save writes atomically (temp file + rename) with mode 0600, creating the directory 0700 if needed.
func (p Pairing) Save(path string) error {
	raw, err := json.MarshalIndent(fileFormat{b64.EncodeToString(p.ID[:]), b64.EncodeToString(p.Key[:]), p.PairedAt, p.DeviceID}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pair-*.json") // CreateTemp makes the file 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// URL is the QR payload; id and key are unpadded base64url. An empty invite is left out.
func (p Pairing) URL(relayWS, invite string) string {
	u := "rexvoice://pair?relay=" + url.QueryEscape(relayWS) +
		"&id=" + b64.EncodeToString(p.ID[:]) + "&key=" + b64.EncodeToString(p.Key[:])
	if invite != "" {
		u += "&invite=" + url.QueryEscape(invite)
	}
	return u
}

const quiet = 2 // quiet-zone modules on each side

// QRText renders url as a terminal QR code with half-block characters (one text row = two modules).
// Light modules are drawn as glyphs and dark modules as blanks, so on a dark terminal it reads as a
// normal dark-on-light code. It panics only if url exceeds QR capacity, which the ~140-byte pairing URL never does.
func QRText(url string) string {
	c, err := qr.Encode(url, qr.M)
	if err != nil {
		panic(fmt.Sprintf("pair: QR encode %d bytes: %v", len(url), err))
	}
	light := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if y >= c.Size+quiet { // padding row below the code when the height is odd
			return false
		}
		return !c.Black(x, y) // Black is false outside the code, so the quiet zone is light
	}
	n := c.Size + 2*quiet
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			switch top, bot := light(x, y), light(x, y+1); {
			case top && bot:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bot:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
