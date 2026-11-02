// Package grok is the xAI Grok Voice Agent API provider (docs/protocol.md section 4.4,
// docs/research/2026-11-01-realtime-providers.md). The wire protocol is OpenAI Realtime GA event names,
// so client.go started as a copy of internal/voice/openai. It registers itself as voice provider "grok".
package grok

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Grok, Dial) }
