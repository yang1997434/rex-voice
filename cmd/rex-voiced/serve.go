package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/yang1997434/rex-voice/internal/call"
	"github.com/yang1997434/rex-voice/internal/openai"
	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/tools"
)

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	relayDefault := defaultRelay
	if v := os.Getenv("REX_VOICE_RELAY"); v != "" {
		relayDefault = v
	}
	relay := fs.String("relay", relayDefault, "relay WebSocket URL (env REX_VOICE_RELAY overrides the default)")
	model := fs.String("model", "gpt-realtime-2.1", "OpenAI Realtime model")
	echo := fs.Bool("echo", false, "no OpenAI: echo phone audio back after 1 s (for iOS bring-up)")
	keep := fs.Bool("keep-tool-outputs", false, "do not delete tool items after each answer (A/B)")
	debug := fs.Bool("debug", false, "debug logging")
	fs.Parse(args)

	path := pairPath()
	p, err := pair.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "还没配对：先运行 rex-voiced pair")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" && !*echo {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set (OPENAI_API_KEY=$(flow-cred OPENAI_API_KEY) rex-voiced serve)")
		return 1
	}

	reg := &tools.Registry{}
	for _, t := range tools.NewRexTools(tools.Options{Audit: logFile("audit.log")}) {
		reg.Register(t)
	}
	cfg := call.Config{
		Pairing:     &p,
		PairingPath: path,
		RelayURL:    *relay,
		Tools:       reg,
		Echo:        *echo,
		Logger:      logger(*debug),
		Version:     version,
	}
	if !*echo {
		cfg.OpenAI = openai.Config{
			Model:           *model,
			Key:             key,
			Instructions:    openai.WithLabels(openai.Prompt, rexLabels()),
			Transcribe:      true,
			KeepToolOutputs: *keep,
			UsageLog:        usageLog(),
			Logger:          cfg.Logger,
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg.Logger.Info("rex-voiced serve", "version", version, "relay", *relay, "echo", *echo, "model", *model)
	if err := call.Run(ctx, cfg); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// logFile opens ~/Library/Logs/rex-voice/<name> for appending, creating the directory 0700.
// It returns a nil interface (not a nil *os.File) on failure, after a warning.
func logFile(name string) io.Writer {
	home, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(home, "Library", "Logs", "rex-voice")
		if err = os.MkdirAll(dir, 0o700); err == nil {
			var f *os.File
			if f, err = os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				return f
			}
		}
	}
	fmt.Fprintln(os.Stderr, "warning: no", name+":", err)
	return nil
}
