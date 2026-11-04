2026-11-01

- [ ] 第一批：中继 → Mac 服务（含文字模式 CLI 自测）→ iOS app → 联调，顺序和验收见 `docs/plan-2026-11-01.md`。
- [ ] 模型 A/B：同一组 20 句真实指令比 `gpt-realtime-2.1` / `gpt-realtime-2.1-mini` / `gpt-live-1`（准确率、首音延迟、每小时花费），定默认值，报表进 `docs/releases.md`。
- [作废 2026-11-02] 改全局 `~/.claude/hooks/prod-guard.py`：来自 rex-voice 的输入不算授权。用户 11-02 改定：语音可以授权部署，只靠复述确认；prod-guard 只加 rex-voice 项目行（见第三批）。
- [ ] 正式版上线后归档 `~/data/claude/voice-probe`（服务和实验用的 Tailscale grant 11-01 都已停/删）。
- [ ] 中继独立域名（`voice.maruiao.top`）：用户想要时在阿里云 DNS 加 A 记录指到 `r.maruiao.top` 同一台机，再 certbot 扩证书。
- [ ] 下一批候选：Claude 变 blocked 时主动来电（CallKit；推送已在第三批做）、整台 Mac 控制工具组（用户 11-02 定路线：Rex 这边的开发监控交流先做完整，再往整台 Mac 控制演化）。

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
- [x] 已做（用户 2026-11-02 真机：延迟都行，音质 Qwen ≈ Grok > GPT，记 docs/releases.md）四家语音 A/B（OpenAI / Qwen 3.8 与 audio-3.1 / Gemini / Grok）等 iOS 定稿实现上 TestFlight 后跑，20 句中文指令，记 docs/releases.md。
- [ ] 市场盘点页 /_r/7c04be05a07119c87544ad435850fff5/ 列的下一批候选：Gemini 3.8 Live、Grok Voice 3.0、StepAudio 3 Realtime（限免）；等用户定。
- [ ] `report-publish` 在本机会先撞系统 openrsync（不认 --chmod）：脚本在 claude-stack 仓，建议改成 /opt/homebrew/bin/rsync，等用户点头。
- [ ] iOS 设计稿 B、C 两版代码在定稿实现时删除，截图保留在 docs/design-specs/ios-liquid-glass/。
- [ ] 局域网直连 + 中继兜底（用户 2026-11-01 问「别人用是不是都走我的中继」）：Mac 在 LAN 监听、二维码同时带 LAN 地址，手机先直连再回退中继；在家不经中继。要用户点头再做。
- [x] 已做（用户 2026-11-02 定 app 侧羽化不重出图：PersonaView 左右和顶部 16% 渐隐，checker 核过 kimi 右侧仅剩极淡残留）新形象 5 组原图裁平露硬切线。
- [x] 已做（用户 2026-11-02 定：接受 40 MB、640px）形象资源进 app 包；上架前再看要不要按需下载。
- [ ] 第三方参考图（aigengtu 三视图、ZipZipPipe 插画、moegirl 快照）只在本机 `docs/design-specs/personas/_refs/`，已 gitignore；换机器要重新按各 `source.md` 下载。
- [ ] Qwen 国际站默认：国内网络下 audio-3.1-plus 首包延迟没量；A/B 时和 3.8（北京）对比记 docs/releases.md。
- [ ] 音色试听（`voice.preview`，Mac 端 2026-11-02 做完）：OpenAI、Qwen 国际站真跑过（`go test -tags manual -run TestPreviewReal ./internal/call/`）；Gemini、Grok 没真跑。Grok 语音模式下 `SendText` 是否直接出音频没核（每条 `conversation.item.create` 另收 $0.004，外加连接分钟），上 Grok 试听前先真跑一次。
- [x] 已做（用户 2026-11-02 真机：展开后两列正常）iPhone Duo 内屏 / 横屏没有真实截图：Device Hub 只能 GUI 操作，osascript 被辅助功能权限挡住（2026-11-02）。要么用户给终端开「辅助功能」+「自动化」权限后补拍，要么以用户真机（0.2.1）验收清单第 13 条为准。
- [ ] 配对页横屏时扫描框和底部引导卡片只差约 1pt（coder 2026-11-02 报），真机横屏看一眼。

## 接着从哪做（2026-11-02 凌晨收口）

