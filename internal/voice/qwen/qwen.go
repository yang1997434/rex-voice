// Package qwen is the Alibaba Qwen-Omni Realtime provider (docs/protocol.md section 4.2,
// docs/research/2026-11-01-realtime-providers.md). It registers itself as voice provider "qwen".
package qwen

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Qwen, Dial) }
