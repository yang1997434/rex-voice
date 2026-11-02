// Package config is the per-user settings of rex-voiced: config.json in the config directory
// (shared with pair.json) and the OpenAI key in the macOS Keychain.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	Name           string            `json:"name,omitempty"`  // how the model addresses the user
	Model          string            `json:"model,omitempty"` // OpenAI Realtime model
	Relay          string            `json:"relay,omitempty"` // relay WebSocket URL
	Voice          string            `json:"voice,omitempty"`
	ProjectAliases map[string]string `json:"project_aliases,omitempty"` // project name → one-line description for the prompt
	ForbidExtra    []string          `json:"forbid_extra,omitempty"`    // more words send_to_pane refuses, on top of the built-in list
}

const (
	DefaultModel = "gpt-realtime-2.1"
	DefaultRelay = "wss://r.maruiao.top/_voice/ws"
	DefaultVoice = "marin"
	fileName     = "config.json"
)

// Dir picks the config directory: flag value, then env REX_VOICE_HOME, then ~/Library/Application Support/rex-voice.
func Dir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	if v := os.Getenv("REX_VOICE_HOME"); v != "" {
		return filepath.Abs(v)
	}
	return DefaultDir()
}

func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "rex-voice"), nil
}

// Read returns config.json as written, without defaults; a missing file is an empty Config.
func Read(dir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", filepath.Join(dir, fileName), err)
	}
	return c, nil
}

// Load is Read with defaults filled into empty fields.
func Load(dir string) (Config, error) {
	c, err := Read(dir)
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Relay == "" {
		c.Relay = DefaultRelay
	}
	if c.Voice == "" {
		c.Voice = DefaultVoice
	}
	return c, err
}

// Save writes config.json (0600, directory 0700) via a temp file so a crash never leaves half a file.
// Save what Read returned plus your changes, not what Load returned, so defaults are not frozen into the file.
func (c Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, fileName+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, fileName))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// ---- Keychain ----

var ErrNotFound = errors.New("not in Keychain")

// Keychain stores one generic password through /usr/bin/security. Because the security tool itself
// creates and reads the item, the item's access list trusts it and launchd reads it without a prompt,
// whatever path rex-voiced is installed at.
type Keychain struct {
	Service, Account string
	// Exec runs `security <args>` with stdin and returns stdout; exit status 44 (item not found) must be ErrNotFound.
	Exec func(stdin string, args ...string) ([]byte, error)
}

// OpenAIKeychain is the item setup writes and serve reads.
func OpenAIKeychain() Keychain {
	return Keychain{Service: "rex-voice", Account: "openai", Exec: runSecurity}
}

func runSecurity(stdin string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("/usr/bin/security", args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 44 {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("security %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (k Keychain) Get() (string, error) {
	out, err := k.Exec("", "find-generic-password", "-s", k.Service, "-a", k.Account, "-w")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Set adds or replaces the item. The secret goes in on stdin (`security -i`), never on a command line other processes can see.
func (k Keychain) Set(secret string) error {
	if secret == "" || strings.ContainsAny(secret, "\"\\' \t\r\n") {
		return errors.New("key 为空或含空白、引号、反斜杠")
	}
	_, err := k.Exec(fmt.Sprintf("add-generic-password -U -s %q -a %q -w %q\n", k.Service, k.Account, secret), "-i")
	return err
}

func (k Keychain) Delete() error {
	_, err := k.Exec("", "delete-generic-password", "-s", k.Service, "-a", k.Account)
	return err
}

// OpenAIKey looks in env OPENAI_API_KEY, then the Keychain; source is "env" or "keychain".
func OpenAIKey(getenv func(string) string, kc Keychain) (key, source string, err error) {
	if v := getenv("OPENAI_API_KEY"); v != "" {
		return v, "env", nil
	}
	key, err = kc.Get()
	switch {
	case errors.Is(err, ErrNotFound):
		return "", "", errors.New("没有 OpenAI key：先运行 rex-voiced setup")
	case err != nil:
		return "", "", fmt.Errorf("从钥匙串读 OpenAI key 失败：%w", err)
	case key == "":
		return "", "", errors.New("钥匙串里的 OpenAI key 是空的：重新运行 rex-voiced setup")
	}
	return key, "keychain", nil
}