- 状态：main 2979ef9；TestFlight 0.2.1（build 202611020816）装在用户 iPhone Duo 上，配对、试听、Duo 两列、四家通话都真机过了；Mac 服务 `~/.local/bin/rex-voiced` 是 main 的开发构建、launchd 在跑；中继生产版本仍是 2026-11-01 部署的那个（本批没改中继）。
- 下一步（用户 2026-11-02 定）：**功能和语音对话实测**——按 CLAUDE.md「真机验收清单」1–13 条逐条过，重点是真实用法：读 Rex 窗格 / Claude 进展、往会话发指令、语音挂断、打断、锁屏 10 分钟、蓝牙、静音挂断、Mac 离线恢复、网络切换。每条记结果进 docs/releases.md「真机反馈」，问题回派 coder。
- 候选：Grok 试听真跑一次；局域网直连（等点头）；Gemini 默认排序看用户反馈；配对页横屏 1pt 间距。
- 未答：`secret-add LOGIN_KEYCHAIN_PASSWORD`；report-publish 的 rsync 修法；Duo 内屏模拟器截图要辅助功能权限（真机已验，可不做）。

## 2026-11-02 第三批（Rex 全操作 + 卡住提醒 + 图片链接 + 语音授权部署）
- [已做] 方案 `~/.claude/plans/curious-zooming-lynx.md`；A 工具组 / B 通话层 / C 中继 APNs / D iOS 四份 brief 并行做完合入 main（f804dd3）；E prod-guard 加 rex-voice 行（37 测试绿，备份 `~/.cache/prod-guard.py.bak-20261102`）；iOS 0.3.0（202611020941）已上 TestFlight；Mac 服务已换新版。
- [已做] APNs key 复用 Claude Terminal 那把（团队级，flow-cred `APNS_KEY_ID / APNS_TEAM_ID / APNS_KEY_PATH`）。
- [已做 11-02] 中继部署（用户授权「部署 rex-voice 中继」）：新二进制 + unit + relay.env + AuthKey.p8 + LoadCredential drop-in；journal `apns push enabled`，用临时 Claude 弹 AskUserQuestion 真推了一条，中继 `push ok`（Apple 已收）。步骤写进 CLAUDE.md「部署」。
- [已做 11-02] reviewer 二审：P1 Claude 退出后窗格仍标 claude-code 会放行（加 owner.pid 存活判断）、P2 close_pane 查状态出错当可关（改拒）、P2 send_keys 对 shell 窗格能按 enter（非 Claude 窗格只许 ctrl+c / escape）、P2 拨号中卡住会多推一条（改跳过）、P3 `/data/claude/geo` 这种经符号链接的路径被拒（按真实路径比）、P3 show_file 相对路径（改拒）。其余处置见 `REVIEW.md`。
- [ ] 真机验收 CLAUDE.md 清单 14–20，结果记 `docs/releases.md`「真机反馈」；Grok / Gemini 的挂断延后只测过 OpenAI 假服务端。
- [已做 11-02] `internal/relay` 的 `TestSlowPeerOnlyClosesPeer` 偶发红：根因是中继先把新连接登记进 map 再发对端通知，对端转发的帧能抢在通知前面；改成先发通知再登记、对端是新来的才补发，40 次全绿。
- [ ] 卡住提醒的两个边角：模型正在说话时 SendText 可能撞 `conversation_already_has_active_response`（没排队）；Mac 没连上中继时 notify 丢掉但仍占 10 分钟配额、不补发。真机碰到再修。
- [ ] Workspace 目录写死 `~/data/claude`，没接 config.json；要配置时在 internal/config 加字段、cmd 透传。
- [ ] send_keys 白名单按实测删了 y / n（Claude 提示框不认）；shell 的 `[y/N]` 用 send_to_pane 发 y。
- [ ] iOS：aps-environment 写死 production，Debug 装真机可能和开发描述文件冲突（TestFlight 不受影响）；横屏 / Duo 展开时附件卡片布局没真机看；push_token 只在 WebSocket 建立时发、不随 present 重发。
- [ ] 推送标题 / 正文没限长，超 4 KB 会被 APNs 413（现在正文截 100 字，实际到不了）。
- [ ] `docs/plan-2026-11-01.md` 没更新本批内容，以 CLAUDE.md 和 protocol.md 为准。

