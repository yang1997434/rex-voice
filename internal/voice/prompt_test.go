package voice

import (
	"strings"
	"testing"
)

func TestBuildWithAliasesAndLabels(t *testing.T) {
	p := Build("小王", map[string]string{"metis-va": "MathisV", "geo": "国内 SoaRank"}, []string{"geo", "jobs"})
	for _, want := range []string{"你是小王的开发助手", "Rex 会话名", "：geo、jobs。", "项目说明：geo（国内 SoaRank）、metis-va（MathisV）。", "hang_up",
		"汇报一句话说结论", "命令、路径、代码、链接、日志一律不念", "read_claude 带 verbose",
		"block 一律用 list_panes 返回的完整 id，不自己缩写", "选择框（/model、/effort、AskUserQuestion、plan 审批）用 send_keys 发数字键选编号，先 read_pane 看清编号再按，不用 down；escape 取消",
		"换模型直接发 `/model opus` 这类带别名的", "不发裸 `/model`", "调档位 `/effort high`", "快速模式 `/fast on`"} {
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
}
