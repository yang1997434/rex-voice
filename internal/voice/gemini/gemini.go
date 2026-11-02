// Package gemini is the Google Gemini Live API provider (docs/research/2026-11-01-realtime-voice-market.md).
package gemini

import (
	"context"
	"errors"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func init() { voice.Register(voice.Gemini, Dial) }

func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	return nil, errors.New("gemini: 还没实现")
}
