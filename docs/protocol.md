# 协议

## 1. 配对

- Mac：`rex-voiced pair` 生成 `id`（16 字节随机）和 `key`（32 字节随机），终端打二维码：`rexvoice://pair?relay=wss://r.maruiao.top/_voice/ws&id=<base64url>&key=<base64url>`；同时把 `{id, key}` 写 `~/Library/Application Support/rex-voice/pair.json`（0600）。
- app 扫码后把三项存 Keychain（`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`，不同步 iCloud）。
- 一次性：Mac 收到第一个成功解密的握手帧后记 `paired_at`，之后再来一个声称是新设备的握手就拒（要换手机重新 `pair`）。

## 2. 中继

- `GET /_voice/ws?role=mac|phone&id=<id>`，升级成 WebSocket。中继按 `id` 配对两端：一端连上等另一端，都在就双向透传二进制帧；任一端断，另一端收到 `present:false`、连接保持。同一 `id` 同一 role 只允许一条连接，新连顶掉旧连（旧连被关，对端不收 `present:false`）。`role` 只能是 `mac` / `phone`，`id` 只许 `[A-Za-z0-9_-]{8,64}`，否则 400 不升级；不校验 `Origin`。单帧上限 64 KiB，超了断那条连接；中继每次写有 10 s 超时，写不出去就关那一端。客户端发来的文本帧中继忽略。
- 中继不解密、不存。`GET /_voice/healthz` → `ok`。
- 两端之间只走**二进制帧**（第 3 节的加密帧）；中继自己只发**文本帧**，内容 `{"t":"peer","present":true|false}`，对端上线 / 掉线时发给另一端，连上时也立即发一次当前状态。没有对端时收到的二进制帧直接丢。
- 保活：每端空闲时每 30 秒发一个**明文** `type 3` 帧 `[03][00000000]`（没有对端时中继丢掉，有对端时对端忽略），用来刷新 nginx `proxy_send_timeout` 并尽早发现死连接；中继→客户端方向没有保活，nginx `proxy_read_timeout 3600s` 到了会断，客户端按 1s→30s 退避重连即可（通话中双向都有音频，不会触发）。
- r.maruiao.top 的 server 块对空 UA 和 `go-http` / `curl` 等 UA 一律 403，WebSocket 客户端和健康检查都要带自定义 `User-Agent`（`rex-voiced/<版本>`、`RexVoice-iOS/<版本>`，健康检查 `curl -A rex-voice-check`）。
- nginx：`location /_voice/` 反代到 `127.0.0.1:8790`，`proxy_http_version 1.1` + Upgrade 头，`proxy_read_timeout`/`proxy_send_timeout 3600s`，`proxy_buffering off`，不套 `limit_req`。

## 3. 手机 ⇄ Mac 帧（经中继）

`[1B type][4B seq big-endian][ciphertext]`

