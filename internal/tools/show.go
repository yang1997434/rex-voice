package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxShowSrc  = 20 << 20 // source image cap
	maxShowJPEG = 2 << 20  // after sips shrinks it to ≤ 1280 px
)

var showExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".heic": true, ".webp": true}

// ShowFileDef builds show_file: it shrinks an image on this Mac to a ≤ 1280 px JPEG with sips
// (through opts.Exec) and hands it to send, which the session layer owns (it pushes it to the
// phone and returns how many chunks it took, or an error such as 「没有通话」).
func ShowFileDef(opts Options, send func(name string, jpeg []byte) (chunks int, err error)) Tool {
	if opts.Exec == nil {
		opts.Exec = runCmd
	}
	return Tool{
		Name:        "show_file",
		Description: "把这台 Mac 上的一张图片（png / jpg / heic / webp，不超过 20 MB）发到用户手机上显示。用户要看截图 / 图片时用，路径从 read_claude 的原话里找；只在通话中可用。",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"图片的绝对路径，可以用 ~ 开头"}},"required":["path"],"additionalProperties":false}`),
		Run: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				Path string `json:"path"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			src, err := expandHome(a.Path, opts.Home)
			if err != nil {
				return nil, err
			}
			if !showExts[strings.ToLower(filepath.Ext(src))] {
				return nil, errors.New("只能发 png / jpg / jpeg / heic / webp 图片: " + src)
			}
			st, err := os.Stat(src)
			switch {
			case err != nil:
				return nil, fmt.Errorf("找不到文件: %s", src)
			case !st.Mode().IsRegular():
				return nil, errors.New("不是普通文件: " + src)
			case st.Size() > maxShowSrc:
				return nil, fmt.Errorf("图片太大（%d MB），上限 20 MB", st.Size()>>20)
			}
			tmp, err := os.CreateTemp("", "rex-voice-show-*.jpg")
			if err != nil {
				return nil, err
			}
			tmp.Close()
			defer os.Remove(tmp.Name())
			if _, err := opts.Exec(ctx, "sips", "-Z", "1280", "-s", "format", "jpeg", src, "--out", tmp.Name()); err != nil {
				return nil, fmt.Errorf("sips 转换失败: %v", err)
			}
			jpeg, err := os.ReadFile(tmp.Name())
			switch {
			case err != nil:
				return nil, err
			case len(jpeg) == 0:
				return nil, errors.New("sips 没有生成图片")
			case len(jpeg) > maxShowJPEG:
				return nil, fmt.Errorf("缩小后仍有 %d KB，超过 2 MB", len(jpeg)>>10)
			}
			name := filepath.Base(src)
			chunks, err := send(name, jpeg)
			if err != nil {
				return nil, err
			}
			return map[string]any{"sent": name, "size": len(jpeg), "chunks": chunks}, nil
		},
	}
}

// ShowLinkDef builds show_link; send (owned by the session layer) puts the link on the phone.
func ShowLinkDef(send func(url, title string) error) Tool {
	return Tool{
		Name:        "show_link",
		Description: "把一个网页链接发到用户手机上，用户点开看。汇报页、PR 链接这类要用户自己看的用这个，不要把网址念出来；只收 http / https，只在通话中可用。",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"title":{"type":"string","description":"链接标题，可省略"}},"required":["url"],"additionalProperties":false}`),
		Run: func(_ context.Context, args json.RawMessage) (any, error) {
			var a struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			u, err := url.Parse(strings.TrimSpace(a.URL))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, errors.New("只能发 http / https 链接: " + a.URL)
			}
			if err := send(u.String(), a.Title); err != nil {
				return nil, err
			}
			return map[string]string{"sent": u.String()}, nil
		},
	}
}

// expandHome turns a leading ~ into home (os.UserHomeDir when empty).
func expandHome(p, home string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("缺少 path")
	}
	if p != "~" && !strings.HasPrefix(p, "~/") {
		if !filepath.IsAbs(p) { // a relative name like -x.png would reach sips as an option
			return "", errors.New("path 要是绝对路径或 ~ 开头: " + p)
		}
		return p, nil
	}
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(home, p[1:]), nil
}