## 2026-11-02 0.3.1（真机反馈第二轮）
- [已做] 千问「说发了没发」：四家文字 / 语音（fakephone 多轮 + 插话）都能调工具，用户再打一通也正常；加了每次工具调用和每句转录的 Mac 日志，再出现看日志定位。
- [已做] 复述规则改成只对 Claude 窗格和敏感词复述，shell 普通命令直接发。
- [已做] 工具返回去噪（list_panes 不带命令原文；read_claude 只给结论，verbose 才给原文）；call.start / voice.preview 可带称呼；Mac config name 改 LeBron。
- [已做] iOS 0.3.1：H 版右侧安全区竖栏定稿（设计稿 F / G / H 见 docs/design-specs/ios-v3-sidebar/，挑选页 /_r/94a219555e3b57acd113652e290bfa6e/）；称呼设置；掉线误报；音色锁定；形象卡最小高度。
- [ ] 真机验收 CLAUDE.md 清单 21–24；竖栏顶写死在屏高 45%，来电 / 录音指示条会不会盖到要真机看。
- [ ] 供应商 logo 现在是自绘占位（品牌色圆底 + 白字形）；用户给官方素材后换 ProviderLogo.glyph。
- [ ] read_claude 去噪的听感（「代码」「某文件」「链接」替换词）要用户真机用几天再调。
- [ ] 测试会话 voicetest-sess（窗格 voicetest）还开着，给语音测试用；不用了 `rex session kill`。

## 2026-11-02 第四批（通话质量 + 设计稿 J/K）
- 已做：千问「Cannot create response while user is speaking」不再断线（speech_stopped 后补发一次）；block / session 模糊解析（子串 → 编辑距离，错 id 直接报错不猜）；提示词改 `/model <别名>`、选择框按数字键、切模型 / 档位先说「这会改默认」、确认必须来自用户下一句。
- [已定] `/model` 与 `/effort` 会改所有新会话默认：用户 2026-11-02 说那只是测试指令能不能直通，不改；保持「发前提一句会改默认」。
- [已定] 第四轮定 J（2026-11-02 用户定），K 不做、环境卡协议消息不做。
- [已做] 0.3.2（build 202611021233，commit 211109b）：J 定稿、轮盘收小（扇形转到 105/150/195°、半径 60、磨砂圆只包三家）+ 勾徽章 + 家名、官方 logo（lobehub icons-static-svg，MIT）。J 通话右上黑点查明是模拟器逐帧动画自带，不是 app 画的（J/K worktree `agent-aac6c836a82f56a74` 未合）；轮盘展开时磨砂圆底右边出屏、K 轮盘盖住环境卡；J duo 通话右上黑点来源待查；本仓 E2E 清单补轮盘一条。
- 测试记录：四家同一套四轮语音测试（开标签 → 切模型 → 看模型 → 关掉挂断，合成语音）结果见 vault 待补；Grok 的 ASR 把第一句中文听成「Set voice to」。
- [ ] 真机验收 CLAUDE.md 清单 25–26（轮盘、J 版换挡）；没核实：滑选时勾跟着走、VoiceOver 是否把家名读两遍、触感。
- [已做 0.3.3] 推送里直接回答 Claude 的提问——Mac 把 blocked 窗格的编号选项塞进推送，手机通知按钮「选 1/2/3」「取消」「输入回复」+ app 内一张提问卡（真实选项按钮 + 输入框），不用打电话；协议加手机 → Mac 的 `answer` ctl（block + keys 或 text），Mac 复用 send_keys / send_to_pane 闸门。纯文字提问只能走输入回复。
- [已做] 0.3.3（build 202611021320，commit 4fe5f40）：轮盘第二版（圆弧拨盘、跟手转、松手即选）；竖栏按右安全区居中再左移 6pt（外屏实测）、内屏 J 版；推送里直接回答（ask / ask_done / answer，通知动作 + AskCard）；中继邀请码（RELAY_INVITES）。Mac 服务已重装、config.json 已带邀请码、新二维码在 Rex 标签页「配对二维码 0.3.3」。
- [已做 2026-11-02] 中继部署（用户明文授权）：新二进制 + relay.env `RELAY_INVITES`，journal `invites required (1 codes)`；手机已重扫。
- [ ] 内屏没在模拟器核过（Duo 模拟器展不开）：竖栏对齐和 J 版布局等真机；差得远就按真机量的偏移改 `ContentView.swift` 里那个 -6pt。
- [ ] `TestDropWithoutPeer` 约 5% 偶发失败（改动前就有，lost 帧和手机上线抢时序），下一批修。
- [ ] AskUserQuestion 一次多道题：窗格一直 blocked，Mac 只发第一题的 ask；下一批看 capture 能不能分出第几题。
- [ ] Mac 连中继失败的日志里带完整 URL（含邀请码）；下一批打码。
- [ ] 设置页「声音」小节右上说明文字还是「通义 Qwen」。
- [已做] 0.3.4（build 202611021415，commit a09034a）：锁屏点通知崩溃（通知代理回调改主线程隔离，记忆 ios-notification-delegate-nonisolated-crash）；多题每题都推、题干与选项同一次截屏、Submit 页题目「提交答案？」；邀请码打码；设置页「千问」。Mac 安装包 v0.3.4 挂在 GitHub Releases；朋友上手流程 docs/getting-started.md。
- [ ] 真机验：0.3.4 锁屏点通知不崩；连问两题每题一张卡、题目选项对得上；内屏竖栏对齐。
- [ ] Rex 下载地址：getting-started.md 里写「向分享人要」，确定官方地址后补上。
- [已做 2026-11-02] TestFlight 外部测试组「朋友」公开链接 https://testflight.apple.com/join/jJmrkutd ，0.3.4 beta 审核 22:42 APPROVED（提交后约 8 小时）；内部组「rex」照旧。后续版本加进「朋友」组会再审一次（通常只有首次慢）。

