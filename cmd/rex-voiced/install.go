package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yang1997434/rex-voice/internal/config"
)

const launchLabel = "com.maruiao.rex-voiced"

const basePath = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"

func plistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
}

func launchDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// rexDir finds the directory holding the rex CLI: PATH first, then where Rex Server installs it.
func rexDir(home string, lookPath func(string) (string, error)) (string, error) {
	if p, err := lookPath("rex"); err == nil {
		return filepath.Dir(p), nil
	}
	d := filepath.Join(home, "Library", "Application Support", "com.superlogical.rex", "RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS")
	if fi, err := os.Stat(filepath.Join(d, "rex")); err == nil && !fi.IsDir() {
		return d, nil
	}
	return "", errors.New("找不到 rex 命令：先装 Rex 并打开一次，让它装好命令行工具")
}

// plist renders the LaunchAgent; configDir is passed as REX_VOICE_HOME only when it is not the default.
func plist(exe, home, rexDir, configDir string) []byte {
	path := basePath
	if !strings.Contains(":"+basePath+":", ":"+rexDir+":") {
		path += ":" + rexDir
	}
	logPath := filepath.Join(home, "Library", "Logs", "rex-voice", "rex-voiced.log")
	env := [][2]string{{"HOME", home}, {"PATH", path}}
	if configDir != "" {
		env = append(env, [2]string{"REX_VOICE_HOME", configDir})
	}
	esc := func(s string) string {
		var b bytes.Buffer
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- Written by rex-voiced install. Provider keys come from the phone or the Keychain, never stored here. -->
<plist version="1.0"><dict>
`)
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", launchLabel)
	fmt.Fprintf(&b, "  <key>ProgramArguments</key><array><string>%s</string><string>serve</string></array>\n", esc(exe))
	b.WriteString("  <key>EnvironmentVariables</key><dict>\n")
	for _, kv := range env {
		fmt.Fprintf(&b, "    <key>%s</key><string>%s</string>\n", kv[0], esc(kv[1]))
	}
	b.WriteString("  </dict>\n")
	b.WriteString("  <key>KeepAlive</key><true/>\n  <key>ThrottleInterval</key><integer>10</integer>\n  <key>ProcessType</key><string>Interactive</string>\n")
	fmt.Fprintf(&b, "  <key>StandardOutPath</key><string>%s</string>\n  <key>StandardErrorPath</key><string>%s</string>\n", esc(logPath), esc(logPath))
	b.WriteString("</dict></plist>\n")
	return []byte(b.String())
}

func install(args []string) int {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dirFlag := configDirFlag(fs)
	relay := fs.String("relay", "", "save this relay WebSocket URL to config.json before installing")
	fs.Parse(args)

	dir, err := config.Dir(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rd, err := rexDir(home, exec.LookPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if _, err := os.Stat(pairPath(dir)); err != nil {
		fmt.Fprintln(os.Stderr, "还没配对：先运行 rex-voiced pair")
		return 2
	}
	c, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// launchd gets no env keys, so the service's only fallback is the Keychain; the phone can bring its own.
	if _, err := config.ProviderKeychain(c.Provider).Get(); err != nil {
		fmt.Fprintf(os.Stderr, "提醒：钥匙串里没有 %s key（%v），通话要靠手机设置里填的 key；要 Mac 兜底就运行 rex-voiced setup -provider %s\n", c.Provider, err, c.Provider)
	}
	if *relay != "" {
		if err := saveRelay(dir, *relay); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "找不到自己的路径：", err)
		return 1
	}
	configEnv := ""
	if def, _ := config.DefaultDir(); dir != def {
		configEnv = dir
	}
	dst := plistPath(home)
	if err := os.MkdirAll(filepath.Join(home, "Library", "Logs", "rex-voice"), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(dst, plist(exe, home, rd, configEnv), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写 plist 失败，没有装：", err)
		return 1
	}
	bootout()
	if out, err := exec.Command("launchctl", "bootstrap", launchDomain(), dst).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "launchctl bootstrap 失败：%v %s\n", err, out)
		return 1
	}
	fmt.Println("已装好并启动", launchLabel, "（", exe, "）")
	fmt.Println("日志：~/Library/Logs/rex-voice/rex-voiced.log；挪动或升级 rex-voiced 后重新运行 rex-voiced install。")
	return 0
}

func uninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	dirFlag := configDirFlag(fs) // only for the message; the LaunchAgent is per user, not per config dir
	fs.Parse(args)
	dir, err := config.Dir(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	bootout()
	if err := os.Remove(plistPath(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("已停掉并删除 %s。配置和配对还在 %s；钥匙串里的 key 要删就运行 security delete-generic-password -s rex-voice -a <qwen|grok>\n", launchLabel, dir)
	return 0
}

// bootout unloads the agent and waits until launchd has let go of it: bootstrap right after a
// bootout that is still in progress fails with "Input/output error" (seen 2026-11-01). Not loaded is fine.
func bootout() {
	target := launchDomain() + "/" + launchLabel
	exec.Command("launchctl", "bootout", target).Run()
	for range 50 {
		if exec.Command("launchctl", "print", target).Run() != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
