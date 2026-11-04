# rex-voice — 手机实时语音遥控 Mac

iPhone app 跟实时语音模型（千问 / Grok；OpenAI、Gemini、MOSS 2026-11-03 用户撤下，包留在 `internal/voice/` 没注册）打电话，它自己读这台 Mac 上 Rex 各窗格和 Claude 的进展、往会话里发指令、听到「挂断吧」就挂。2026-11-01 立项，用户拍板：原生 iOS app + TestFlight、扫码配对、先只管 Rex、以后扩到整台 Mac。方案全文见 `docs/plan-2026-11-01.md`，协议见 `docs/protocol.md`，续做见 `rex-voice-followups.md`。立项前的一次性实验在 `~/data/claude/voice-probe/`（Node，不进仓），`tools.mjs` 是工具行为的移植依据。

## 三个部件

| 部件 | 路径 | 跑在哪 | 常驻方式 |
|---|---|---|---|
| `rex-voiced` Mac 服务 | `cmd/rex-voiced` + `internal/*` | 这台 Mac mini | launchd（`scripts/`） |
| `rex-voice-relay` 中继 | `cmd/rex-voice-relay` | ccs-maruiao（`r.maruiao.top` 那台），nginx `r.maruiao.top/_voice/` 后面 | systemd |
| iOS app | `ios/RexVoice` | iPhone，TestFlight 分发 | — |

音频走 Mac：手机 ⇄ 中继 ⇄ Mac ⇄ 语音供应商。key 平时由手机随 `call.start` 发来；Mac 兜底 key 在钥匙串（`rex-voiced setup -provider qwen|grok` 写入，服务 `rex-voice` 账户是供应商名），env `DASHSCOPE_API_KEY` / `XAI_API_KEY` 优先，开发时用 `DASHSCOPE_API_KEY=$(flow-cred QWEN_API_KEY_2)`（国际站的 key；`QWEN_API_KEY` 是北京的）。手机和 Mac 之间端到端加密，中继只转发。

## 规矩

