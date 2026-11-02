package pair

import (
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

type Direction uint32

const (
	PhoneToMac Direction = 0
	MacToPhone Direction = 1
)

const headerLen = 5 // [type][seq BE32]

const (
	TypeAudio     = 0
	TypeControl   = 1
	TypeSalt      = 2 // plaintext [02][00000000][16-byte salt]
	TypeKeepalive = 3 // plaintext [03][00000000]
	TypeAttach    = 4 // encrypted [4B id BE][4B index BE][data ≤ 60000 B], after an attach control
)

// SaltFrame is the plaintext type 2 frame announcing this side's session salt.
func SaltFrame(salt [16]byte) []byte {
	return append([]byte{TypeSalt, 0, 0, 0, 0}, salt[:]...)
}

// ParseSaltFrame accepts only an exact 21-byte type 2 frame with a zero seq.
func ParseSaltFrame(frame []byte) (salt [16]byte, ok bool) {
	if len(frame) != headerLen+16 || frame[0] != TypeSalt || [4]byte(frame[1:headerLen]) != [4]byte{} {
		return salt, false
	}
	return [16]byte(frame[headerLen:]), true
}

// KeepaliveFrame is the plaintext type 3 frame each side sends when idle.
func KeepaliveFrame() []byte { return []byte{TypeKeepalive, 0, 0, 0, 0} }

var (
	ErrReplay = errors.New("pair: frame seq not greater than previous")
	ErrFrame  = errors.New("pair: frame too short or failed authentication")
)

// Cipher seals or opens frames for one direction. One Cipher per direction per side;
// not safe for concurrent use.
type Cipher struct {
	aead cipher.AEAD
	dir  Direction
	seq  uint32 // Seal: last sealed seq; Open: last accepted seq. First frame is seq 1.
}

// NewCipher derives this connection's key for one direction from the long-term key and both
// session salts (docs/protocol.md section 3), so every connection gets fresh nonces.
func NewCipher(key [32]byte, dir Direction, phoneSalt, macSalt [16]byte) *Cipher {
	info := "phone->mac"
	if dir == MacToPhone {
		info = "mac->phone"
	}
	var k [chacha20poly1305.KeySize]byte
	salt := append(phoneSalt[:], macSalt[:]...)
	if _, err := io.ReadFull(hkdf.New(sha256.New, key[:], salt, []byte(info)), k[:]); err != nil {
		panic(err) // HKDF-SHA256 can always produce 32 bytes
	}
	aead, err := chacha20poly1305.New(k[:])
	if err != nil {
		panic(err) // key size is fixed
	}
	return &Cipher{aead: aead, dir: dir}
}

func (c *Cipher) nonce(seq uint32) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint32(n[4:], uint32(c.dir))
	binary.BigEndian.PutUint32(n[8:], seq)
	return n
}

// Seal returns [type][seq BE32][ciphertext+tag]. It panics rather than wrap seq, since a wrapped seq reuses a nonce.
func (c *Cipher) Seal(typ byte, plain []byte) []byte {
	if c.seq == ^uint32(0) {
		panic("pair: seq exhausted, re-pair or rekey")
	}
	c.seq++
	out := make([]byte, headerLen, headerLen+len(plain)+c.aead.Overhead())
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:], c.seq)
	return c.aead.Seal(out, c.nonce(c.seq), plain, out[:1])
}

// Open verifies and decrypts a frame. A seq not strictly greater than the last accepted one
// returns ErrReplay; the last seq only advances on a frame that authenticates.
func (c *Cipher) Open(frame []byte) (typ byte, seq uint32, plain []byte, err error) {
	if len(frame) < headerLen+c.aead.Overhead() {
		return 0, 0, nil, ErrFrame
	}
	typ, seq = frame[0], binary.BigEndian.Uint32(frame[1:headerLen])
	if seq <= c.seq {
		return typ, seq, nil, ErrReplay
	}
	plain, err = c.aead.Open(nil, c.nonce(seq), frame[headerLen:], frame[:1])
	if err != nil {
		return typ, seq, nil, ErrFrame
	}
	c.seq = seq
	return typ, seq, plain, nil
}