## MOSS-Live（2026-11-02 调研；2026-11-03 用户定接入，只能聊）

- 模思智能 11-02 发布的全双工实时语音，闭源、只有云 API（`wss://api.mosi.cn/v1/realtime?model=moss-live-1.0`），限时免费。协议和 `voice.Session` 接口能对上（16k 上行 / 48k 下行 PCM、`mic_state`、`response.interrupted`、播放回执），但 API 没有开发者 tools、没有文字输入事件，所以接进来只能「听和聊」，不能操作 Rex。
- 用户 11-02 定先不接；11-03 推翻、定直接上；同日探出它其实有工具调用。文档：https://platform.moss.cn/docs/reference/moss-live
- [已做] 0.3.6（Mac bcffa81 / iOS 0d89fa9）：`internal/voice/moss` 第五家（协议 4.5：事件名字段 `action`、chunk 分片帧、回执流控、关键词挂断、开场快照进 instructions）；iOS 枚举 / 轮盘四槽 / `logo-moss`（照官方位图描的线稿）/ MOSS 不显示音色卡和试听；Qwen 默认只连国际站（用户 11-03 定；之前「记住端点」的做法撤了，北京 key 用 `Extra.region = cn`）。真 key manual 测试三连过，说完 → 开口 0.75 秒。
- [ ] 真机验 E2E 32；轮盘弧变 120° 后展开时会盖住「开始通话」右上角（通话中不展开，不影响操作），用户看截图定要不要收弧。
- [已做] 工具：11-03 探出 MOSS 其实收 `session.tools`、发 `response.function_call_arguments.done`、收 `conversation.item.create function_call_output`（不发 `response.create`），文档没写；接上后开场快照 / BuildChat / 关键词挂断都删了。**但它收了结果不自己接着答**，要用户再问一句才用（六种推法都试过，协议 4.5）；仍没有文字输入（试听、口播提醒做不了）。用户 11-03 定：留着、接受多问一句，不藏。下一步看模思什么时候把回路补上（`response.create` 不再拒，或开文字输入），到时去掉「再问一句」。manual 第二次跑有一轮在第 31 个 chunk 后服务端停发（没复现），再遇到就把 `moss: send` / `moss: audio` 日志整段存下来发给模思。
- [已做] 第一句延迟（用户 11-03 反馈）：Qwen 的根因是先连北京、等拒绝（实测 4.5 秒）才连新加坡，改成默认只连国际站；连上后说完到开口 0.2 秒。OpenAI 拨号 1.3 秒、说完到开口 0.4 秒，Mac 转发不缓冲、手机收到即播；「偶尔慢」还没抓到样本，再遇到记下时间我查 debug 日志。
- [已做] 0.3.5（build 202611030931，commit 4ecff6f）：设置页「Claude 等我时推送提醒」开关（ctl `prefs{push}`，Mac 只放内存、手机每次连上重发）；Mac 上作答打字时不再每 5 秒重推（ParseBox 把输入框那行整行丢掉，根因见 docs/protocol.md）。本机 rex-voiced 已重装 0.3.5-dev。
- [ ] 真机验 E2E 30、31（推送开关、作答不重复推）。

