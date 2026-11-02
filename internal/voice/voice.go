// Package voice is the realtime speech-to-speech provider interface (docs/protocol.md section 4).
// The session layer (internal/call) only talks to Session; each provider lives in a subpackage
// that calls Register from init. Audio on this interface is always PCM16 mono 24 kHz both ways;
// a provider that wants another rate resamples inside (see Down24to16).
package voice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/yang1997434/rex-voice/internal/tools"
)

type Provider string

const (
	OpenAI Provider = "openai"
	Qwen   Provider = "qwen"
	Gemini Provider = "gemini"
	Grok   Provider = "grok"
)

type Config struct {
	Provider                        Provider
	Model, Voice, Key, Instructions string // empty Model / Voice take Defaults(Provider); Key is never logged
	Tools                           []tools.ToolDef
	Call                            func(ctx context.Context, name, args string) string // tools.Registry.Call
	TextOnly                        bool
	Transcribe                      bool // report the user's speech as UserText
	Logger                          *slog.Logger
	Handlers                        Handlers
	UsageLog                        io.Writer // one JSON line per response
	Version                         string    // User-Agent rex-voiced/<Version>, default "dev"
	// Extra is provider specific: openai "keep_tool_outputs", "url"; qwen "region", "workspace".
	Extra map[string]string
}

// Handlers are called serially on the provider's event goroutine; keep them short.
type Handlers struct {
	AudioDelta    func(pcm []byte) // PCM16 24 kHz; audio of an interrupted answer is already dropped
	TextDelta     func(string)
	AssistantText func(text string) // final text of one answer
	UserText      func(string)      // transcript of the user's speech
	SpeechStarted func()            // the user started talking: stop playback
	ResponseDone  func(Usage)
	TurnDone      func() // the model is done answering (no tool calls left to run)
	Error         func(error)
}

type Usage struct {
	Input, Cached, AudioIn, TextIn, Output, AudioOut, TextOut int
	USD                                                       float64 // estimate; 0 when Unpriced
	Unpriced                                                  bool
}

type Session interface {
	AppendAudio(pcm []byte) error
	SendText(text string) error // a user text turn, answered right away
	Cancel() error              // stop the current answer
	SetMuted(muted bool) error  // the phone stopped / resumed sending real audio
	Close() error
	Done() <-chan struct{} // closed when the session has stopped (read error or Close)
	Age() time.Duration
	MaxAge() time.Duration // provider's session cap; 0 = no cap
	RecentTurns(n int) []Turn
}

type Turn struct{ Role, Text string } // Role is "user" or "assistant"

var ErrUnknownProvider = errors.New("voice: unknown provider")

var defaults = map[Provider][2]string{
	OpenAI: {"gpt-realtime-2.1", "marin"},
	Qwen:   {"qwen3.8-omni-flash-realtime", "Tina"},
	Gemini: {"gemini-3.8-live", "Kore"},
	Grok:   {"grok-voice-3.0", "Ara"},
}

// Defaults is the provider's default model and voice; both empty for an unknown provider.
func Defaults(p Provider) (model, voice string) {
	d := defaults[p]
	return d[0], d[1]
}

var (
	mu      sync.Mutex
	dialers = map[Provider]func(context.Context, Config) (Session, error){}
)

// Register is called from a provider package's init.
func Register(p Provider, dial func(context.Context, Config) (Session, error)) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := dialers[p]; dup {
		panic("voice: duplicate provider " + string(p))
	}
	dialers[p] = dial
}

// Dial fills Model / Voice from Defaults and connects through the registered provider.
func Dial(ctx context.Context, cfg Config) (Session, error) {
	model, v := Defaults(cfg.Provider)
	if model == "" {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, cfg.Provider)
	}
	mu.Lock()
	dial := dialers[cfg.Provider]
	mu.Unlock()
	if dial == nil {
		return nil, fmt.Errorf("voice: %s 还没实现", cfg.Provider)
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("voice: 没有 %s 的 API key：在手机设置里填，或在 Mac 上运行 rex-voiced setup -provider %s", cfg.Provider, cfg.Provider)
	}
	if cfg.Model == "" {
		cfg.Model = model
	}
	if cfg.Voice == "" {
		cfg.Voice = v
	}
	return dial(ctx, cfg)
}
