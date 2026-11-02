# 协议

## 1. 配对

- Mac：`rex-voiced pair` 生成 `id`（16 字节随机）和 `key`（32 字节随机），终端打二维码：`rexvoice://pair?relay=wss://r.maruiao.top/_voice/ws&id=<base64url>&key=<base64url>`；同时把 `{id, key}` 写 `~/Library/Application Support/rex-voice/pair.json`（0600）。
- app 扫码后把三项存 Keychain（`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`，不同步 iCloud）。
- 一次性：Mac 收到第一个成功解密的握手帧后记 `paired_at`，之后再来一个声称是新设备的握手就拒（要换手机重新 `pair`）。

## 2. 中继

- `GET /_voice/ws?role=mac|phone&id=<id>`，升级成 WebSocket。中继按 `id` 配对两端：一端连上等另一端，都在就双向透传二进制帧；任一端断，另一端收到关闭。同一 `id` 同一 role 只允许一条连接，新连顶掉旧连。
- 中继不解密、不存。`GET /_voice/healthz` → `ok`。
- nginx：`location /_voice/` 反代到 `127.0.0.1:8790`，`proxy_read_timeout 3600s`，带 Upgrade 头，不套 `limit_req`。

## 3. 手机 ⇄ Mac 帧（经中继）

`[1B type][4B seq big-endian][ciphertext]`

- 加密 ChaCha20-Poly1305，密钥 `HKDF-SHA256(key, salt="rex-voice", info="phone->mac" | "mac->phone")`，nonce 12 字节 = 4 字节 0 + 4 字节方向(0/1) + 4 字节 seq；seq 单调递增，收到回退或重复的丢弃并断开。附加数据 = type 字节。
- `type 0` 音频：PCM16 LE 单声道 24 kHz，每帧 20 ms（960 字节明文）。手机→Mac 是麦克风，Mac→手机 是模型输出。
- `type 1` 控制：JSON。
  - 手机→Mac：`{"t":"call.start"}`、`{"t":"call.end"}`、`{"t":"hello","app":"<版本>","device":"<机型>"}`
  - Mac→手机：`{"t":"status","state":"connecting|listening|thinking|speaking|ended","detail":"…"}`、`{"t":"transcript","who":"user|assistant","text":"…"}`、`{"t":"interrupt"}`（立即停播、清空播放队列）、`{"t":"hang_up","reason":"user|idle|error"}`、`{"t":"error","msg":"…"}`

## 4. Mac ⇄ OpenAI Realtime（WebSocket）

- `wss://api.openai.com/v1/realtime?model=<model>`，头 `Authorization: Bearer <key>`。
- `session.update`：`type: realtime`、`instructions`、`tools`、`tool_choice: auto`、`audio.input.format {type:"audio/pcm", rate:24000}`（只支持 24k）、`audio.input.transcription {model:"gpt-4o-mini-transcribe", language:"zh"}`、`audio.input.turn_detection {type:"semantic_vad", interrupt_response:true}`、`audio.input.noise_reduction {type:"near_field"}`、`audio.output.voice "marin"`。
- 送音频：`input_audio_buffer.append {audio: base64}`；收音频：`response.output_audio.delta`（base64 PCM16 24k）。
- 打断：收到 `input_audio_buffer.speech_started` → 给手机发 `interrupt`，并发 `conversation.item.truncate {item_id, content_index:0, audio_end_ms}`（WebSocket 模式服务端不会自动截断，`audio_end_ms` = 已经送给手机播放的毫秒数减去手机侧缓冲，先按已发送量算）。
- 工具：`response.output_item.done` 里 `item.type == "function_call"`；一轮攒齐，`response.done` 后并行执行，全部 `conversation.item.create {type:"function_call_output", call_id, output}` 送完再发**一次** `response.create`。用户插话导致的 `conversation_already_has_active_response` 是良性的。
- 省钱：答完一轮，把这轮的 `function_call_output` 条目删掉（事件名待核实：文档只写了 `conversation.item.truncate`；若无 delete，就用 `response.create {input:[...]}` 自定义输入 + `conversation:"none"` 的方式替代）。会话上限 60 分钟，55 分钟时新建会话并把最近 5 轮文字当 `instructions` 附带过去。
- 用量：每个 `response.done` 的 `usage` 记日志（input / cached / audio in / audio out），按官方单价折算写进 `~/Library/Logs/rex-voice/usage.log`。

## 5. 工具（移植自 `~/data/claude/voice-probe/tools.mjs`，已在实验里验证）

| 工具 | 参数 | 返回 | 实现要点 |
|---|---|---|---|
| `list_panes` | — | `[{session, block, label, project, app, state, doing}]` | `rex ls --json` → 每会话 `rex block ls -s <sid> --json` → 每块 `rex block call -s <sid> -b <bid> program_status`（`records[0]`: state / app / msg / owner.pid）；`project` = `lsof -a -p <pid> -d cwd` 的目录名。会话标签不一定是项目名。 |
| `read_pane` | `block`, `lines=40` | 屏幕最后 N 行文字 | `rex capture -s <sid> -b <bid>`，**必须带 `-s`**（服务不在那个会话里时按 block 找不到）；去尾空白、折叠连续空行、截 6000 字。 |
| `list_claude` | — | `[{pid, project, cwd, started, sessions:[{title,last_active}]}]` | `ps -ax -o pid=,lstart=,command=` 过滤 `claude` 进程；转录目录 `~/.claude/projects/<cwd 把非字母数字换成 ->/`。 |
| `read_claude` | `pid` 或 `block` | `[{title, last_active, last_user, claude_recent[2]}]` | 转录 JSONL：`type=user` 且非 `isMeta`、正文不以 `<` 开头的算用户指令；`type=assistant` 取 `message.content[].text`；标题取最后一条 `aiTitle`。先用 `lsof -p <pid>` 找进程打开的 `.jsonl` 精确对应，找不到退回按目录 mtime 两小时内最近两份。 |
| `send_to_pane` | `block`, `text` | `{sent}` 或 `{refused}` | 先过禁发正则（生产/部署/上线/发布/回滚/迁移/deploy/rm -rf/git push/sudo），`rex send -s <sid> -b <bid> <text>` + `rex send-key -s <sid> -b <bid> enter`。提示词要求模型先逐字复述、用户确认后才调。 |
| `hang_up` | `reason?` | `{ok}` | Mac 给手机发 `hang_up`，关 OpenAI 会话。提示词：听到「挂断吧 / 先这样 / 拜拜 / 不聊了」先说一句告别再调。 |

提示词（实验版，继续打磨）：中文、口语、像电话汇报；要调工具先说「我看一下」；问项目时把属于它的窗格全看一遍；Claude 原话用 `read_claude`，屏幕现状才用 `read_pane`；只讲结论和卡点，不念日志；附项目名表（geo / geo-us / metis-va / litscreen / shuqian / 26wiki / video / sub2api）和启动时生成的会话标签表。

## 6. 静音与挂断

- app：能量门限（RMS 低于阈值的 20 ms 帧不发，阈值真机调），连续 30 秒没发过帧给 Mac 发 `{"t":"status","state":"idle"}`。
- Mac：60 秒没收到音频 → 让模型说一句「还在吗？没事我先挂了」；90 秒 → `hang_up reason=idle`。
