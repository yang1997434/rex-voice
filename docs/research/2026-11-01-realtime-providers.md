# 实时语音提供方调研（2026-11-01）

给 `internal/voice` 接 阿里 Qwen-Omni Realtime 和 字节豆包 Realtime 3.0 用。两路 researcher 只读官方文档得出，标「未核实」的要在接入时拿真实返回对一遍。现有基线是 OpenAI Realtime（`docs/protocol.md` §4.1）。

## 共同点（决定接口怎么设计）

| 项 | OpenAI | Qwen 3.8-omni-flash-realtime | 豆包 3.0（Seeduplex，`model: "1.2.6.1"`） |
|---|---|---|---|
| 上行音频 | PCM16 24k | **只收 16k** PCM16（3.5 系列可 24k） | **只收 16k** PCM16（或 Opus），20ms=640B，**必须按实时节奏发** |
| 下行音频 | PCM16 24k | PCM16 24k | 24k，**默认 ogg_opus，要显式设 `pcm_s16le`**（`pcm` 是 float32） |
| 协议 | JSON 事件（GA 名） | JSON 事件（OpenAI **beta** 名） | JSON 事件（名字像 OpenAI，细节不同） |
| 打断 | speech_started + cancel + truncate | speech_started + cancel，**无 truncate** | `conversation.item.input_audio_transcription.started` 当开口信号 + `response.cancel`→`response.canceled`，无 truncate |
| 工具 | 扁平 tool 定义；`function_call_output` + `response.create` | 定义多套一层 `function:{}`（未核实）；结果同 OpenAI | `response.function_call_arguments.done` 里 `items[]` 一次多个；结果 `conversation.item.create role:"tool"` 带 `call_id`，全部回传后模型自动继续（是否还要 `response.create` 未核实） |
| 删会话项 | 有 | **无** | 有 `conversation.item.delete`（字段未核实） |
| 会话时长 | 60 分钟 | 120 分钟；音频保留 100 轮 / 600 秒 | 没查到；闲置 10 分钟断 |
| 静音 | 不用管 | 不用管 | 关麦**必须**发 `input_audio_mute.commit` / `input_audio_unmute.commit`，否则超时不再回应 |
| 关闭 | 直接断 | 直接断（5–10 秒才真关） | 先 `session.close` 等回复再断，否则 55000001 |

## Qwen-Omni Realtime

- 端点：`wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1/realtime?model=…`（新）；旧 `wss://dashscope.aliyuncs.com/api-ws/v1/realtime` 是否还能用未核实（先试旧的，不行再查 WorkspaceId）。头 `Authorization: Bearer <DASHSCOPE_API_KEY>`，key 绑地域。本机 `flow-cred QWEN_API_KEY` 是北京地域、11-01 实测能列出 `qwen3.8-omni-flash-realtime`。
- session：无 `session.type`；`audio.input.format {type:"pcm", sample_rate:16000}`；`audio.output.voice`（默认 `Tina`）；`turn_detection {type: server_vad|semantic_vad|semantic_vad_ec, threshold, silence_duration_ms(默认 800)}`，`interrupt_response` 默认 true；输入转写要配 `input_audio_transcription {model:"qwen3-asr-flash-realtime"}`（固定值）。
- 事件名：`response.audio.delta`、`response.audio_transcript.delta/.done`、`response.text.delta/.done`、`response.function_call_arguments.delta/.done`、`conversation.item.input_audio_transcription.delta/.completed`（delta 里 `text` 已确认 + `stash` 草稿）、`input_audio_buffer.speech_started/.stopped`。`response.cancel` 在没有进行中回复时会回 error（要容忍）。
- 没有：`conversation.item.delete/truncate`、`tool_choice`、message 类型的 `conversation.item.create`（不能塞系统 / 用户文本）→ 会话轮换时「最近 5 轮」只能塞进 instructions。
- 工具和 `enable_search` 互斥。
- 价格（北京，元/百万 token）：文本入 1.5、音频入 6、文本出 4.5、音频出 12，100 万 token 免费额度（90 天）。音频入每秒 7 token、出每秒 12.5 token；音频和文本输出分开计费。
- 音色：Tina（默认）、Liora Mira、Raymond、Ryan、Mia、Serena、Evan、Momo…；3.8 独有 Zane / Cici；方言若干。
- 出处：https://www.alibabacloud.com/help/zh/model-studio/realtime 、/client-events 、/server-events 、/omni-realtime-interaction-process 、https://help.aliyun.com/zh/model-studio/model-pricing 、/omni-voice-list

## 豆包 Realtime 3.0（Seeduplex）

