// Package doubao is the ByteDance Doubao Realtime 3.0 (Seeduplex) provider (docs/research/2026-11-01-realtime-providers.md).
package doubao

import (
	"context"
	"errors"

	"github.com/yang1997434/rex-voice/internal/voice"
)

func init() { voice.Register(voice.Doubao, Dial) }

func Dial(ctx context.Context, cfg voice.Config) (voice.Session, error) {
	return nil, errors.New("doubao: 还没实现")
}