- 加密 ChaCha20-Poly1305，密钥 `HKDF-SHA256(key, salt = phoneSalt ‖ macSalt（32 字节）, info="phone->mac" | "mac->phone")`，nonce 12 字节 = 4 字节 0 + 4 字节方向(0/1) + 4 字节 seq；seq 单调递增，收到回退或重复的丢弃并断开。附加数据 = type 字节。
- **会话盐（2c 落地，11-01 定）**：长期 key 直接派生会导致每次重连 nonce 重复、旧帧可重放。每条 WebSocket 连接建立后、以及每次收到中继的 `present:true` 后，双方各发一个**明文** `type 2` 帧 `[02][00000000][16 字节随机盐]`；本连接的两个方向密钥 = `HKDF-SHA256(key, salt = phoneSalt ‖ macSalt, info = 方向)`，seq 从 1 重新计。收齐两个盐之前不发、也不收 type 0/1 帧（收到就丢）；同一连接里再收到 type 2 就重新派生（对端重连了）。测试向量相应改成带两个盐（2c 已更新）。
  - Mac 侧实现（2c，`internal/call`）：中继连上时一定会立即推一次 `peer` 状态，所以 Mac **只在收到 `present:true` 时**发盐（没有对端时发了也会被中继丢掉），且**每次 `present:true` 都换一个新盐**、清掉对端盐和密钥、要求重新 `hello`——例外：两端几乎同时连上时中继会连发两次 `present:true`（2c 实测，`peerOf` 在发通知时才查），这时若还没收到任何手机盐，就**原样重发同一个 Mac 盐**而不换，否则手机会用一个 Mac 已丢掉的盐派生（所以手机必须忽略重复的 Mac 盐）；`present:false` 同样清掉。同一个 Mac 盐下收到**已经用过的**手机盐（重复发或被重放）直接忽略、不重新派生，否则 seq 归零会让本连接的旧帧可重放；收到没见过的手机盐才重新派生。盐帧长度不是 21 字节、或 seq 字段不是 0 的，丢掉并记 warn。手机侧（第 3 步）照同样规则做：每次 `present:true` 换新盐（还没收到 Mac 盐时重发原盐、不换），重复的 Mac 盐忽略，每次重新派生后都重发 `hello`（Mac 每次重新派生都会要求重新 `hello`）。
- `type 3` 保活：明文、无载荷，见第 2 节。
- `type 0` 音频：PCM16 LE 单声道 24 kHz，每帧 20 ms（960 字节明文）。手机→Mac 是麦克风，Mac→手机 是模型输出。
- `type 1` 控制：JSON。
  - 手机→Mac：`{"t":"hello","app":"<版本>","device":"<机型>","device_id":"<app 首次启动生成的随机 UUID，存 Keychain>"}`（每次连上、盐交换完先发）、`{"t":"call.start","provider":"openai|qwen|gemini|grok","model":"…","voice":"…","api_key":"…"}`（四个字段都可选，2026-11-01 用户定「手机管 key、选双工模型」：手机把设置页里的供应商 / 模型 / 音色 / 该家的 key 随 `call.start` 发来，Mac 只放内存、本次通话用完即丢，不落盘、不进任何日志和审计；没带的字段用 Mac `config.json` 的默认，`api_key` 没带时才用 Mac 钥匙串里的兜底 key；不认识的 `provider` 回 `error` 并保持 `ended`）、`{"t":"call.end"}`。一次性配对靠 `device_id`：Mac 第一次收到能解密的 hello 时把它记进 `pair.json`，之后 `device_id` 不同的 hello 一律回 `error` 并断开，要换手机重新 `pair`。
  - Mac→手机：`{"t":"status","state":"connecting|listening|thinking|speaking|ended"}`（`detail` 可选，Mac 目前不发，手机按可选解）、`{"t":"transcript","who":"user|assistant","text":"…"}`、`{"t":"interrupt"}`（立即停播、清空播放队列）、`{"t":"hang_up","reason":"user|idle|error"}`、`{"t":"error","msg":"…"}`

### 测试向量

`internal/pair/pair_test.go` 的 `TestVectors` 断言这三组，且断言下面的帧 hex 出现在本文件里；三组都用 phoneSalt = `101112…1e1f`（0x10–0x1f）、macSalt = `202122…2e2f`（0x20–0x2f），即 HKDF salt = `1011…2e2f` 共 32 字节。2026-11-01 改成会话盐后另用 Python `cryptography`（HKDF + ChaCha20Poly1305）独立重算一遍，结果相同。Mac 盐帧 = `0200000000202122232425262728292a2b2c2d2e2f`。Swift 侧照这三组做互通测试。约定：每个方向的首帧 seq = 1，接收方记「上一个通过校验的 seq」（初始 0），只有校验通过的帧才推进它；seq 到 2³²−1 后发送方不再发（防 nonce 重复）。配对 URL 里的 `id`、`key` 是**不带填充**的 base64url。

