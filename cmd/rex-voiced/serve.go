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
	"github.com/yang1997434/rex-voice/internal/config"
	"github.com/yang1997434/rex-voice/internal/openai"
	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/tools"
)

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dirFlag := configDirFlag(fs)
	relay := fs.String("relay", "", "relay WebSocket URL (default: config.json relay)")
	model := fs.String("model", "", "OpenAI Realtime model (default: config.json model)")
	echo := fs.Bool("echo", false, "no OpenAI: echo phone audio back after 1 s (for iOS bring-up)")
	keep := fs.Bool("keep-tool-outputs", false, "do not delete tool items after each answer (A/B)")
	debug := fs.Bool("debug", false, "debug logging")
	fs.Parse(args)

	dir, c, err := loadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *relay == "" {
		*relay = c.Relay
	}
	if *model == "" {
		*model = c.Model
	}
	path := pairPath(dir)
	p, err := pair.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "还没配对：先运行 rex-voiced pair")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	log := logger(*debug)
	var key string
	if !*echo {
		var src string
		if key, src, err = config.OpenAIKey(os.Getenv, config.OpenAIKeychain()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		log.Info("openai key: " + src)
	}

	audit := logFile("audit.log")
	reg := &tools.Registry{}
	for _, t := range withForbidExtra(tools.NewRexTools(tools.Options{Audit: audit}), c.ForbidExtra, audit) {
		reg.Register(t)
	}
	cfg := call.Config{
		Pairing:     &p,
		PairingPath: path,
		RelayURL:    *relay,
		Tools:       reg,
		Echo:        *echo,
		Logger:      log,
		Version:     version,
	}
	if !*echo {
		cfg.Instructions = func() string { return openai.Build(c.Name, c.ProjectAliases, projectLabels()) }
		cfg.OpenAI = openai.Config{
			Model:           *model,
			Key:             key,
			Voice:           c.Voice,
			Transcribe:      true,
			KeepToolOutputs: *keep,
			UsageLog:        usageLog(),
			Logger:          cfg.Logger,
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg.Logger.Info("rex-voiced serve", "version", version, "config", dir, "relay", *relay, "echo", *echo, "model", *model)
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
