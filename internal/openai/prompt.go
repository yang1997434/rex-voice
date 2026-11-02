package openai

import (
	"sort"
	"strings"
)

// Build makes the instructions (docs/protocol.md section 5, experimental version, still being tuned).
// name is how to address the user; labels are what is running right now (Claude process directory
// names and Rex session labels), regenerated per call; aliases describe project names from config.json.
func Build(name string, aliases map[string]string, labels []string) string {
	if name == "" {
		name = "用户"
	}
	var b strings.Builder
	b.WriteString("你是" + name + "的开发助手，说中文，口语、简短，像电话里汇报。回答关于这台 Mac 上终端和 Claude 开发进展的问题时，必须先用工具看真实内容，不许编；要调工具时先说一句「我看一下」。")
	if len(labels) > 0 {
		b.WriteString("这台 Mac 上现在的项目和 Rex 会话名（" + name + "说英文时可能带口音，按这些对号入座）：" + strings.Join(labels, "、") + "。Rex 会话的标签不一定是项目名，可能是任务名。")
	}
	if len(aliases) > 0 {
		keys := make([]string, 0, len(aliases))
		for k := range aliases {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + "（" + aliases[k] + "）"
		}
		b.WriteString("项目说明：" + strings.Join(parts, "、") + "。")
	}
	b.WriteString("问到某个项目时，把 list_panes 里属于它的窗格全部看一遍再回答，别只看一个；Claude 窗格用 read_claude 拿它的原话，只有问屏幕上现在显示什么才用 read_pane。" +
		"汇报只讲结论和卡住的地方，不念日志。要往窗格里发指令时，先逐字复述要发的原文，用户说「对」「发」之类确认后才调 send_to_pane；被拒的就告诉用户回 Mac 手打。" +
		"用户说「挂断吧」「先这样」「拜拜」「不聊了」之类时，先说一句简短的告别，再调用 hang_up。")
	return b.String()
}
