package relay

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Alert is one push. Block set (a Claude waiting on a question) makes it actionable: category
// claude-ask, with block and options as custom keys the app answers with (protocol section 2).
type Alert struct {
	Title, Body string
	Block       string
	Options     []string
}

// Notifier sends one alert push to a device token.
type Notifier interface {
	Push(ctx context.Context, token string, a Alert) error
}

const (
	apnsDefaultURL = "https://api.push.apple.com"
	apnsTopic      = "com.maruiao.rexvoice"
	jwtTTL         = 50 * time.Minute // Apple rejects provider tokens older than 60 min
)

// APNS pushes through Apple's HTTP/2 provider API with a token (.p8) key.
type APNS struct {
	key         *ecdsa.PrivateKey
	keyID, team string
	url         string
	client      *http.Client
	mu          sync.Mutex
	jwt         string
	jwtIssued   time.Time
	now         func() time.Time
}

// NewAPNS loads the PKCS#8 .p8 key at keyPath; url "" means production APNs.
func NewAPNS(keyPath, keyID, teamID, url string) (*APNS, error) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	der := raw
	if b, _ := pem.Decode(raw); b != nil {
		der = b.Bytes
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("apns key: %w", err)
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apns key: not an ECDSA key")
	}
	if url == "" {
		url = apnsDefaultURL
	}
	return &APNS{key: ec, keyID: keyID, team: teamID, url: strings.TrimRight(url, "/"), client: &http.Client{}, now: time.Now}, nil
}

// token returns the cached ES256 provider JWT, re-signing it every jwtTTL.
func (a *APNS) token() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.jwt != "" && now.Sub(a.jwtIssued) < jwtTTL {
		return a.jwt, nil
	}
	enc := base64.RawURLEncoding
	hdr, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.keyID})
	claims, _ := json.Marshal(map[string]any{"iss": a.team, "iat": now.Unix()})
	signing := enc.EncodeToString(hdr) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // JWS ES256 wants raw r||s, not ASN.1
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.jwt, a.jwtIssued = signing+"."+enc.EncodeToString(sig), now
	return a.jwt, nil
}

func (a *APNS) Push(ctx context.Context, token string, al Alert) error {
	jwt, err := a.token()
	if err != nil {
		return err
	}
	aps := map[string]any{"alert": map[string]string{"title": al.Title, "body": al.Body}, "sound": "default"}
	p := map[string]any{"aps": aps}
	if al.Block != "" {
		aps["category"], aps["mutable-content"] = "claude-ask", 0
		p["block"], p["options"] = al.Block, al.Options
		if al.Options == nil {
			p["options"] = []string{}
		}
	}
	payload, _ := json.Marshal(p)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+"/3/device/"+token, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", apnsTopic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	req.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct{ Reason string }
	json.Unmarshal(b, &e)
	return fmt.Errorf("apns %d %s", resp.StatusCode, e.Reason)
}
