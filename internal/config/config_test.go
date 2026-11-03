package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Provider: DefaultProvider, Relay: DefaultRelay}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v want %+v", c, want)
	}
}

func TestLoadMergesPartialFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"name":"小王","voice":"cedar","project_aliases":{"geo":"国内 SoaRank"}}`), 0o600)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Name: "小王", Provider: DefaultProvider, Relay: DefaultRelay, Voice: "cedar", ProjectAliases: map[string]string{"geo": "国内 SoaRank"}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("got %+v want %+v", c, want)
	}
}

func TestLoadBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"name":`), 0o600)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "config.json") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveKeepsOtherFieldsAndNoDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	if err := (Config{Name: "a", ForbidExtra: []string{"drop"}}).Save(dir); err != nil {
		t.Fatal(err)
	}
	c, _ := Read(dir)
	c.Name = "b"
	if err := c.Save(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if s := string(b); !strings.Contains(s, `"b"`) || !strings.Contains(s, "drop") || strings.Contains(s, "model") {
		t.Fatalf("file = %s", s)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "config.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode())
	}
}

type fakeSecurity struct {
	out   string
	err   error
	calls []string
}

func (f *fakeSecurity) kc() Keychain {
	return Keychain{Service: "rex-voice", Account: "qwen", Exec: func(stdin string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(args, " ")+"|"+stdin)
		return []byte(f.out), f.err
	}}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestProviderKeyEnvFirst(t *testing.T) {
	f := &fakeSecurity{out: "key-from-keychain\n"}
	key, src, err := ProviderKey(env(map[string]string{"DASHSCOPE_API_KEY": "key-from-env"}), f.kc(), "qwen")
	if err != nil || key != "key-from-env" || src != "env" || len(f.calls) != 0 {
		t.Fatalf("key=%q src=%q err=%v calls=%v", key, src, err, f.calls)
	}
	// another provider's env name; DASHSCOPE_API_KEY must not leak into it
	key, src, _ = ProviderKey(env(map[string]string{"DASHSCOPE_API_KEY": "q", "XAI_API_KEY": "x"}), f.kc(), "grok")
	if key != "x" || src != "env" {
		t.Fatalf("grok key=%q src=%q", key, src)
	}
}

func TestProviderKeyFromKeychain(t *testing.T) {
	f := &fakeSecurity{out: "key-from-keychain\n"}
	key, src, err := ProviderKey(env(nil), f.kc(), "qwen")
	if err != nil || key != "key-from-keychain" || src != "keychain" {
		t.Fatalf("key=%q src=%q err=%v", key, src, err)
	}
	if len(f.calls) != 1 || f.calls[0] != "find-generic-password -s rex-voice -a qwen -w|" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestProviderKeyMissingPointsToSetup(t *testing.T) {
	f := &fakeSecurity{err: ErrNotFound}
	if _, _, err := ProviderKey(env(nil), f.kc(), "qwen"); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderKeyKeychainErrorNotHidden(t *testing.T) {
	f := &fakeSecurity{err: errors.New("security find-generic-password: User interaction is not allowed")}
	if _, _, err := ProviderKey(env(nil), f.kc(), "qwen"); err == nil || !strings.Contains(err.Error(), "User interaction") {
		t.Fatalf("err = %v", err)
	}
}

func TestKeychainSetUsesStdin(t *testing.T) {
	f := &fakeSecurity{}
	if err := f.kc().Set("abc_123-x"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "-i|add-generic-password -U -s \"rex-voice\" -a \"qwen\" -w \"abc_123-x\"\n" {
		t.Fatalf("calls = %q", f.calls)
	}
	for _, bad := range []string{"", "k x", `k"x`, "k\nx"} {
		if err := f.kc().Set(bad); err == nil {
			t.Errorf("Set(%q) accepted", bad)
		}
	}
}

// TestKeychainReal round-trips a throwaway item in the login keychain; REX_VOICE_KEYCHAIN_TEST=1 to run.
func TestKeychainReal(t *testing.T) {
	if os.Getenv("REX_VOICE_KEYCHAIN_TEST") == "" {
		t.Skip("set REX_VOICE_KEYCHAIN_TEST=1")
	}
	k := ProviderKeychain("qwen")
	k.Service = "rex-voice-unittest"
	if k.Account != "qwen" {
		t.Fatalf("account = %q", k.Account)
	}
	t.Cleanup(func() { k.Delete() })
	if _, err := k.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before Set: %v", err)
	}
	if err := k.Set("test-value"); err != nil {
		t.Fatal(err)
	}
	if v, err := k.Get(); err != nil || v != "test-value" {
		t.Fatalf("Get = %q %v", v, err)
	}
}
