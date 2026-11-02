package voice

import (
	"strings"
	"testing"
)

func TestBuildWithAliasesAndLabels(t *testing.T) {
	p := Build("小王", map[string]string{"metis-va": "MathisV", "geo": "国内 SoaRank"}, []string{"geo", "jobs"})
	for _, want := range []string{"你是小王的开发助手", "Rex 会话名", "：geo、jobs。", "项目说明：geo（国内 SoaRank）、metis-va（MathisV）。", "hang_up",
		"汇报一句话说结论", "命令、路径、代码、链接、日志一律不念", "read_claude 带 verbose"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q in %s", want, p)
		}
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
