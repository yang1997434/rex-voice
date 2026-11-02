2026-11-01

- [ ] 第一批：中继 → Mac 服务（含文字模式 CLI 自测）→ iOS app → 联调，顺序和验收见 `docs/plan-2026-11-01.md`。
- [ ] 模型 A/B：同一组 20 句真实指令比 `gpt-realtime-2.1` / `gpt-realtime-2.1-mini` / `gpt-live-1`（准确率、首音延迟、每小时花费），定默认值，报表进 `docs/releases.md`。
- [ ] 改全局 `~/.claude/hooks/prod-guard.py`：来自 rex-voice 的输入不算授权（改前给用户看原文和 diff）。
- [ ] 正式版上线后归档 `~/data/claude/voice-probe`（服务和实验用的 Tailscale grant 11-01 都已停/删）。
- [ ] 中继独立域名（`voice.maruiao.top`）：用户想要时在阿里云 DNS 加 A 记录到 23.94.9.58，再 certbot 扩证书。
- [ ] 下一批候选：Claude 变 blocked 时主动来电（推送 / CallKit）、整台 Mac 控制工具组。

## 2026-11-01 第一批（1、2a–2d、3a、3b）收口追加
- [ ] 文字模式 A/B 显示删工具项省 36% 输入 token 但只省 13% 费用；用语音真通话再比一次（`-keep-tool-outputs`），再定默认。
- [ ] 手机能量门阈值（默认 0.01）要在真机上按环境校准，设置页有滑杆；校准后把合理默认写进代码。
- [ ] `ITSAppUsesNonExemptEncryption=NO` 是合规判断（只用系统 CryptoKit 做端到端加密），要用户确认；若要改成 YES 得走出口合规问卷。
- [ ] 中继每小时 nginx `proxy_read_timeout 3600s` 到期会断一次、两端自动重连，已接受；真机验收看重连时通话是否续上。
- [ ] 死连接只靠 30 秒保活发送失败发现，TCP 半开可能要几分钟；真机验「网络切换」那条不过再加 pong 超时。
- [ ] `Handshake` 已挪进 RexVoiceCore（0617e59）；RelayClient / AudioEngine / CallController 仍没有自动化测试（app target 只能编 iOS、本机无模拟器），要测就得装模拟器 runtime。
- [ ] Mac 端第一个 GitHub Release（`scripts/release-mac.sh 0.1.0` + `gh release create`）等联调通了再出；出了以后 README 加一行 `curl` 装法。
- [ ] App 图标是代码生成的简版（8e84bda），以后换正式图只需替换 `ios/RexVoice/Assets.xcassets/AppIcon.appiconset/icon-1024.png`（1024×1024、无 alpha）。
- [已做] 中继 2026-11-01 部署到 ccs-maruiao：`/opt/rex-voice/rex-voice-relay`、systemd `rex-voice-relay`（enabled）、nginx 片段 `/etc/nginx/snippets/nginx-voice.conf` include 进 `sites-available/reports` 的 r.maruiao.top 块，改前备份 `/etc/nginx/backups/reports.bak-20261101-voice`。prod-guard 不认「部署 rex-voice 中继」，这次用户用「部署 汇报站 生产」授权；改 prod-guard 时顺带加这个词。

## 2026-11-01 第二批（提供方接口、Qwen、豆包、设计稿、市场调研）收口追加
- [x] 已做（豆包 2026-11-01 用户定移除，代码已删，调研留档）豆包 3.0 真跑：等用户 `secret-add DOUBAO_API_KEY` 后跑 `DOUBAO_API_KEY=$(flow-cred DOUBAO_API_KEY) go test -tags manual -run Manual -v ./internal/voice/doubao`，核 4 处猜的字段（采样率键名 rate/sample_rate、usage 拆分、音色后缀 uranus/jupiter、是否要 response.create）。
- [ ] Qwen：新端点（带 WorkspaceId）没真跑（国际站端点 11-01 已真跑，见 protocol §4.2）；audio-3.1 的 `smart_turn` 与 `smart_turn_v2` 差别没查、flash 档价格没录（plus 已录 prices.go）；被打断的回复全文仍进 RecentTurns（和 OpenAI 一样）；语音模式回复进行中 SendText 的报错行为未核实。
- [ ] 豆包不支持文字输入：`rex-voiced chat -provider doubao` 不可用；空闲提示改为手机显示文字（34c6f02）。
- [ ] 四家语音 A/B（OpenAI / Qwen 3.8 与 audio-3.1 / Gemini / Grok）等 iOS 定稿实现上 TestFlight 后跑，20 句中文指令，记 docs/releases.md。
- [ ] 市场盘点页 /_r/7c04be05a07119c87544ad435850fff5/ 列的下一批候选：Gemini 3.8 Live、Grok Voice 3.0、StepAudio 3 Realtime（限免）；等用户定。
- [ ] `report-publish` 在本机会先撞系统 openrsync（不认 --chmod）：脚本在 claude-stack 仓，建议改成 /opt/homebrew/bin/rsync，等用户点头。
- [ ] iOS 设计稿 B、C 两版代码在定稿实现时删除，截图保留在 docs/design-specs/ios-liquid-glass/。
- [ ] 局域网直连 + 中继兜底（用户 2026-11-01 问「别人用是不是都走我的中继」）：Mac 在 LAN 监听、二维码同时带 LAN 地址，手机先直连再回退中继；在家不经中继。要用户点头再做。
- [x] 已做（用户 2026-11-02 定 app 侧羽化不重出图：PersonaView 左右和顶部 16% 渐隐，checker 核过 kimi 右侧仅剩极淡残留）新形象 5 组原图裁平露硬切线。
- [x] 已做（用户 2026-11-02 定：接受 40 MB、640px）形象资源进 app 包；上架前再看要不要按需下载。
- [ ] 第三方参考图（aigengtu 三视图、ZipZipPipe 插画、moegirl 快照）只在本机 `docs/design-specs/personas/_refs/`，已 gitignore；换机器要重新按各 `source.md` 下载。
- [ ] Qwen 国际站默认：国内网络下 audio-3.1-plus 首包延迟没量；A/B 时和 3.8（北京）对比记 docs/releases.md。
- [ ] 音色试听（`voice.preview`，Mac 端 2026-11-02 做完）：OpenAI、Qwen 国际站真跑过（`go test -tags manual -run TestPreviewReal ./internal/call/`）；Gemini、Grok 没真跑。Grok 语音模式下 `SendText` 是否直接出音频没核（每条 `conversation.item.create` 另收 $0.004，外加连接分钟），上 Grok 试听前先真跑一次。
