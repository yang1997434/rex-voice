// Package grok is the xAI Grok Voice Agent API provider (docs/research/2026-11-01-realtime-voice-market.md).
package grok

import (
	"context"
	"errors"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func init() { voice.Register(voice.Grok, Dial) }

func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	return nil, errors.New("grok: 还没实现")
}
