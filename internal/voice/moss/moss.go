// Package moss is the MOSS-Live provider (模思智能, https://platform.moss.cn/docs/reference/moss-live).
// It only listens and talks: no tools and no text input, so it ignores cfg.Tools / cfg.Instructions,
// SendText fails (the session layer falls back to a transcript) and a hang-up phrase in the user's
// transcript calls hang_up for the model. It registers itself as voice provider "moss".
package moss

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Moss, Dial) }