| # | key（hex） | 方向 | seq | type | 明文（hex） | 帧（hex） |
|---|---|---|---|---|---|---|
| 1 | `000102…1e1f`（0x00–0x1f） | phone->mac (0) | 1 | 1 | `{"t":"hello","app":"1.0","device":"iPhone"}` 的 UTF-8 | `0100000001cf157aea72a115abc5c127fdb119375ca71ca651a2ef7a09586971c4b055cb5ab9d484c0feac3a2cb3184af2492fcf576c2b891fb06fb968fa8e32` |
| 2 | 同上 | mac->phone (1) | 2 | 1 | `{"t":"ping"}` 的 UTF-8 | `0100000002bb7fdebff7ddfc8ac040de173242f85d62a57994c8c9ed473578f5ff` |
| 3 | `42` × 32 | phone->mac (0) | 0x01020304 | 0 | `0001020304050607` | `000102030427cab922deb03fa6d2de530290b8ced22f46ef267361583b` |

## 4. Mac ⇄ 实时语音提供方

Mac 侧把提供方抽成一个接口 `internal/voice`（`voice.go`）：`voice.Dial(ctx, voice.Config{Provider, Model, Voice, Key, Instructions, Tools, Call, TextOnly, Transcribe, Logger, Handlers, UsageLog, Version, Extra}) (voice.Session, error)` 按 `Provider` 分发到各家在 `init` 里 `voice.Register` 的实现；`Model` / `Voice` 空时取 `voice.Defaults(p)`（openai `gpt-realtime-2.1` / `marin`，qwen `qwen3.8-omni-flash-realtime` / `Tina`，doubao `1.2.6.1` / `zh_female_vv_uranus_bigtts`）；不认识的 `Provider` 返回 `voice.ErrUnknownProvider`，登记了默认值但还没实现的返回「还没实现」，没 key 直接报错、不拨号；`Extra` 放各家自己的选项（openai 的 `url`、`keep_tool_outputs`）。`voice.Session` 有 `AppendAudio / SendText / Cancel / SetMuted / Close / Done / Age / MaxAge / RecentTurns`；`voice.Handlers` 是 `AudioDelta(pcm) / TextDelta / AssistantText / UserText / SpeechStarted / ResponseDone(voice.Usage) / TurnDone / Error`；工具调用不走事件，提供方内部调 `Config.Call(ctx, name, args)` 拿结果。各家不一样的都关在提供方里：打断后截断（OpenAI 的 `truncate`）和丢掉被打断那条的迟到音频、工具事件格式、裁剪工具项。会话层（`internal/call`）只认这个接口：`provider / model / voice / api_key` 按 `call.start` > `config.json` > `voice.Defaults` 合并（`config.json` 的 `model`、`voice` 只在供应商和它的 `provider` 相同时才用），`api_key` 没带时用 Mac 钥匙串里该家的兜底 key（服务 `rex-voice`、账户 = 供应商名）；手机停发真实帧 ≥ 200 ms 时调 `SetMuted(true)`、再来真实帧调 `SetMuted(false)`（豆包要发 `input_audio_mute.commit`，OpenAI 是空操作）；会话活到 `MaxAge()` 前 5 分钟时轮换（0 = 不轮换）。音频统一走 PCM16 24 kHz；提供方要别的采样率就在提供方内部重采样（`voice.Down24to16`：24k→16k，先低通再抽取），手机和会话层不知道。已定要接：OpenAI Realtime（现有，`internal/voice/openai`）、阿里 Qwen-Omni Realtime、字节豆包 Realtime（协议细节等调研回来补成 §4.2 / §4.3）。

### 4.1 OpenAI Realtime（WebSocket）

