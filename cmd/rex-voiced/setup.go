package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"golang.org/x/term"

	"github.com/yang1997434/rex-voice/internal/config"
)

// setup stores one provider's fallback key in the Keychain and the user's name in config.json.
// Interactive on a terminal; otherwise it reads the provider's env key (config.KeyEnv) and REX_VOICE_NAME.
// The key may be left out: the phone can send its own with call.start.
func setup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	dirFlag := configDirFlag(fs)
	provider := fs.String("provider", config.DefaultProvider, "whose API key to store: openai | qwen | doubao")
	fs.Parse(args)
	keyEnv := config.KeyEnv(*provider)
	if keyEnv == "" {
		fmt.Fprintln(os.Stderr, "不支持的供应商：", *provider)
		return 2
	}

	dir, err := config.Dir(*dirFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c, err := config.Read(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	kc := config.ProviderKeychain(*provider)
	_, kerr := kc.Get()
	haveKey := kerr == nil
	if kerr != nil && !errors.Is(kerr, config.ErrNotFound) {
		fmt.Fprintln(os.Stderr, "读钥匙串失败：", kerr)
		return 1
	}
	tty := term.IsTerminal(int(os.Stdin.Fd()))
	in := bufio.NewReader(os.Stdin)

	var key string
	if tty {
		hint := ""
		if haveKey {
			hint = "（钥匙串里已有，直接回车保留）"
		}
		fmt.Print(*provider, " API key（可留空，在手机设置里填）", hint, "：")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		key = strings.TrimSpace(string(b))
	} else {
		key = strings.TrimSpace(os.Getenv(keyEnv))
	}
	switch {
	case key != "":
		if err := kc.Set(key); err != nil {
			fmt.Fprintln(os.Stderr, "存钥匙串失败：", err)
			return 1
		}
		fmt.Printf("%s key 已存进钥匙串（服务 rex-voice，账户 %s）。\n", *provider, *provider)
	case !haveKey:
		fmt.Printf("没存 %s key：通话要靠手机设置里填的 key；要 Mac 兜底就在终端里重跑 rex-voiced setup -provider %s，或设 env %s。\n", *provider, *provider, keyEnv)
	}

	name := c.Name
	if name == "" {
		name = defaultName()
	}
	if tty {
		fmt.Printf("语音助手怎么称呼你 [%s]：", name)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if line = strings.TrimSpace(line); line != "" {
			name = line
		}
	} else if v := strings.TrimSpace(os.Getenv("REX_VOICE_NAME")); v != "" {
		name = v
	}
	c.Name = name
	if err := c.Save(dir); err != nil {
		fmt.Fprintln(os.Stderr, "写配置失败：", err)
		return 1
	}
	fmt.Println("配置已写到", dir+"/config.json")
	fmt.Println("下一步：rex-voiced pair（用手机扫码）→ rex-voiced install（装成后台服务）")
	return 0
}

// defaultName is the account's full name (dscl RealName), else the login name.
func defaultName() string {
	u, err := user.Current()
	if err != nil {
		return os.Getenv("USER")
	}
	out, err := exec.Command("dscl", ".", "-read", "/Users/"+u.Username, "RealName").Output()
	if err == nil {
		if v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "RealName:")); v != "" {
			return v
		}
	}
	return u.Username
}
