package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowFileValidation(t *testing.T) {
	home := t.TempDir()
	for name, size := range map[string]int64{"a.png": 3, "big.jpg": 20<<20 + 1, "notes.txt": 3, "huge.png": 3} {
		f, _ := os.Create(filepath.Join(home, name))
		f.Truncate(size) // sparse, so the 20 MB file is cheap
		f.Close()
	}
	var sent []string
	exec := func(_ context.Context, name string, args ...string) ([]byte, error) {
		n := 100
		if strings.Contains(strings.Join(args, " "), "huge.png") {
			n = 2<<20 + 1
		}
		return nil, os.WriteFile(args[len(args)-1], make([]byte, n), 0o600)
	}
	r := &Registry{}
	r.Register(ShowFileDef(Options{Exec: exec, Home: home}, func(name string, jpeg []byte) (int, error) {
		sent = append(sent, name)
		return 1, nil
	}))
	for path, want := range map[string]string{
		"~/a.png":                `"sent":"a.png"`, // ~ expands to Home
		"~/notes.txt":            "只能发",
		"~/big.jpg":              "上限 20 MB",
		"~/huge.png":             "超过 2 MB",
		"~/missing.png":          "找不到文件",
		filepath.Join(home, "x"): "只能发",
	} {
		if got := r.Call(context.Background(), "show_file", `{"path":"`+path+`"}`); !strings.Contains(got, want) {
			t.Errorf("show_file %s = %s, want %s", path, got, want)
		}
	}
	if len(sent) != 1 || sent[0] != "a.png" {
		t.Fatalf("sent = %v, want only a.png", sent)
	}
}

func TestShowLinkValidation(t *testing.T) {
	var sent []string
	r := &Registry{}
	r.Register(ShowLinkDef(func(url, title string) error { sent = append(sent, url+"|"+title); return nil }))
	for args, want := range map[string]string{
		`{"url":"https://example.com/p?q=1","title":"PR"}`: `"sent":"https://example.com/p?q=1"`,
		`{"url":"file:///etc/passwd"}`:                     "只能发 http / https",
		`{"url":"ftp://example.com/x"}`:                    "只能发 http / https",
		`{"url":"javascript:alert(1)"}`:                    "只能发 http / https",
		`{"url":"https://"}`:                               "只能发 http / https",
	} {
		if got := r.Call(context.Background(), "show_link", args); !strings.Contains(got, want) {
			t.Errorf("show_link %s = %s, want %s", args, got, want)
		}
	}
	if len(sent) != 1 || sent[0] != "https://example.com/p?q=1|PR" {
		t.Fatalf("sent = %v", sent)
	}
}