- `wss://api.openai.com/v1/realtime?model=<model>`，头 `Authorization: Bearer <key>`、`User-Agent: rex-voiced/<version>`。连上先发 `session.update`，等到 `session.updated` 才算接通（10 秒没等到报错）。实现在 `internal/voice/openai`。
- `session.update`：`type: realtime`、`instructions`、`tools`、`tool_choice: auto`（没有工具时两项都不发），再按模式二选一：
  - 语音：`audio.input.format {type:"audio/pcm", rate:24000}`（只支持 24k）、`audio.input.turn_detection {type:"semantic_vad", interrupt_response:true}`、`audio.input.noise_reduction {type:"near_field"}`、开转写时加 `audio.input.transcription {model:"gpt-4o-mini-transcribe", language:"zh"}`、`audio.output.voice`（默认 `"marin"`；输出格式不设，用默认 PCM16 24k）。
  - 文字（`rex-voiced chat`）：`output_modalities: ["text"]`，不带 `audio` 块。
- 送音频：`input_audio_buffer.append {audio: base64}`；收音频：`response.output_audio.delta`（base64 PCM16 24k）。每轮回复定稿：语音模式取 `response.output_audio_transcript.done`，文字模式把 `response.output_text.delta` 攒到 `response.done`。
- 打断：收到 `input_audio_buffer.speech_started` → 给手机发 `interrupt`，并发 `conversation.item.truncate {item_id, content_index:0, audio_end_ms}`（WebSocket 模式服务端不会自动截断，`audio_end_ms` = 已经送给手机播放的毫秒数减去手机侧缓冲，先按已发送量算）。
- 工具：`response.output_item.done` 里 `item.type == "function_call"`（带 `id`、`call_id`、`name`、`arguments`），按 `response_id` 攒到这轮的 `response.done`，然后并行执行（每个 20 秒超时，超时的 output 是 `{"error":"工具超时"}`，照样送回），全部 `conversation.item.create {type:"function_call_output", id, call_id, output}` 送完再发**一次** `response.create`。用户插话导致的 `conversation_already_has_active_response` 是良性的，只记 debug。
- 省钱：一个 `response.done` 里没有工具调用、也没有工具批次还在跑，就算答完；这时把这条回答链上所有 `function_call` 和 `function_call_output` 条目用 `conversation.item.delete {item_id}` 删掉（服务端回 `conversation.item.deleted`，11-01 真跑确认）。`function_call` 的 id 取 `response.output_item.done` 的 `item.id`；`function_call_output` 的 id 由 Mac 自己在 create 时指定（`rv_out_<n>`），不靠回包——GA 版服务端回的是 `conversation.item.added` / `conversation.item.done`，没有 `conversation.item.created`。delete 带 `event_id: rv_del_<n>`，回来的 `error` 若 `event_id` 是这个前缀就忽略（删不存在的 item）。`KeepToolOutputs` 可关掉裁剪做 A/B。副作用（11-01 实测）：删掉后模型下一问要重新调 `list_panes`。会话上限 60 分钟，55 分钟时新建会话并把最近 5 轮文字当 `instructions` 附带过去（`Session.Age()` / `RecentTurns(n)`，由会话层做）。
- 用量：每个 `response.done` 的 `usage`（`input_tokens`、`output_tokens`、`input_token_details.{cached_tokens,audio_tokens,text_tokens,cached_tokens_details}`、`output_token_details.{audio_tokens,text_tokens}`）按 `internal/voice/openai/prices.go` 的单价折算（cached 按输入价的 1/10），一行 JSON `{time, model, input, cached, cached_audio, audio_in, text_in, output, audio_out, text_out, usd}` 写进 `~/Library/Logs/rex-voice/usage.log`；不在单价表里的模型 `usd:0` 并带 `unpriced:true`。

### 4.2 阿里 Qwen-Omni Realtime（WebSocket）

