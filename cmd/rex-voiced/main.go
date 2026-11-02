// rex-voiced is the Mac service: phone ⇄ relay ⇄ here ⇄ a realtime voice provider (internal/voice).
// setup → pair → install gets a new Mac running; `rex-voiced chat` is a text-mode console
// for checking the tool loop without a phone.
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
	"strings"
	"time"

	"github.com/yang1997434/rex-voice/internal/config"
	"github.com/yang1997434/rex-voice/internal/tools"
	"github.com/yang1997434/rex-voice/internal/voice"
)

var version = "dev" // set with -ldflags "-X main.version=…"

const usage = `usage: rex-voiced <command> [flags]
  setup      写称呼、存供应商兜底 key 到钥匙串（-provider，可不填、在手机上填；第一次用先跑它）
  pair       生成配对二维码，用 RexVoice app 扫
  install    装成登录后自动运行的后台服务（launchd）
  uninstall  停掉并删除后台服务
  serve      前台运行服务（install 装的就是它）
  chat       文字模式，不用手机试工具
  version    打印版本
每个命令都接受 -config-dir（默认 ~/Library/Application Support/rex-voice，或 env REX_VOICE_HOME）。`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "-version", "--version":
		fmt.Println("rex-voiced", version)
	case "-h", "-help", "--help", "help":
		fmt.Println(usage)
	case "setup":
		os.Exit(setup(os.Args[2:]))
	case "install":
		os.Exit(install(os.Args[2:]))
	case "uninstall":
		os.Exit(uninstall(os.Args[2:]))
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
	dirFlag := configDirFlag(fs)
	provider := fs.String("provider", "", "openai | qwen | gemini | grok (default: config.json provider)")
	model := fs.String("model", "", "provider's model (default: config.json model when the provider matches, else the provider's default)")
	keep := fs.Bool("keep-tool-outputs", false, "do not delete tool items after each answer (A/B)")
	instrFile := fs.String("instructions-file", "", "use this file verbatim as the instructions instead of the built-in prompt")
	debug := fs.Bool("debug", false, "log every provider event type to stderr")
	fs.Parse(args)

	_, c, err := loadConfig(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *provider == "" {
		*provider = c.Provider
	}
	if *model == "" && *provider == c.Provider {
		*model = c.Model
	}
	key, _, err := config.ProviderKey(os.Getenv, config.ProviderKeychain(*provider), *provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	hung := make(chan string, 1)
	reg := &tools.Registry{}
	for _, t := range withForbidExtra(tools.NewRexTools(tools.Options{Audit: os.Stderr}), c.ForbidExtra, os.Stderr) {
		reg.Register(t)
	}
	instr := voice.Build(c.Name, c.ProjectAliases, projectLabels())
	if *instrFile != "" {
		b, err := os.ReadFile(*instrFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		instr = string(b)
	}
	reg.Register(tools.HangUpDef(func(reason string) {
		select {
		case hung <- reason:
		default:
		}
	}))

	turnDone := make(chan struct{}, 1)
	cfg := voice.Config{
		Provider:     voice.Provider(*provider),
		Model:        *model,
		Key:          key,
		Instructions: instr,
		Tools:        reg.Defs(),
		TextOnly:     true,
		Extra:        keepExtra(*keep),
		UsageLog:     usageLog(),
		Version:      version,
		Logger:       logger(*debug),
		Call: func(ctx context.Context, name, args string) string {
			t0 := time.Now()
			out := reg.Call(ctx, name, args)
			fmt.Fprintf(os.Stderr, "[tool] %s %s → %d bytes %d ms\n", name, args, len(out), time.Since(t0).Milliseconds())
			return out
		},
		Handlers: voice.Handlers{
			TextDelta:     func(d string) { fmt.Print(d) },
			AssistantText: func(string) { fmt.Println() },
			ResponseDone: func(u voice.Usage) {
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
	s, err := voice.Dial(dctx, cfg)
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

// configDirFlag registers -config-dir, shared by every subcommand.
func configDirFlag(fs *flag.FlagSet) *string {
	return fs.String("config-dir", "", "config directory (default ~/Library/Application Support/rex-voice; env REX_VOICE_HOME)")
}

func loadConfig(flagValue string) (string, config.Config, error) {
	dir, err := config.Dir(flagValue)
	if err != nil {
		return "", config.Config{}, err
	}
	c, err := config.Load(dir)
	return dir, c, err
}

// projectLabels is the prompt's table of what runs now: Claude process directory names, then Rex session labels.
func projectLabels() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := &tools.Registry{}
	for _, t := range tools.NewRexTools(tools.Options{}) {
		if t.Name == "list_claude" {
			reg.Register(t)
		}
	}
	var procs []struct{ Project string }
	if out := reg.Call(ctx, "list_claude", "{}"); json.Unmarshal([]byte(out), &procs) != nil {
		fmt.Fprintln(os.Stderr, "warning: list_claude failed, prompt has no Claude projects:", out)
	}
	var names []string
	for _, p := range procs {
		names = append(names, p.Project)
	}
	seen := map[string]bool{}
	var labels []string
	for _, l := range append(names, rexLabels()...) {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			labels = append(labels, l)
		}
	}
	return labels
}

// withForbidExtra makes send_to_pane also refuse the config.json forbid_extra words (case-insensitive),
// writing the same audit line as the built-in refusal; the built-in list stays in internal/tools.
func withForbidExtra(ts []tools.Tool, words []string, audit io.Writer) []tools.Tool {
	if len(words) == 0 {
		return ts
	}
	if audit == nil {
		audit = io.Discard
	}
	for i, t := range ts {
		if t.Name != "send_to_pane" {
			continue
		}
		run := t.Run
		ts[i].Run = func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct{ Block, Text string }
			if json.Unmarshal(args, &a) == nil {
				low := strings.ToLower(a.Text)
				for _, w := range words {
					if w != "" && strings.Contains(low, strings.ToLower(w)) {
						line, _ := json.Marshal(map[string]string{"time": time.Now().Format(time.RFC3339), "tool": "send_to_pane",
							"block": a.Block, "text": a.Text, "result": "refused"})
						res := map[string]string{"refused": "这类指令不能用语音发，请用户回 Mac 手打"}
						if _, err := audit.Write(append(line, '\n')); err != nil {
							res["audit_error"] = err.Error()
						}
						return res, nil
					}
				}
			}
			return run(ctx, args) // malformed args: the original reports the error
		}
	}
	return ts
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