- 产品在「豆包语音」（不是方舟），控制台「API Key 管理」拿 key；头 `X-Api-Key`（旧四头鉴权也支持但要下线）。**2026-06 发布稿说「邀测」，现在是否开放未核实**——要用户在 https://console.volcengine.com 的豆包语音里开通并给 key。本机只有 `VOLC_ASR_*` / `VOLC_TTS_*`，不是这个。
- 端点：`wss://openspeech.bytedance.com/api/v3/duplex/realtime/dialogue`（带 `/duplex/` 的才是 3.0；没有的是老二进制协议，不接）。
- 流程：`session.create {model:"1.2.6.1", instructions, tools, audio:{output:{format:{type:"pcm_s16le", rate:24000}, voice}}}` → `session.created`（带 `session.id`）；`session.update` 是全量覆盖；`input_audio_buffer.append`（base64 PCM16 16k）；关麦 `input_audio_mute.commit`；结束 `session.close`。
- 下行：`conversation.item.input_audio_transcription.started/delta/completed/failed`、`response.output_text.delta/done`、`response.output_audio.started/delta/done`、`response.function_call_arguments.done {items:[{call_id,name,arguments}]}`、`response.canceled`、`response.done`（用量，字段未核实）、`error`。字段名官方页没全给，**以官方 demo 为准**（文档要登录才看全）。
- 上下文 64K tokens；注入历史必须 user/assistant 成对、≤20 轮；`session.update` 回传 `session.id` 可续接最近 20 轮。
- 限流：QPM 60、TPM 10 万。5xx 重连、4xx 不重试。
- 价格（元/百万 token，是否覆盖 3.0 未写）：音频入 80、文本入 10、缓存 5、音频出 300、文本出 80。音频入每秒 6.25 token、出每秒 25 token。
- 音色：`zh_female_vv_uranus_bigtts`（Vivi 2.0）、`zh_female_xiaohe_uranus_bigtts`、`zh_male_m191_uranus_bigtts`（云舟）、`zh_male_taocheng_uranus_bigtts`（小天）；文档例子又用 `_jupiter_` 后缀，3.0 该用哪个未核实（错了报 `InvalidSpeaker`）。
- 已知坑：模型可能把工具名念出来（instructions 压住）；`session.create` 可能拒收不认识的 JSON Schema 关键字；默认严格审核。
- 出处：https://docs.volcengine.com/docs/DoubaoVoice/endtoend-realtime-voice-full-duplex-version?lang=zh 、https://docs.volcengine.com/docs/doubao-voice/access-must-read?lang=zh 、/billing-instructions 、/tone-list-3 ；第三方 https://github.com/kikilomas95-create/seeduplex-web-demo 、https://github.com/SpaceEggs/agentic-call-control/issues/2

## Google Gemini Live API（2026-11-01 researcher，官方 ai.google.dev，没真跑）

