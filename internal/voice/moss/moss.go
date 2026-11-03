// Package moss is the MOSS-Live provider (模思智能, https://platform.moss.cn/docs/reference/moss-live).
// Tools work (undocumented, found 2026-11-03: session.tools, response.function_call_arguments.done,
// function_call_output items; protocol.md 4.5); there is no text input, so SendText fails and the
// session layer falls back to a transcript. It registers itself as voice provider "moss".
package moss

import "github.com/yang1997434/rex-voice/internal/voice"

func init() { voice.Register(voice.Moss, Dial) }
