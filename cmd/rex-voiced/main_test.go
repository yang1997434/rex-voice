package main

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlist(t *testing.T) {
	b := plist("/Apps/A&B/rex-voiced", "/Users/x", "/opt/rex dir", "")
	if err := xml.Unmarshal(b, new(any)); err != nil {
		t.Fatalf("not XML: %v\n%s", err, b)
	}
	s := string(b)
	for _, want := range []string{
		"<array><string>/Apps/A&amp;B/rex-voiced</string><string>serve</string></array>",
		"<string>" + basePath + ":/opt/rex dir</string>",
		"<key>HOME</key><string>/Users/x</string>",
		"<string>/Users/x/Library/Logs/rex-voice/rex-voiced.log</string>",
		"<key>KeepAlive</key><true/>", "<integer>10</integer>", "<string>Interactive</string>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "REX_VOICE_HOME") {
		t.Error("default config dir written into plist")
	}
	if s := string(plist("/x", "/h", "/usr/bin", "/tmp/cfg")); !strings.Contains(s, "<key>REX_VOICE_HOME</key><string>/tmp/cfg</string>") || strings.Contains(s, ":/usr/bin</string>") {
		t.Errorf("custom dir / duplicate PATH entry: %s", s)
	}
	if _, err := exec.LookPath("plutil"); err == nil {
		f := filepath.Join(t.TempDir(), "a.plist")
		os.WriteFile(f, b, 0o644)
		if out, err := exec.Command("plutil", "-lint", f).CombinedOutput(); err != nil {
			t.Fatalf("plutil: %v %s", err, out)
		}
	}
}

func TestRexDir(t *testing.T) {
	home := t.TempDir()
	notFound := func(string) (string, error) { return "", errors.New("no") }
	if _, err := rexDir(home, notFound); err == nil || !strings.Contains(err.Error(), "先装 Rex") {
		t.Fatalf("err = %v", err)
	}
	d := filepath.Join(home, "Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "rex"), nil, 0o755)
	if got, err := rexDir(home, notFound); err != nil || got != d {
		t.Fatalf("fallback = %q %v", got, err)
	}
	if got, _ := rexDir(home, func(string) (string, error) { return "/opt/bin/rex", nil }); got != "/opt/bin" {
		t.Fatalf("PATH = %q", got)
	}
}
