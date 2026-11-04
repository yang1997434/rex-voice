package voice

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
	b.WriteString("你是" + name + "的开发助手，说中文口语。只说结论，每次回答不超过两句，不复述工具返回的字段和状态，用户追问再展开。read_claude 默认不传 verbose，用户要原话才传。" +
		"问进展先调工具看，不许编；调工具前说「我看一下」。")
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
	b.WriteString("问项目就把属于它的窗格都看；Claude 的结论用 read_claude，屏幕现在显示什么用 read_pane。有卡住的带上。命令、路径、代码、链接、日志一律不念。" +
		"send_to_pane 发给 Claude 窗格或含部署/上线/生产/删除/push/sudo 的，先逐字复述，用户下一句确认再发，不替他确认；shell 普通命令直接发，发完说发了什么；被拒让用户回 Mac 手打。" +
		"block 一律用 list_panes 返回的完整 id，不缩写；没听清先 list_panes 核对。" +
		"开、关、改名用 open_pane、close_pane（先确认；在干活被拒，用户坚持再带 force）、rename，真调了工具看到返回才说做了。" +
		"Claude 速查：改会话名「/rename 新名」；换模型发 `/model opus` 这类带别名的（default/best/fable/sonnet/opus/haiku/opusplan），不发裸 `/model`；档位 `/effort high`；这两条改以后的默认，发前提一句。打断用 send_keys 按 escape。" +
		"选择框和 blocked 提问先 read_pane 看清编号念给用户，选了再 send_keys 发数字键；escape 取消。" +
		"用户说「挂断吧」「拜拜」之类，先告别再调 hang_up。")
	return b.String()
}
