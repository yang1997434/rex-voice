package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yang1997434/rex-voice/internal/tools"
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

func TestForbidExtra(t *testing.T) {
	ran := 0
	ts := []tools.Tool{{Name: "send_to_pane", Run: func(context.Context, json.RawMessage) (any, error) { ran++; return "sent", nil }}}
	var audit bytes.Buffer
	run := withForbidExtra(ts, []string{"DROP TABLE"}, &audit)[0].Run
	res, _ := run(context.Background(), json.RawMessage(`{"block":"b1","text":"please drop table users"}`))
	if m, ok := res.(map[string]string); !ok || m["refused"] == "" || ran != 0 || !strings.Contains(audit.String(), `"result":"refused"`) {
		t.Fatalf("res=%v ran=%d audit=%s", res, ran, audit.String())
	}
	if res, _ := run(context.Background(), json.RawMessage(`{"block":"b1","text":"ls"}`)); res != "sent" || ran != 1 {
		t.Fatalf("allowed text: res=%v ran=%d", res, ran)
	}
	if got := withForbidExtra(ts, nil, io.Discard); &got[0] != &ts[0] {
		t.Fatal("no words should leave tools untouched")
	}
}
