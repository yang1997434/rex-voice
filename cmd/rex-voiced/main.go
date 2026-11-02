// rex-voiced is the Mac service: phone ⇄ relay ⇄ here ⇄ OpenAI Realtime.
// `rex-voiced chat` is a text-mode console for checking the tool loop without a phone.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/yang1997434/rex-voice/internal/openai"
	"github.com/yang1997434/rex-voice/internal/tools"
)

var version = "dev" // set with -ldflags "-X main.version=…"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rex-voiced <pair|serve|chat> [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "pair":
		os.Exit(pairCmd(os.Args[2:]))
	case "serve":
		os.Exit(serve(os.Args[2:]))
	case "chat":
		os.Exit(chat(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(2)
	}
}

func chat(args []string) int {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	model := fs.String("model", "gpt-realtime-2.1", "OpenAI Realtime model")
	keep := fs.Bool("keep-tool-outputs", false, "do not delete tool items after each answer (A/B)")
	instrFile := fs.String("instructions-file", "", "use this file as the base prompt instead of the built-in one")
	debug := fs.Bool("debug", false, "log every OpenAI event type to stderr")
	fs.Parse(args)

	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set (OPENAI_API_KEY=$(flow-cred OPENAI_API_KEY) rex-voiced chat)")
		return 1
	}
	base := openai.Prompt
	if *instrFile != "" {
		b, err := os.ReadFile(*instrFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		base = string(b)
	}

	hung := make(chan string, 1)
	reg := &tools.Registry{}
	for _, t := range tools.NewRexTools(tools.Options{Audit: os.Stderr}) {
		reg.Register(t)
	}
	reg.Register(tools.HangUpDef(func(reason string) {
		select {
		case hung <- reason:
		default:
		}
	}))

	turnDone := make(chan struct{}, 1)
	cfg := openai.Config{
		Model:           *model,
		Key:             key,
		Instructions:    openai.WithLabels(base, rexLabels()),
		Tools:           reg.Defs(),
		TextOnly:        true,
		KeepToolOutputs: *keep,
		UsageLog:        usageLog(),
		Version:         version,
		Logger:          logger(*debug),
		Call: func(ctx context.Context, name, args string) string {
			t0 := time.Now()
			out := reg.Call(ctx, name, args)
			fmt.Fprintf(os.Stderr, "[tool] %s %s → %d bytes %d ms\n", name, args, len(out), time.Since(t0).Milliseconds())
			return out
		},
		Handlers: openai.Handlers{
			TextDelta:     func(d string) { fmt.Print(d) },
			AssistantText: func(string) { fmt.Println() },
			ResponseDone: func(u openai.Usage) {
				fmt.Fprintf(os.Stderr, "[usage] input=%d cached=%d text_in=%d audio_in=%d output=%d $%.5f\n",
					u.Input, u.Cached, u.TextIn, u.AudioIn, u.Output, u.USD)
			},
			TurnDone: func() {
				select {
				case turnDone <- struct{}{}:
				default:
				}
			},
			Error: func(err error) { fmt.Fprintln(os.Stderr, "[error]", err) },
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	s, err := openai.Dial(dctx, cfg)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer s.Close()

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	idle := true // wait for each answer before sending the next line, so piped input does not talk over the model
	for {
		in := lines
		if !idle {
			in = nil
		}
		select {
		case reason := <-hung:
			fmt.Fprintln(os.Stderr, "已挂断", reason)
			return 0
		case <-s.Done():
			return 1
		case <-ctx.Done():
			return 130
		case <-turnDone:
			idle = true
		case line, ok := <-in:
			if !ok {
				return 0
			}
			if line == "" {
				continue
			}
			fmt.Fprintln(os.Stderr, ">", line)
			if err := s.SendText(line); err != nil {
				fmt.Fprintln(os.Stderr, "[error]", err)
				return 1
			}
			idle = false
		}
	}
}

func logger(debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// rexLabels lists Rex session labels for the prompt; failure only warns.
func rexLabels() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "rex", "ls", "--json").Output()
	var v struct {
		Sessions []struct{ Label string }
	}
	if err == nil {
		err = json.Unmarshal(out, &v)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: rex ls failed, prompt has no session labels:", err)
		return nil
	}
	var labels []string
	for _, s := range v.Sessions {
		labels = append(labels, s.Label)
	}
	return labels
}

func usageLog() io.Writer { return logFile("usage.log") }