- 模型 `models/gemini-3.8-live`（Stable；`-extended-thinking` 只能异步工具，`thinkingLevel` low/medium/high）。端点 `wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent?key=<key>`（key 在 URL 里，**不能进日志**）。
- 每条客户端消息恰好一个顶层键：`setup`（第一条）/ `clientContent` / `realtimeInput` / `toolResponse`。`setup {model, generationConfig:{responseModalities:["AUDIO"], speechConfig:{voiceConfig:{prebuiltVoiceConfig:{voiceName}}}}, systemInstruction:{parts:[{text}]}, tools:[{functionDeclarations:[{name,description,behavior,parameters}]}], realtimeInputConfig:{automaticActivityDetection:{disabled,startOfSpeechSensitivity,endOfSpeechSensitivity,prefixPaddingMs,silenceDurationMs}}, inputAudioTranscription:{}, outputAudioTranscription:{}, sessionResumption:{handle?}, contextWindowCompression:{slidingWindow:{}}}`。文档矛盾：`responseModalities` 在 get-started 示例里放 setup 顶层、API 参考放 generationConfig（真连核）。
- 上行 `{"realtimeInput":{"audio":{"data":<b64>,"mimeType":"audio/pcm;rate=16000"}}}`（文档称可标实际采样率让服务端重采样，未实测）；文字轮 `{"clientContent":{"turns":[{role:"user",parts:[{text}]}],"turnComplete":true}}`（会打断当前生成，不用另发 response.create）；`realtimeInput.audioStreamEnd:true` 停顿 >1s 时发。
- 服务端消息：顶层可带 `usageMetadata`、`voiceActivity`，恰好一个：`setupComplete` / `serverContent{modelTurn.parts[].inlineData.data(b64 PCM16 24k), interrupted, turnComplete, generationComplete, inputTranscription{text}, outputTranscription{text}, interactionStatus}` / `toolCall{functionCalls[{id,name,args}]}` / `toolCallCancellation{ids}` / `goAway{timeLeft}` / `sessionResumptionUpdate{newHandle,resumable}`。
- 打断：服务端自己取消，发 `interrupted:true` + `turnComplete`，客户端只清播放；作废的工具调用由 `toolCallCancellation` 通知。无 cancel 事件。
- 工具结果 `{"toolResponse":{"functionResponses":[{id,name,response:{...}}]}}`，不用再触发；`behavior` BLOCKING/NON_BLOCKING（3.8-live 两者都行，显式写）。
- 会话：单连接约 10 分钟 → `goAway` → 新连接 setup 带 `sessionResumption.handle`（保存最新 `sessionResumptionUpdate.newHandle`，2 小时有效）；纯音频 15 分钟上限，开 `contextWindowCompression.slidingWindow:{}` 可突破；上下文 128k。
- 用量 `usageMetadata{promptTokenCount, responseTokenCount, promptTokensDetails[], responseTokensDetails[]（按 modality 拆）}`。价格：音频入 $3/M（≈$0.005/分钟）、出 $12/M（≈$0.018/分钟），文本入 $0.75 / 出 $4.50；免费层可用。
- 音色 30 个：Zephyr、Puck、Charon、Kore、Fenrir、Leda、Orus、Aoede、Callirrhoe、Autonoe、Enceladus、Iapetus、Umbriel、Algieba、Despina、Erinome、Algenib、Rasalgethi、Laomedeia、Achernar、Alnilam、Schedar、Gacrux、Pulcherrima、Achird、Zubenelgenubi、Vindemiatrix、Sadachbia、Sadaltager、Sulafat。中文 zh-Hans 在语言表，靠 systemInstruction 限定。
- 坑：中国大陆 / 香港不在支持地区（本机在美国，不影响）；原生音频只出 AUDIO，文字靠转写；Extended Thinking 判空闲看 `interactionStatus: IDLE`。

## xAI Grok Voice（2026-11-01 researcher，官方 docs.x.ai，没真跑）

- 模型 `grok-voice-3.0`（`grok-voice-latest` 指向它；无 think-fast-3.0）。端点 `wss://api.x.ai/v1/realtime?model=grok-voice-3.0[&reasoning.effort=none|high][&conversation_id=]`，头 `Authorization: Bearer <key>`。协议 = OpenAI Realtime GA 事件名，官方称换 base URL 可直接用 OpenAI SDK。
- 与 OpenAI 的差异：`session.update.session` 里 **无 `type:"realtime"`**；`voice` 和 `turn_detection` 在 session 顶层；`turn_detection` 只有 `{type:"server_vad"}` 或 null（无 threshold/semantic_vad/interrupt_response；多余字段是否报错未核实）+ `idle_timeout_ms`；无 `noise_reduction`、无 `tool_choice`（schema 里没有，发了是否报错未核实）；音频 `audio.input.format {type:"audio/pcm", rate:24000}`（8k–48k 可选，默认 24k）同 OpenAI；用户转写要 `audio.input.transcription.model:"grok-transcribe"` 才推 `conversation.item.input_audio_transcription.updated`（累计全文），`.completed` 是否仍发未核实；`response.output_text.delta` 和 `response.text.delta` 都会出现。
- 工具：`function` 扁平格式（指南）vs 嵌套（schema）矛盾，先扁平；结果 `function_call_output` + `response.create`，但官方要求**等本地音频播完再发 `response.create`**，否则下一轮和没播完的叠音。自带 `web_search` / `x_search` / `file_search` / `mcp`。
- 打断：VAD 模式自动 cancel；`conversation.item.truncate` 有（回执多带 `transcript`）；`output_audio_buffer.clear` 只在 WebRTC/SIP。`conversation.item.delete` 有。
- 会话 120 分钟（`error.type=max_duration`）；`resumption.enabled` + `?conversation_id=` 可续接 30 分钟内的历史。
- usage 只有 `input_tokens / output_tokens / total_tokens`，无音频文本拆分。**计费 $0.08/分钟（含模型），开 server_vad 时按连接时长计、静音也算**；每条客户端 `conversation.item.create`（文本）另 $0.004，`function_call_output` 免费。并发 10/team（档位 T0–T4 为 10/20/50/100/200）。区域 us-east-1。
- 音色：文档点到 `eve`（默认）、`ara`、`rex`，发布文 Ara/Eve/Leo，完整表要 `GET /v1/tts/voices`；不分大小写，每个音色会所有语言；`zh` 在语言表。
- 坑：默认 `reasoning.effort=high` 可能拖慢首包，实测 `none`；重放事件名 created/added 两说。
