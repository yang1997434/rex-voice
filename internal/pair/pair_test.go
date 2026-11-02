package pair

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsc.io/qr"
)

func testKey() (k [32]byte) {
	for i := range k {
		k[i] = byte(i)
	}
	return
}

// Session salts used by the tests and by the docs/protocol.md vectors.
var phoneSalt, macSalt = seqBytes(0x10), seqBytes(0x20)

func seqBytes(start byte) (b [16]byte) {
	for i := range b {
		b[i] = start + byte(i)
	}
	return
}

func TestSaltChangesKey(t *testing.T) {
	frame := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Seal(1, []byte("x"))
	other := macSalt
	other[0] ^= 1
	if _, _, _, err := NewCipher(testKey(), PhoneToMac, phoneSalt, other).Open(frame); !errors.Is(err, ErrFrame) {
		t.Fatalf("frame opened under a different mac salt: err=%v", err)
	}
	if _, _, _, err := NewCipher(testKey(), PhoneToMac, macSalt, phoneSalt).Open(frame); !errors.Is(err, ErrFrame) {
		t.Fatalf("frame opened with salts swapped: err=%v", err)
	}
}

func TestSaltFrame(t *testing.T) {
	f := SaltFrame(macSalt)
	if hex.EncodeToString(f) != "0200000000202122232425262728292a2b2c2d2e2f" {
		t.Fatalf("SaltFrame = %x", f)
	}
	if got, ok := ParseSaltFrame(f); !ok || got != macSalt {
		t.Fatalf("ParseSaltFrame = %x %v", got, ok)
	}
	for name, bad := range map[string][]byte{
		"short":     f[:20],
		"long":      append(append([]byte{}, f...), 0),
		"type":      append([]byte{3}, f[1:]...),
		"seq":       append([]byte{2, 0, 0, 0, 1}, f[5:]...),
		"keepalive": KeepaliveFrame(),
	} {
		if _, ok := ParseSaltFrame(bad); ok {
			t.Errorf("%s: ParseSaltFrame accepted %x", name, bad)
		}
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	tx, rx := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt), NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt)
	for i, msg := range []string{"a", "", `{"t":"ping"}`} {
		typ, seq, plain, err := rx.Open(tx.Seal(1, []byte(msg)))
		if err != nil || typ != 1 || seq != uint32(i+1) || string(plain) != msg {
			t.Fatalf("frame %d: typ=%d seq=%d plain=%q err=%v", i, typ, seq, plain, err)
		}
	}
}

func TestDirectionsUseDifferentKeys(t *testing.T) {
	frame := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Seal(1, []byte("x"))
	if _, _, _, err := NewCipher(testKey(), MacToPhone, phoneSalt, macSalt).Open(frame); !errors.Is(err, ErrFrame) {
		t.Fatalf("opened phone->mac frame with mac->phone cipher: err=%v", err)
	}
	a := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Seal(1, []byte("same"))
	b := NewCipher(testKey(), MacToPhone, phoneSalt, macSalt).Seal(1, []byte("same"))
	if bytes.Equal(a[5:], b[5:]) {
		t.Fatal("both directions produced identical ciphertext")
	}
}

func TestTamperFails(t *testing.T) {
	for name, mut := range map[string]func([]byte){
		"type":       func(f []byte) { f[0] ^= 1 },
		"ciphertext": func(f []byte) { f[5] ^= 1 },
		"tag":        func(f []byte) { f[len(f)-1] ^= 1 },
	} {
		f := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Seal(1, []byte("hello"))
		mut(f)
		if _, _, _, err := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Open(f); !errors.Is(err, ErrFrame) {
			t.Errorf("%s tampered: err=%v, want ErrFrame", name, err)
		}
	}
	if _, _, _, err := NewCipher(testKey(), PhoneToMac, phoneSalt, macSalt).Open([]byte{1, 0, 0, 0, 1}); !errors.Is(err, ErrFrame) {
		t.Errorf("short frame: err=%v", err)
	}
}

func TestReplay(t *testing.T) {
	tx, rx := NewCipher(testKey(), MacToPhone, phoneSalt, macSalt), NewCipher(testKey(), MacToPhone, phoneSalt, macSalt)
	f1, f2 := tx.Seal(1, []byte("1")), tx.Seal(1, []byte("2"))
	if _, _, _, err := rx.Open(f2); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := rx.Open(f2); !errors.Is(err, ErrReplay) {
		t.Errorf("duplicate: err=%v, want ErrReplay", err)
	}
	if _, _, _, err := rx.Open(f1); !errors.Is(err, ErrReplay) {
		t.Errorf("rollback: err=%v, want ErrReplay", err)
	}
	// A forged frame with a higher seq must not advance the window.
	forged := tx.Seal(1, []byte("3"))
	forged[len(forged)-1] ^= 1
	if _, _, _, err := rx.Open(forged); !errors.Is(err, ErrFrame) {
		t.Fatalf("forged: err=%v", err)
	}
	forged[len(forged)-1] ^= 1
	if _, seq, _, err := rx.Open(forged); err != nil || seq != 3 {
		t.Errorf("genuine seq 3 after forged one: seq=%d err=%v", seq, err)
	}
}

