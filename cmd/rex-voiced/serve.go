package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/yang1997434/rex-voice/internal/call"
	"github.com/yang1997434/rex-voice/internal/config"
	"github.com/yang1997434/rex-voice/internal/pair"
	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
	_ "github.com/yang1997434/rex-voice/internal/voice/gemini" // registers provider "gemini"
	_ "github.com/yang1997434/rex-voice/internal/voice/grok"   // registers provider "grok"
	_ "github.com/yang1997434/rex-voice/internal/voice/moss"   // registers provider "moss"
	_ "github.com/yang1997434/rex-voice/internal/voice/openai" // registers provider "openai"
	_ "github.com/yang1997434/rex-voice/internal/voice/qwen"   // registers provider "qwen"
)

func serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dirFlag := configDirFlag(fs)
	relay := fs.String("relay", "", "relay WebSocket URL (default: config.json relay)")
	model := fs.String("model", "", "model of config.json's provider (default: config.json model, else the provider's default)")
	echo := fs.Bool("echo", false, "no voice provider: echo phone audio back after 1 s (for iOS bring-up)")
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
	if m, _ := voice.Defaults(voice.Provider(c.Provider)); m == "" {
		fmt.Fprintln(os.Stderr, "config.json provider 不支持：", c.Provider)
		return 1
	}
	if !*echo {
		// Only a warning: the phone may bring its own key with call.start.
		if _, src, err := config.ProviderKey(os.Getenv, config.ProviderKeychain(c.Provider), c.Provider); err != nil {
			log.Warn("no fallback key on this Mac, calls need the phone's key", "provider", c.Provider, "err", err)
		} else {
			log.Info(c.Provider + " key: " + src) // README 常见问题 points at "openai key: keychain"
		}
	}

	audit := logFile("audit.log")
	reg := &tools.Registry{}
	for _, t := range tools.NewRexTools(tools.Options{Audit: audit, ForbidExtra: c.ForbidExtra}) {
		reg.Register(t)
	}
	cfg := call.Config{
		Pairing:     &p,
		PairingPath: path,
		RelayURL:    *relay,
		Invite:      c.Invite,
		Tools:       reg,
		Echo:        *echo,
		Watch:       true, // tell the user when a Claude pane turns blocked; off with -echo
		Logger:      log,
		Version:     version,
	}
	if !*echo {
		cfg.Instructions = func(name string) string {
			return voice.Build(cmp.Or(name, c.Name), c.ProjectAliases, projectLabels())
		}
		cfg.Voice = voice.Config{
			Provider:   voice.Provider(c.Provider),
			Model:      *model,
			Voice:      c.Voice,
			Transcribe: true,
			Extra:      keepExtra(*keep),
			UsageLog:   usageLog(),
			Logger:     cfg.Logger,
		}
		// Looked up per call, so a key stored after start is picked up without a restart.
		cfg.Key = func(p voice.Provider) string {
			key, _, err := config.ProviderKey(os.Getenv, config.ProviderKeychain(string(p)), string(p))
			if err != nil {
				log.Warn("no fallback key", "provider", p, "err", err)
			}
			return key
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg.Logger.Info("rex-voiced serve", "version", version, "config", dir, "relay", strings.SplitN(*relay, "?", 2)[0], "echo", *echo, "provider", c.Provider, "model", *model)
	if err := call.Run(ctx, cfg); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// keepExtra is the openai provider's switch to keep tool items (A/B).
func keepExtra(keep bool) map[string]string {
	if !keep {
		return nil
	}
	return map[string]string{"keep_tool_outputs": "true"}
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
