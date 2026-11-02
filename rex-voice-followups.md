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