- 端点：默认旧端点 `wss://dashscope.aliyuncs.com/api-ws/v1/realtime?model=<model>`（11-01 实测北京 key 能用）；`Extra.workspace` 有值时改走 `wss://<workspace>.cn-beijing.maas.aliyuncs.com/api-ws/v1/realtime`，`Extra.url` 整体覆盖。头同 4.1。连上发 `session.update`，等 `session.updated`（10 秒），之前来的 `error` 算拨号失败。实现在 `internal/voice/qwen`。
- `session.update`：`instructions`、`audio.input.format {type:"pcm", sample_rate:16000}`（3.8 只收 16k）、`audio.output.voice`（**文字模式也必须带**，服务端默认的 `Chelsie` 3.8 不认，会回 error 并断开）、`tools`（OpenAI 扁平格式原样发，实测接受、模型能调；不用套 `function:{}`）；文字模式加 `modalities:["text"]`；语音模式加 `turn_detection {type:"semantic_vad"}`，`input_audio_transcription` 开转写时 `{model:"qwen3-asr-flash-realtime"}`、不开时发 `null`（否则服务端默认开 `gummy-realtime-v1`）。
- 上行：24k PCM16 经 `voice.Downsampler` 转 16k 再 `input_audio_buffer.append`。文字轮用 `conversation.item.create {type:"message", role:"user", content:[{type:"input_text"}]}` + `response.create`——文档没列 message 类型，实测可用。
- 下行：`response.audio.delta`（24k）→ `AudioDelta`；`response.audio_transcript.done` → `AssistantText`；`response.text.delta` / `.done` → `TextDelta` / `AssistantText`；`conversation.item.input_audio_transcription.completed` → `UserText`；`response.function_call_arguments.done`（带 `response_id`、`call_id`、`name`、`arguments`）按 `response_id` 攒到 `response.done` 后照 4.1 批处理（结果 `function_call_output`，最后一次 `response.create`）。
- 打断：`input_audio_buffer.speech_started` → `SpeechStarted` + `response.cancel`，并丢掉被打断那条回复（按 `response.created` 的 id）的迟到音频。没有 `truncate` / `delete`，不裁剪工具项；被取消的回复 `response.done.status = "cancelled"`，`audio_transcript.done` 仍给全文。实测空闲时 `cancel` 不回 error，只在 `response.created` 之前取消回 `Cannot cancel response before response.created`，这类含 cancel 的 error 吞掉；其余 `error` 报 `Handlers.Error` 并结束会话。
- 会话上限 120 分钟；`SetMuted` 空操作。
- 用量：`response.done.usage` 的 `input_tokens`、`output_tokens`、`input_tokens_details.{text_tokens,audio_tokens}`、`output_tokens_details.{text_tokens,audio_tokens}`（注意是 `tokens_details`），按 `internal/voice/qwen/prices.go` 北京价（元/百万 token：文本入 1.5、音频入 6、文本出 4.5、音频出 12，出处 https://help.aliyun.com/zh/model-studio/model-pricing ，按 7.2 折美元）写同格式 usage.log 行，`cached` 恒 0。

### 4.4 xAI Grok Voice（WebSocket，2026-11-01 照官方文档写，还没真跑）

