package voice

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildWithAliasesAndLabels(t *testing.T) {
	p := Build("小王", map[string]string{"metis-va": "MathisV", "geo": "国内 SoaRank"}, []string{"geo", "jobs"})
	for _, want := range []string{"你是小王的开发助手", "Rex 会话名", "：geo、jobs。", "项目说明：geo（国内 SoaRank）、metis-va（MathisV）。", "hang_up",
		"只说结论，每次回答不超过两句，不复述工具返回的字段和状态，用户追问再展开", "命令、路径、代码、链接、日志一律不念", "read_claude 默认不传 verbose，用户要原话才传",
		"block 一律用 list_panes 返回的完整 id，不缩写", "先 read_pane 看清编号念给用户，选了再 send_keys 发数字键；escape 取消",
		"换模型发 `/model opus` 这类带别名的", "不发裸 `/model`", "档位 `/effort high`", "「/rename 新名」", "send_keys 按 escape",
		"Claude 的结论用 read_claude，屏幕现在显示什么用 read_pane"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q in %s", want, p)
		}
	}
	if strings.Contains(p, "down 移动再 enter") {
		t.Error("prompt still suggests blind down+enter")
	}
	if strings.Contains(p, "Andy") {
		t.Error("prompt still names Andy")
	}
}

func TestBuildEmptyOmitsSentences(t *testing.T) {
	p := Build("", nil, nil)
	if !strings.HasPrefix(p, "你是用户的开发助手") {
		t.Errorf("default name: %s", p)
	}
	for _, no := range []string{"Rex 会话名", "项目说明"} {
		if strings.Contains(p, no) {
			t.Errorf("empty input still writes %q", no)
		}
	}
	// Every answer re-sends the prompt as input tokens (11-04: 95% of Qwen input is text).
	if n := utf8.RuneCountInString(p); n > 700 {
		t.Errorf("Build(\"\", nil, nil) is %d runes, want <= 700", n)
	}
}
