// Package qwen is the Alibaba Qwen-Omni Realtime provider (docs/research/2026-11-01-realtime-providers.md).
package qwen

import (
	"context"
	"errors"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func init() { voice.Register(voice.Qwen, Dial) }

func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	return nil, errors.New("qwen: 还没实现")
}