## 0.3.7（2026-11-03 用户：撤三家、幽灵通话、千问 3.8 关窗格）

- [已做] 供应商只留千问 + Grok（用户 11-03 定「GPT、Gemini、MOSS 都撤掉」）：Mac `serve.go` 不再注册三家、默认供应商改 qwen、`setup` 只认 qwen | grok；包和 `voice.OpenAI` 等常量留着（`internal/call` 测试用 openai 假服务器）；iOS 枚举只剩两家、轮盘单槽、三家 logo 删掉。要加回来：serve.go 一行 blank import + iOS 一个 case + logo。
- [已做] 锁屏后自己起通话（用户 11-03 真机两次）：根因是手机锁屏时 socket 已死、没收到 Mac 的 `hang_up`，`inCall` 还是 true，重连后 `.ready` 把存着的 `call.start` 原样重发（CallController 旧 336 行）。改法：Mac 每次 hello 后必回 `status`（有通话是当前状态、没有是 `ended`），手机重连后不再重发、等这条，`ended` 或 5 秒没等到就结束并提示；正常挂断仍靠 `hang_up` 放完道别。
- [已做] 千问 3.8 说「我这就关」却没调 close_pane（用户 11-03 真机三次）：真机日志里那一轮没有任何工具事件；同一通电话里 3.8 两次发来截断的 `function_call_arguments.done`（`read_claude` 参数只剩半截、调失败后它才重发）。改法：`response.done` 的 `output[]` 里的 `function_call` 项也认（补没来的、替换截断的）；提示词加「没调工具不许说做了」。文字模式复现 3 次 3.8 都调了 close_pane（两次被拒：刚打完招呼的 Claude 还在 working，它问要不要强制）；语音假手机 3.8 第二轮没触发服务端 VAD（听不到第二句），3.1 / Grok 语音整套开关挂都过。3.8 真机还要用户再试 E2E 33。
- [已做] `rex-voiced chat -voice`（3.8 只认 Tina，之前没法在文字模式测它）；假手机 `-invite` / `REXVOICE_INVITE`（中继开了邀请码后假手机连不上）。
- [ ] 真机验 E2E 33；3.8 若仍关不了，下一步下掉 3.8 或只留 3.1（用户定）。
- [ ] close_pane 对刚打完招呼、正在回「你好」的 Claude 会以 working 拒掉，助手只能问「要强制吗」；要不要把「只是在回话、没在跑工具」的 Claude 放行，用户定。

## 0.3.8（2026-11-04 用户：通话费用、只说结论、read_claude 口语总结、省 token）

- [已做 11-04] 方案 `~/.claude/plans/curious-zooming-lynx.md`（11-04 批准）：Mac 每次回答后发 `cost`、hello 后发 `stats`（新 calls.log，历史不回填）；`read_claude` 的 reply 用 qwen-flash 压成两句（Mac 兜底 key，没 key 回退规则版）；提示词 ≤ 700 字、工具定义 ≤ 2500 字、工具输出裁剪、上一轮入 token ≥ 6000 就轮换会话；iOS 横幅金额 + 设置页「费用」卡。改前千问 3.1 plus 每次回答入 token 中位 3350，改后文字模式同样五问 2476（首轮底座 1716 = 提示词 698 字 + 工具定义 2494 字，所以 ≤ 1800 的目标到不了；再降只能砍工具）。回答都两句以内、不念路径；read_claude 总结每次约 1 秒、不到 0.0005 美元。Mac 钥匙串 11-04 存了千问国际站兜底 key（总结用它）。
- [ ] Jev（TypeSafe）判「Claude 事件值不值得现在打断用户」：用户 11-04 定本批不加，省 token 做完看效果再说。
- [ ] 本文件已超 10KB，下次收口把已做条目挪进 vault（挪前列给用户看）。
- [ ] 真机验 E2E 34（横幅金额、费用卡、两句结论）。
- [ ] 工具描述压缩后模型会不会用错 read_pane / read_claude：文字模式五问都选对了，真机多用几天再看。
- [ ] 轮换阈值 `rotateInput = 6000` 是拍的，看真机通话 usage.log 再调。
