// Package gemini is the Google Gemini Live API provider (docs/protocol.md section 4.3,
// docs/research/2026-11-01-realtime-providers.md). It registers itself as voice provider "gemini".
package gemini

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Gemini, Dial) }