- `wss://api.x.ai/v1/realtime?model=<model>&reasoning.effort=none`（默认 `grok-voice-3.0`；`Extra.reasoning` 改 effort，服务端默认 `high` 拖慢首包；`Extra.url` 覆盖端点），头同 4.1。连上发 `session.update` 等 `session.updated`（10 秒）。事件名同 OpenAI GA，实现在 `internal/voice/grok`（从 openai 包复制改的）。
- `session.update`：**不带** `type`、不发 `tool_choice`；`instructions`、`voice`（顶层，默认 `Ara`，不分大小写）、`tools`（扁平格式，同 4.1）；语音模式加顶层 `turn_detection {type:"server_vad", idle_timeout_ms:60000}`（不发 semantic_vad / interrupt_response / noise_reduction）、`audio.input.format` 与 `audio.output.format` 都是 `{type:"audio/pcm", rate:24000}`（24k 直通，不重采样）、开转写时 `audio.input.transcription {model:"grok-transcribe", language_hint:"zh"}`；文字模式只加 `output_modalities:["text"]`。
- 下行同 4.1，另外 `response.text.delta` 与 `response.output_text.delta` 都收（一轮里只认先到的那个名字，防止重复）；`conversation.item.input_audio_transcription.updated` 是累计全文，只存不报，`.completed` 来了以它为准报 `UserText`，没来就在 `response.created` 时把累计全文当 `UserText`（同一 item 只报一次）。
- 打断：`speech_started` → `SpeechStarted` + `conversation.item.truncate`（同 4.1），不发 `response.cancel`（server VAD 自己取消）。
- 工具：同 4.1 批处理，但这轮回复出过音频的，`response.create` 要等到它的 `response.output_audio.done`（或用户插话，最多等 10 秒）再发——xAI 说上一条音频没到齐就 create 会叠音；本地播放有没有播完不管。裁剪工具项同 4.1（`conversation.item.delete`）。会话上限 120 分钟，`SetMuted` 空操作。
- 用量：按连接时长计费（$0.08/分钟，含静音），每条 `SendText` 另加 $0.004，`function_call_output` 免费（`prices.go`）。`response.done` 只报 tokens、`usd` 记 0；`Close`（含拨号失败）时写一行 usage.log：4.1 的字段加 `seconds`，`input` / `output` 是整个会话的 token 合计，其余 0。


## 5. 工具（移植自 `~/data/claude/voice-probe/tools.mjs`，已在实验里验证）

| 工具 | 参数 | 返回 | 实现要点 |
|---|---|---|---|
| `list_panes` | — | `[{session, block, label, project, app, state, doing}]` | `rex ls --json` → 每会话 `rex block ls -s <sid> --json` → 每块 `rex block call -s <sid> -b <bid> program_status`（不支持 `--json`，输出本身是 JSON；取 `records[0]`: state / app / msg / owner.pid；调用失败或没有记录的窗格按 `app:"shell", state:"idle"`）；`state` 原样透传 Rex 的值（实测有 `working` / `done` / `blocked` / `idle`）；`project` = 进程 cwd 的目录名，cwd 先取 `~/.claude/sessions/<pid>.json` 的 `cwd`，没有才 `lsof -a -p <pid> -d cwd -Fn`。会话标签不一定是项目名。 |
| `read_pane` | `block`, `lines=40` | 字符串：屏幕最后 N 行文字 | `rex capture -s <sid> -b <bid>`，**必须带 `-s`**（服务不在那个会话里时按 block 找不到；`sid` 靠遍历 `rex ls` / `rex block ls` 找，找不到返回 `没有这个窗格: <block>`）；每行去尾空白、连续空行折成一行、去掉首尾空行，取最后 N 行，再截最后 6000 字。 |
| `list_claude` | — | `[{pid, project, cwd, started, name, status, session?:{title,last_active}}]` | `ps -ax -o pid=,lstart=,command=` 过滤命令首词是 `claude` 的进程（排除 `claude-*` 和 `.app/`），`started` 是 `lstart` 原文；`cwd`、`name`、`status` 优先取 `~/.claude/sessions/<pid>.json`，没有就 `lsof -a -p <pid> -d cwd -Fn`、`name` 空、`status:"unknown"`；拿不到 cwd 的进程不列。`status` 只取 busy / idle / blocked，其余记 `unknown`。`session` 取法同 `read_claude`（取 1 份），没有记录就不带这个字段。转录目录 `~/.claude/projects/<cwd 把非字母数字换成 ->/`。 |
| `read_claude` | `pid` 或 `block` | `[{title, last_active, last_user, claude_recent[≤2], status, session_name}]`（精确命中 1 个，退回猜测时最多 2 个，新的在前） | 转录 JSONL：`type=user` 且非 `isMeta`、正文不以 `<` 开头的算用户指令（截 500 字）；`type=assistant` 取 `message.content[].text`（每段截 1500 字，按时间先后）；标题取最后一条 `aiTitle`（实测在 `type:"ai-title"` 行），整份没有就取第一条用户指令前 60 字；`last_active` 是文件 mtime。**从文件末尾按 64 KiB 倒读**，攒够标题、1 条用户指令、2 段 assistant 就停，不整读。精确对应：读 `~/.claude/sessions/<pid>.json`（Claude Code 自己写的，字段 `sessionId`、`cwd`、`name`、`status` busy/idle…、`version`；格式没承诺，缺字段就走退回办法，不报错），转录就是 `~/.claude/projects/<cwd 转义>/<sessionId>.jsonl`，命中就用、不看多久没动；文件不存在或没有 `sessionId` 时退回按目录 mtime 两小时内最近两份；都没有返回 `两小时内没有活动的会话记录`。`status` = sessions 文件的 `status`，再叠加 Rex `program_status` 的 `state`：窗格 `blocked` 优先；sessions 文件没有状态时 `working`→busy、`done`/`idle`→idle；都没有为 `unknown`。传 `block` 时用该窗格的 `owner.pid`；只传 `pid` 时遍历窗格找同 pid 的那个取 state（找不到就不叠加，Claude 可能不在 Rex 里）。`session_name` = sessions 文件的 `name`。（11-01 验证：`lsof` 看不到转录文件，Claude 是开-追加-关，方案里的 lsof 做法作废。） |
| `send_to_pane` | `block`, `text` | `{sent}` 或 `{refused}`（审计写失败时多一个 `audit_error`） | 先过禁发正则（`生产\|部署\|上线\|发布\|回滚\|迁移\|deploy\|rm -rf\|git push\|sudo`，不区分大小写），命中返回 `{"refused":"这类指令不能用语音发，请用户回 Mac 手打"}`；否则 `rex send -s <sid> -b <bid> -- <text>`（带 `--`，防 text 以 `-` 开头被当成参数）+ `rex send-key -s <sid> -b <bid> enter`。发出和被拒都往审计流写一行 JSON `{time, tool, block, text, result:"sent" 或 "refused"}`。空 text 报错。提示词要求模型先逐字复述、用户确认后才调。 |
| `hang_up` | `reason?` | `{ok}` | 由会话层用 `tools.HangUpDef(run)` 注册（第 2b 步）：Mac 给手机发 `hang_up`，关 OpenAI 会话。提示词：听到「挂断吧 / 先这样 / 拜拜 / 不聊了」先说一句告别再调。 |