- Go 1.27，只用标准库 + `golang.org/x/*`，唯一例外 `rsc.io/qr`（终端二维码，纯 Go 无依赖，2026-11-01 定）（WebSocket 用 `golang.org/x/net/websocket`，2026-11-01 定：只转发二进制帧、不需要 ping 控制，够用且在 x/* 范围内；模块路径 `github.com/yang1997434/rex-voice`）。`gofmt -l . && go vet ./... && go test ./...` 是门禁。
- Swift 6 / SwiftUI，iOS 26 起（`project.yml` deploymentTarget，2026-11-02 按代码改正，原来写 18），只用系统框架（AVAudioEngine、CryptoKit、VisionKit 扫码、Network）。不引第三方包，不用 WebRTC。构建用 `xcodebuild`，没有 Xcode GUI。
- `~/data/archive/claude-terminal-20261029` 里的旧 iOS 项目是无许可的私有 fork：**代码一行不抄**，只参考发版做法（见 `docs/plan-2026-11-01.md`「Context」）。
- 工具层（`internal/tools`）是注册表，加新能力（AppleScript、computer-use）只加一组、不改调用方。
- 语音发指令的闸（2026-11-02 用户定）：发往 **shell 窗格**的文字含「生产 / 部署 / 上线 / 发布 / 回滚 / 迁移 / rm -rf / git push / sudo」直接拒；发往 **Claude 窗格**的一律放行，靠提示词「逐字复述 → 用户确认」（往 shell 窗格发普通命令不复述、直接发，2026-11-02 用户定），部署授权照常由那个 Claude 的 `~/.claude/hooks/prod-guard.py` 把关（语音说「部署 geo 生产」等于用户在那个会话里打了这句）。关窗格 / 关会话也要先复述确认，在跑的窗格不带 force 不关。
- 每次 TestFlight 上传写进 `docs/releases.md`：版本、build 号、commit、改了什么。
- 供应商只有千问和 Grok（0.3.7 起，2026-11-03 用户定）：`internal/voice/{openai,gemini,moss}` 留着（`internal/call` 的测试用 openai 假服务器），`serve.go` 不注册、iOS 枚举里没有；要加回来就补一行 blank import 和一个 case。MOSS 的坑（半个工具回路、没文字输入）记在协议 4.5。

## 部署

- **中继（ccs-maruiao）**：改生产，先要用户明文说「部署 rex-voice 中继」。步骤：本机 `GOOS=linux GOARCH=amd64 go build` → `scp` 到 `/opt/rex-voice/` → `systemctl restart rex-voice-relay` → `curl -A rex-voice-check https://r.maruiao.top/_voice/healthz`（不带 UA 会被该站的反爬 403）。中继以 systemd `DynamicUser` 跑、只听 `127.0.0.1:8790`。nginx 片段在 `scripts/nginx-voice.conf`，改 nginx 前先备份原文件。按总纲「部署分档」走：只换二进制算热修档。APNs（2026-11-02 上线）：`/etc/rex-voice/relay.env`（APNS_KEY_PATH=/run/credentials/rex-voice-relay.service/apns.p8、APNS_KEY_ID、APNS_TEAM_ID）+ `/etc/rex-voice/AuthKey.p8`（root 0600，key 来自 flow-cred `APNS_KEY_PATH`）+ drop-in `rex-voice-relay.service.d/apns.conf` 写 `LoadCredential=apns.p8:/etc/rex-voice/AuthKey.p8`；unit 备份在 `/etc/rex-voice/backups/`。重启后 journal 要有 `apns push enabled`，推一条要看到 `push ok`。
- **Mac 服务**：`rex-voiced setup` → `pair` → `install`（`uninstall` 卸），本机、可随时回退，不算生产。对外分发包用 `scripts/release-mac.sh <版本>`（签名 + 公证），装法见 `README.md`。
- **iOS**：`scripts/build-ios.sh <版本>`，用 ASC Admin key（`flow-cred ASC_ADMIN_KEY_ID / ASC_ISSUER_ID / ASC_ADMIN_KEY_PATH`），team `KAA6GJ59XS`，bundle id `com.maruiao.rexvoice`。发前先 `security unlock-keychain`，否则 archive 会卡死在签名。

## E2E 入口

还没有自动化的。真机验证靠 TestFlight 装到用户手机上，由用户按 `docs/plan-2026-11-01.md`「验收」和下面的清单逐条试；Mac 侧有文字模式 CLI（`rex-voiced chat`）可不用手机验证工具闭环，`rex-voiced serve -echo` 可不花 OpenAI 钱验证手机音频来回。

真机验收清单（草稿，2026-11-01，待主会话核）：

1. 配对：Mac 上 `rex-voiced pair`，app 右上「配对」→ 首次弹相机权限 → 扫码后回主屏，提示「配对成功，正在连接」，几秒内状态变「待机」。扫一个别的二维码：不跳回、不变；杀掉 app 再开，不用再扫，直接「连接中…」→「待机」。
2. 通话：按「开始通话」→ 首次弹麦克风权限 → 状态「接通中」→「在听」，形象换成听的表情；说一句话，状态依次「在想」「在说」、表情跟着换，听到回答，形象下方的转录卡片出现自己的话和助手的话（各最近一句）。
3. 打断：助手说话时插嘴，它的声音 1 秒内停下，状态回「在听」，接着回答新问题；扬声器外放时助手不会被自己的声音打断。
4. 锁屏 10 分钟：通话中按电源键锁屏、放口袋 10 分钟，期间能继续对话、不掉线；解锁后状态仍是「通话中」。
5. 蓝牙：通话中连上 / 断开 AirPods，声音和麦克风切到耳机 / 切回手机，通话不断；拔插有线耳机同样。
6. 挂断：说「挂断吧」，助手道别后 3 秒内通话结束，状态回「待机」，提示「已挂断」；按「挂断」按钮同样立即回「待机」。
7. 静音挂断：通话中一直不说话，约 60 秒时助手问「还在吗」，约 90 秒时自动挂断，提示「太久没说话，已自动挂断」。
8. Mac 不在线：Mac 上停掉服务（`rex-voiced uninstall` 或退出 serve），app 几秒内显示「Mac 不在线」、通话按钮变灰；重新开服务，状态自己回到「待机」，不用重开 app。
9. 网络切换：蜂窝网下也能连上（不依赖 Surge / Tailscale）；通话中 Wi-Fi 切蜂窝，短暂「连接中…」后自己恢复，通话继续。
10. 来电打断：通话中接一个电话或唤起 Siri，结束后回到 app，通话恢复（能听能说），或状态栏提示原因。
11. 设置页填 Qwen 的 key 并选 Qwen 模型 → 通话 → Mac 日志 `call started` 行里 provider=qwen、phone_key=true
12. 设置页选一个音色按试听 → 1–3 秒内听到一句打招呼、按钮经连接中 / 播放中回到试听；通话中按钮灰
13. iPhone Duo：合上时 app 铺到外屏边缘、页眉完整；展开后主屏两列、设置页居中；展开 / 合上来回切 3 次布局都对；横屏可用。
14. 新开窗格：说「在 rex-voice 新开一个 Claude」→ 该会话多一个窗格、claude 已起来；说「把它改名叫 test」→ 窗格标签变 test；说「关掉它」→ 助手复述确认后窗格消失；对一个正在跑的窗格说关，助手说它在干什么、不关。
15. 回答提示框：某个 Claude 弹 AskUserQuestion 或 plan 审批时，说「选第二个」「同意」→ 那个窗格选中对应项；说「取消」→ Esc。
16. 卡住提醒（通话中）：通话时让另一个 Claude 弹一个问题 → 5–10 秒内助手主动说「X 的 Claude 卡住了，问…」。
17. 卡住提醒（锁屏）：不在通话、手机锁屏，让一个 Claude 弹问题 → 手机收到推送「X 的 Claude 在等你」；同一窗格 10 分钟内不再推。
18. 图片与链接：说「把那张截图发我」→ 转录卡片下出现缩略图，点开可缩放；说「把汇报页链接发我」→ 出现可点链接，点开进 Safari。
19. 语音授权部署：对某个 Claude 窗格说「部署 geo 生产」→ 助手逐字复述、你说「对」→ 那句进了那个窗格，Claude 侧 prod-guard 认它为授权（不真部署，只看 Claude 的回应）。
20. 挂断：说「挂了」→ 助手整句道别说完才挂，没有被截断。
21. Duo 外屏竖栏（0.3.1）：合上时右侧状态图标下面空一大段、竖栏贴底：四家 logo 竖排 + 配对 / 设置；来电 / 录音指示条出现时不盖住栏顶；点 logo 切供应商、通话中点了抖一下不切；展开后竖栏靠右、左边两列。
22. 称呼（0.3.1）：设置页「基本」卡填称呼 → 试听和通话里助手用这个称呼；清空 → 退回 Mac 配置里的名字。
23. 掉线误报（0.3.1）：挂断后切后台一分钟再回来，不再出现「连接断开：操作无法完成」，状态自己回「待机」。
24. 附件不压扁形象（0.3.1）：Duo 外屏通话中收到两三张图，形象卡仍占四成以上屏高，下面那段能滚、挂断按钮不动。
25. 供应商轮盘（0.3.2）：竖栏只有一个 logo（官方图形）+ 家名；长按 0.35 秒弹出其余三家、圆盘整个在屏内、不压「配对」「设置」；滑到哪家勾徽章跟着走、松手选中并浮出家名；短按只抖一下；通话中长按不展开。切换后设置页「声音」里选中的也跟着变。
26. J 版通话换挡（0.3.2）：待机形象卡占满、无转录；接通后形象缩成顶部横幅、下面整段对话气泡，图片 / 链接作为气泡插在对话里；往上翻后来新消息不自动滚、底部出「回到最新」胶囊。
27. 轮盘第二版（0.3.3）：长按单 logo 后其余三家落在一条圆弧上（44pt）；手指不抬、绕圆心转，环跟着转，转到谁下面谁放大；松手直接选中（飞到圆心）；滑回圆心松手不换；外屏、内屏竖栏都和状态栏 Wi-Fi 图标同一竖线，内屏是 J 版不是两列。
28. 推送里直接回答（0.3.3）：不在通话、手机锁屏，让一个 Claude 弹 AskUserQuestion → 推送正文带编号选项；解锁后长按通知出「选 1 / 选 2 / 选 3 / 输入回复 / 取消」，按「选 2」→ 那个窗格选中第二项、推送消失；打开 app → 主屏有「X 的 Claude 在问」卡，按真实选项或打字发送同样生效；Claude 答完卡自己消失。纯文字提问只有输入框。
29. 邀请码（0.3.3）：中继配了邀请码后，旧配对的手机显示「中继要邀请码，请在 Mac 上重新 pair 后扫码」而不是一直「连接中」；`rex-voiced pair -force` 重扫后恢复「待机」。
30. 推送开关（0.3.5）：设置页「基本」卡里关掉「Claude 等我时推送提醒」，让一个 Claude 弹 AskUserQuestion → 手机不收推送，但打开 app 主屏仍有提问卡、通话中助手仍会说；重开开关再弹一题 → 推送回来。杀掉 app 再开，开关状态还在。
31. 作答不重复推（0.3.5）：在 Mac 上回答 AskUserQuestion，光标移到「Type something.」打几个字 → 手机只收到最初那一条推送，不再每 5 秒一条。
32. MOSS（0.3.6）：设置页网格第五家「MOSS」、填 key，没有音色卡和试听；主屏长按 logo 轮盘弧上四家都在屏内、能转到 MOSS 选中；通话：问「现在有几个 Claude 在跑」→ 它说一句过渡语后停住，再问「查到了吗」→ 报真实的窗格（和 OpenAI 答的一致）；插嘴它 1 秒内停；说「挂断吧」→ 它道别说完才挂；通话中让一个 Claude 弹题 → 主屏有提问卡、助手不口播只多一条「X 的 Claude 在等你回答」文字；Qwen 接通 1 秒内。
33. 0.3.7：设置页网格只有「千问」「Grok」两家（升级前选着 GPT / Gemini / MOSS 的，打开后变成千问，不空白）；主屏长按 logo 轮盘弧上只有一家、短弧在屏内；幽灵通话：通话中说「挂断吧」，助手道别时立刻锁屏、放一分钟再解锁 → 状态是「待机」、没有自己起通话、Mac 日志没有新的 call started；通话中 Wi-Fi 切蜂窝仍自己恢复、通话继续；千问选 qwen3.8（音色 Tina）：开一个 Claude、打招呼、说「把它关掉」→ 它要么关掉、要么说「它在干活，要强制关吗」，不会只说「我这就关」而没动作；选 qwen-audio-3.1 同样。
34. 0.3.8：通话中横幅计时下面出现「¥x.xx」并随对话涨（Grok 每 10 秒也涨）；挂断后设置页「费用」卡「今天」多一通、金额对得上 Mac `~/Library/Logs/rex-voice/calls.log` 最后一行；问「geo 的 Claude 做到哪了」→ 助手两句以内说结论、不念路径命令，Mac 日志有 `summary` 行（没存千问兜底 key 时是 `summary fallback` 警告、仍能答）；问「屏幕上现在显示什么」才念屏幕；一通电话打完 usage.log 里千问每次回答的入 token 中位数 ≤ 1800。

## 哪些文档不能信

- `docs/plan-2026-11-01.md` 是立项方案快照，做的过程中以代码和本文件为准；里面标「待 coder 核实」的都是没验证的。
