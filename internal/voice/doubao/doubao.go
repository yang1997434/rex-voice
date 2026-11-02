// Package doubao is the ByteDance Doubao Realtime 3.0 (Seeduplex) provider (docs/protocol.md section 4.3,
// docs/research/2026-11-01-realtime-providers.md). Implementation in client.go, prices in prices.go.
package doubao

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Doubao, Dial) }
