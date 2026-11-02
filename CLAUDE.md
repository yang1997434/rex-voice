# rex-voice — 手机实时语音遥控 Mac

iPhone app 跟实时语音模型（OpenAI Realtime）打电话，它自己读这台 Mac 上 Rex 各窗格和 Claude 的进展、往会话里发指令、听到「挂断吧」就挂。2026-11-01 立项，用户拍板：原生 iOS app + TestFlight、扫码配对、先只管 Rex、以后扩到整台 Mac。方案全文见 `docs/plan-2026-11-01.md`，协议见 `docs/protocol.md`，续做见 `rex-voice-followups.md`。立项前的一次性实验在 `~/data/claude/voice-probe/`（Node，不进仓），`tools.mjs` 是工具行为的移植依据。

## 三个部件

| 部件 | 路径 | 跑在哪 | 常驻方式 |
|---|---|---|---|
| `rex-voiced` Mac 服务 | `cmd/rex-voiced` + `internal/*` | 这台 Mac mini | launchd（`scripts/`） |
| `rex-voice-relay` 中继 | `cmd/rex-voice-relay` | ccs-maruiao `23.94.9.58`，nginx `r.maruiao.top/_voice/` 后面 | systemd |
| iOS app | `ios/RexVoice` | iPhone，TestFlight 分发 | — |

音频走 Mac：手机 ⇄ 中继 ⇄ Mac ⇄ OpenAI。OpenAI key 只在 Mac：服务从钥匙串读（`rex-voiced setup` 写入，服务 `rex-voice` 账户 `openai`），env `OPENAI_API_KEY` 优先，开发时用 `OPENAI_API_KEY=$(flow-cred OPENAI_API_KEY)`。手机和 Mac 之间端到端加密，中继只转发。

## 规矩

- Go 1.27，只用标准库 + `golang.org/x/*`，唯一例外 `rsc.io/qr`（终端二维码，纯 Go 无依赖，2026-11-01 定）（WebSocket 用 `golang.org/x/net/websocket`，2026-11-01 定：只转发二进制帧、不需要 ping 控制，够用且在 x/* 范围内；模块路径 `github.com/yang1997434/rex-voice`）。`gofmt -l . && go vet ./... && go test ./...` 是门禁。
- Swift 6 / SwiftUI，iOS 18 起，只用系统框架（AVAudioEngine、CryptoKit、VisionKit 扫码、Network）。不引第三方包，不用 WebRTC。构建用 `xcodebuild`，没有 Xcode GUI。
- `~/data/archive/claude-terminal-20261029` 里的旧 iOS 项目是无许可的私有 fork：**代码一行不抄**，只参考发版做法（见 `docs/plan-2026-11-01.md`「Context」）。
- 工具层（`internal/tools`）是注册表，加新能力（AppleScript、computer-use）只加一组、不改调用方。
- `send_to_pane` 对含「生产 / 部署 / 上线 / 发布 / 回滚 / 迁移 / rm -rf / git push / sudo」的文字直接拒，这是语音绕过 `~/.claude/hooks/prod-guard.py` 的临时闸；正式闸要改 prod-guard 认来源（续做）。
- 每次 TestFlight 上传写进 `docs/releases.md`：版本、build 号、commit、改了什么。

## 部署

- **中继（ccs-maruiao）**：改生产，先要用户明文说「部署 rex-voice 中继」。步骤：本机 `GOOS=linux GOARCH=amd64 go build` → `scp` 到 `/opt/rex-voice/` → `systemctl restart rex-voice-relay` → `curl -A rex-voice-check https://r.maruiao.top/_voice/healthz`（不带 UA 会被该站的反爬 403）。中继以 systemd `DynamicUser` 跑、只听 `127.0.0.1:8790`。nginx 片段在 `scripts/nginx-voice.conf`，改 nginx 前先备份原文件。按总纲「部署分档」走：只换二进制算热修档。
- **Mac 服务**：`rex-voiced setup` → `pair` → `install`（`uninstall` 卸），本机、可随时回退，不算生产。对外分发包用 `scripts/release-mac.sh <版本>`（签名 + 公证），装法见 `README.md`。
- **iOS**：`scripts/build-ios.sh <版本>`，用 ASC Admin key（`flow-cred ASC_ADMIN_KEY_ID / ASC_ISSUER_ID / ASC_ADMIN_KEY_PATH`），team `KAA6GJ59XS`，bundle id `com.maruiao.rexvoice`。发前先 `security unlock-keychain`，否则 archive 会卡死在签名。

## E2E 入口

还没有。真机验证靠 TestFlight 装到用户手机上，由用户按 `docs/plan-2026-11-01.md`「验收」逐条试；Mac 侧有文字模式 CLI（`rex-voiced chat`）可不用手机验证工具闭环。

## 哪些文档不能信

- `docs/plan-2026-11-01.md` 是立项方案快照，做的过程中以代码和本文件为准；里面标「待 coder 核实」的都是没验证的。
