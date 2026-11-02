2026-11-01

- [ ] 第一批：中继 → Mac 服务（含文字模式 CLI 自测）→ iOS app → 联调，顺序和验收见 `docs/plan-2026-11-01.md`。
- [ ] 模型 A/B：同一组 20 句真实指令比 `gpt-realtime-2.1` / `gpt-realtime-2.1-mini` / `gpt-live-1`（准确率、首音延迟、每小时花费），定默认值，报表进 `docs/releases.md`。
- [ ] 改全局 `~/.claude/hooks/prod-guard.py`：来自 rex-voice 的输入不算授权（改前给用户看原文和 diff）。
- [ ] 正式版上线后归档 `~/data/claude/voice-probe`（服务和实验用的 Tailscale grant 11-01 都已停/删）。
- [ ] 中继独立域名（`voice.maruiao.top`）：用户想要时在阿里云 DNS 加 A 记录到 23.94.9.58，再 certbot 扩证书。
- [ ] 下一批候选：Claude 变 blocked 时主动来电（推送 / CallKit）、整台 Mac 控制工具组。