// Vectors are mirrored in docs/protocol.md section 3 "测试向量" (all with phoneSalt 0x10..0x1f and
// macSalt 0x20..0x2f); they were cross-checked against Python's cryptography (HKDF + ChaCha20Poly1305) on 2026-11-01.
var vectors = []struct {
	key   string
	dir   Direction
	seq   uint32
	typ   byte
	plain string // hex
	frame string // hex
}{
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", PhoneToMac, 1, 1,
		hex.EncodeToString([]byte(`{"t":"hello","app":"1.0","device":"iPhone"}`)),
		"0100000001cf157aea72a115abc5c127fdb119375ca71ca651a2ef7a09586971c4b055cb5ab9d484c0feac3a2cb3184af2492fcf576c2b891fb06fb968fa8e32"},
	{"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", MacToPhone, 2, 1,
		hex.EncodeToString([]byte(`{"t":"ping"}`)),
		"0100000002bb7fdebff7ddfc8ac040de173242f85d62a57994c8c9ed473578f5ff"},
	{"4242424242424242424242424242424242424242424242424242424242424242", PhoneToMac, 0x01020304, 0,
		"0001020304050607",
		"000102030427cab922deb03fa6d2de530290b8ced22f46ef267361583b"},
}

func TestVectors(t *testing.T) {
	doc, err := os.ReadFile("../../docs/protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vectors {
		var key [32]byte
		kb, _ := hex.DecodeString(v.key)
		copy(key[:], kb)
		plain, _ := hex.DecodeString(v.plain)
		c := NewCipher(key, v.dir, phoneSalt, macSalt)
		c.seq = v.seq - 1
		if got := hex.EncodeToString(c.Seal(v.typ, plain)); got != v.frame {
			t.Errorf("vector %d: frame %s, want %s", i, got, v.frame)
		}
		frame, _ := hex.DecodeString(v.frame)
		typ, seq, got, err := NewCipher(key, v.dir, phoneSalt, macSalt).Open(frame)
		if err != nil || typ != v.typ || seq != v.seq || !bytes.Equal(got, plain) {
			t.Errorf("vector %d: open typ=%d seq=%d plain=%x err=%v", i, typ, seq, got, err)
		}
		if !bytes.Contains(doc, []byte(v.frame)) {
			t.Errorf("vector %d frame missing from docs/protocol.md", i)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "pair.json")
	if _, err := Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: err=%v, want ErrNotExist", err)
	}
	p, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == ([16]byte{}) || p.Key == ([32]byte{}) {
		t.Fatal("Generate left id or key zero")
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		fi, err := os.Stat(f)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode=%v err=%v, want %#o", f, fi.Mode().Perm(), err, want)
		}
	}
	got, err := Load(path)
	if err != nil || got != p {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, p)
	}

	p.MarkPaired()
	p.DeviceID = "6F1C2A90-0000-4000-8000-000000000001"
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil || !got.PairedAt.Equal(p.PairedAt) || got.PairedAt.IsZero() || got.DeviceID != p.DeviceID {
		t.Fatalf("PairedAt/DeviceID round trip: %v %q / %v %q err=%v", got.PairedAt, got.DeviceID, p.PairedAt, p.DeviceID, err)
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Errorf("0644 file: err=%v, want permission error", err)
	}
}

func TestURL(t *testing.T) {
	p, _ := Generate()
	relay := "wss://r.maruiao.top/_voice/ws"
	u, err := url.Parse(p.URL(relay))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "rexvoice" || u.Host != "pair" || q.Get("relay") != relay {
		t.Fatalf("parsed %+v", u)
	}
	var id [16]byte
	var key [32]byte
	if decodeInto(id[:], q.Get("id")) != nil || decodeInto(key[:], q.Get("key")) != nil || id != p.ID || key != p.Key {
		t.Fatalf("id/key did not round trip: %q %q", q.Get("id"), q.Get("key"))
	}
}

// QRText must be a faithful picture of the code: light glyph halves where modules are white,
// a 2-module light quiet zone, and one text row per two module rows.
func TestQRText(t *testing.T) {
	p, _ := Generate()
	u := p.URL("wss://r.maruiao.top/_voice/ws")
	c, err := qr.Encode(u, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(QRText(u), "\n"), "\n")
	n := c.Size + 2*quiet
	if len(rows) != (n+1)/2 {
		t.Fatalf("%d rows, want %d", len(rows), (n+1)/2)
	}
	for ry, row := range rows {
		cells := []rune(row)
		if len(cells) != n {
			t.Fatalf("row %d: %d cells, want %d", ry, len(cells), n)
		}
		for x, ch := range cells {
			for half, y := range []int{2 * ry, 2*ry + 1} {
				wantLight := y < n && !c.Black(x-quiet, y-quiet)
				gotLight := ch == '█' || (half == 0 && ch == '▀') || (half == 1 && ch == '▄')
				if gotLight != wantLight {
					t.Fatalf("module (%d,%d): light=%v want %v (char %q)", x, y, gotLight, wantLight, ch)
				}
			}
		}
	}
	t.Logf("url %d bytes, QR version size %d modules", len(u), c.Size)
}