出错一律返回 `{"error":"…"}`（不超过 300 字），不让会话层报错：未知工具、参数不是 JSON 对象或带未定义字段、外部命令失败（取它 stderr）、工具内部 panic 都走这条，模型能看到原因。

提示词（实验版，继续打磨）：中文、口语、像电话汇报；要调工具先说「我看一下」；问项目时把属于它的窗格全看一遍；Claude 原话用 `read_claude`，屏幕现状才用 `read_pane`；只讲结论和卡点，不念日志；附 `config.json` 里 `name`（称呼）和 `project_aliases`（项目说明），以及每次 `call.start` 和会话轮换时现生成的标签表（在跑的 Claude 目录名 + Rex 会话名），见 `internal/voice/prompt.go` `Build`。

## 6. 静音与挂断

- app：能量门限（RMS 低于阈值的 20 ms 帧不发，阈值真机调），有声后再多发 300 ms（15 帧）防吞字尾；不发 `idle` 状态帧（Mac 自己按收帧计时）。
- Mac 补静音（11-01 真跑发现：手机停发后服务端 VAD 判不出用户说完）：通话中每 20 ms 检查，距上一个真实音频帧超过 200 ms（小于这个间隔算中继抖动，不插零）且不超过 5 s 时，往 OpenAI 追加 960 字节全零 PCM；超过 5 s 停止补（长静音不计费）。静音计时只看真实帧。
- Mac：60 秒没收到音频 → 让模型说一句「还在吗？没事我先挂了」；90 秒 → `hang_up reason=idle`。
