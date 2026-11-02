# REVIEW.md — 二审结论台账

reviewer / advisor 的发现里判为「误报」或「不重要」的记在这里；同类攒到三条就把「别报」写进对应 agent 的提示。

## 2026-11-02 第三批（工具组 / 通话层 / 中继 APNs / iOS 0.3.0）

- 不重要：`internal/call/push.go` 分片循环里写失败只 `ws.Close()`，剩余分片各记一条 warn（最多约 35 条）。无害，日志多几行。
- 不重要：`internal/relay/relay.go` notify 不限速、每帧一个 goroutine（10 s 上限）。Mac 侧已按窗格 10 分钟限一条；拿到 64 位 id 才发得了，沿用「id 即凭证」模型。
- 故意：`internal/tools/show.go` 不限制图片目录（任何可读图片都能发到手机）。用户要看的截图散在各仓，限目录会挡住正常用法；路径只收绝对路径或 ~。
- 记下：`close_pane` 只护 `working`，`blocked`（等用户回答）的 Claude 不带 force 也会被关。按文档设计；要护再加。
