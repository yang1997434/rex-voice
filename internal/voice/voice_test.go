package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDial(t *testing.T) {
	var got Config
	Register(OpenAI, func(_ context.Context, cfg Config) (Session, error) { got = cfg; return nil, nil })
	if _, err := Dial(context.Background(), Config{Provider: OpenAI, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-realtime-2.1" || got.Voice != "marin" {
		t.Fatalf("defaults not filled: %+v", got)
	}
	got = Config{}
	if _, err := Dial(context.Background(), Config{Provider: OpenAI, Model: "m", Voice: "v", Key: "k"}); err != nil || got.Model != "m" || got.Voice != "v" {
		t.Fatalf("explicit model/voice: %+v %v", got, err)
	}
	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{Provider: "nope", Key: "k"}, "unknown provider"},
		{Config{Provider: Qwen, Key: "k"}, "还没实现"},
		{Config{Provider: OpenAI}, "没有 openai 的 API key"},
	} {
		got = Config{}
		_, err := Dial(context.Background(), tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) || got.Provider != "" {
			t.Errorf("%+v: err = %v, dialed %v", tc.cfg, err, got.Provider != "")
		}
	}
	if _, err := Dial(context.Background(), Config{Provider: "nope"}); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("err = %v, want ErrUnknownProvider", err)
	}
}
