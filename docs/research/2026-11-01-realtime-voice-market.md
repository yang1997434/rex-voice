# 实时语音全双工（speech-to-speech）市场盘点：质量与价格（2026-11-01）

两路 Parallel.ai 调研原文合并，海外一份、国内 + 评测一份。汇报页见 /_r/（链接在会话里），本文件是存档。价格、榜单都带日期，用前重核。

---

# 第一部分：海外

# 海外实时语音 speech-to-speech（S2S）模型与 API 盘点 — 2026-11-01

只读网络调研，工具：Parallel.ai（`deep-research` skill）。原始抓取文本在 `/tmp/rex-voice/raw/`。
口径：
- 「全双工」= 模型能边听边说、服务端 VAD、可被打断。「轮次制 + 可打断」= 服务端 VAD + 打断时截断输出，但模型本身不同时听说（OpenAI gpt-realtime 系、Gemini Live、Grok、Nova 都属此类；OpenAI 自己把 gpt-live-1 和 gpt-realtime 区分为 full-duplex vs turn-based）。
- 价格一律换算成 **每分钟美元（输入+输出各算满 1 分钟）**，换算依据写在每家小节。两家独立来源一致才记为事实，否则标「单来源」。
- 评测分以 Artificial Analysis（AA）S2S Index 为主，快照 2026-10-18（ridgebench 转录）和 2026-10-13（xAI 引用）。

---

## ① 总表

| 厂商 | 模型 | 发布 | 全双工 / 打断 | 工具 | 价格 $/分钟（输入 + 输出） | 评测（AA S2S Index / Big Bench Audio / TTFA） | 中文 | 备注 |
|---|---|---|---|---|---|---|---|---|
| OpenAI | gpt-live-1 | API 2026-09-10（ChatGPT 2026-07-08） | 真全双工（边听边说） | 委托给后端 Responses 模型 / 自带 agent | **$0.05/分钟 固定**（按秒计），后端模型另算；WebRTC 建会话预扣 15 秒 | 83.2（Astra medium）/ 90.1% / 1.34 s；配 Sol low：80.1 | 未查到官方语言表 | 知识截止 2025-07-31；WebRTC / WebSocket / SIP |
| OpenAI | gpt-realtime-2.1 | 2026-07-07 | 轮次制 + 服务端 VAD（server_vad / semantic_vad）可打断 | 函数调用、并行、可配 reasoning effort | 音频 $32/$64 per 1M → **$0.019 + $0.077 = $0.096**；缓存输入 $0.40 → $0.00024/分钟；文本 $4/$24 | 75.2（high）/ 96% / 1.21 s；minimal 70.7 / 87% / 0.97 s | 未查到官方语言表 | 知识截止 2024-09-30；128K 上下文、32K 输出 |
| OpenAI | gpt-realtime-2.1-mini | 2026-07-07 | 同上 | 同上（社区反馈 SIP 下工具调用不如旧 mini 稳） | 音频 $10/$20 → **$0.006 + $0.024 = $0.030**；缓存 $0.30；文本 $0.60/$2.40 | 54.4（minimal）/ 63% / 0.85 s；high TTFA 4.28 s | 同上 | |
| OpenAI | gpt-realtime-2 | 2026-05-07 | 同上 | 同上 | 与 2.1 相同 $0.096 | 73.6（high）/ 97% / 1.14 s | 同上 | 被 2.1 取代 |
| OpenAI | gpt-realtime（2025-08-28） | 2025-08-28 | 同上 | 函数调用 | 音频同 $32/$64，文本输出 $16 | 69.1 / 83% / 0.98 s | 同上 | 2027-01-20 停用（单来源）；换 2.1 |
| Google | gemini-3.8-live | 2026-09-15 | 轮次制 + 自动 VAD 可打断；支持异步工具（边聊边跑） | 异步函数调用、Google Search grounding | 音频 $3/$12 per 1M，Google 自给 **$0.005 + $0.018 = $0.023**；文本 $0.75/$4.50；免费层可用 | 76.0 / 92% / 1.18 s；Arena Elo 1088（最高） | 支持（99 语含 zh-Hans/zh-Hant，可中途切换） | Live API 仍标 Preview；WebSocket；16 kHz 进 / 24 kHz 出 |
| Google | gemini-3.8-live-extended-thinking | 2026-09-15 | 同上（边想边说） | 仅异步工具 | 与 3.8 Live 同价 $0.023（思考 token 按文本输出 $4.50 计） | 82.6（high）/ 97.7% / 1.35 s | 同上 | Google 宣称 AA Index #1（82.6），10-14 被 Grok 3.0 超过 |
| xAI | grok-voice-3.0 | 2026-10-14 | 轮次制 + server_vad 可打断 | function / web_search / x_search / file_search / MCP | **$0.08/分钟 固定**（$4.80/h）+ $0.004 文本输入（单位未写清） | **86.9（#1）/ 98.5% / 0.81 s**；Full Duplex Bench 97.0% | 支持（列表含 Chinese Simplified `zh`） | WebSocket（JSON 或二进制帧）、WebRTC、SIP、LiveKit；PCM 8–48 kHz / Opus / G.711 |
| xAI | grok-voice-2.0 | 2026-07-29 | 同上 | 同上 | 同 $0.08/分钟 | 81.3 / 97.2% / 0.70 s | 同上 | 已弃用，别名自动切 3.0 |
| Amazon | amazon.nova-2-5-sonic | 2026-10-05 | 轮次制 + 可配 turn detection、barge-in | 函数调用、异步工具 | speech $3/$12 per 1M，text $0.33/$2.75（三家第三方一致，AWS 价格页 JS 未抓到）；**每秒 token 数 AWS 未公布，按 25 tok/s 估 ≈ $0.0045 + $0.018**（估算） | AA 只有 Nova 2.0 Sonic（2026-03）：无 Index / 88% / 1.14 s | 不支持（7 语：en/fr/it/de/es/pt/hi） | Bedrock `InvokeModelWithBidirectionalStream`（HTTP/2 双向流）；默认 20 并发会话；256K 上下文 |
| Microsoft | Azure Voice Live（托管 gpt-realtime-2.1 等） | 定价 2025-07-01 起 | 随底层模型；平台加语义 VAD、打断检测 | 随模型；接 Foundry Agent | Pro 原生音频 $32/$64 → **$0.096**（同 OpenAI）；Standard（2.1-mini）$11/$22 → $0.0066+$0.0264 = **$0.033**；Lite（phi4-mm-realtime）$4/$12 → $0.003+$0.0144 = **$0.017**；级联档 Audio-Standard Pro $17/$31 | 同底层模型 | 级联路径 146 语输入 / 151 语输出；原生音频随 OpenAI | WebSocket + WebRTC；按 Azure 文档 AOAI ~10/~20 tok/s，Phi ~12.5/~20 tok/s 换算 |
| Hume | EVI 3 / EVI 4-mini | 停服通知 2026-10-02 | 轮次制，可配 `min_interruption_ms` | 工具 + 可换 LLM | 价格页未抓到 | AA 未收录 | 不支持（EVI 4-mini 11 语无中文；EVI 3 仅英文） | **2026-11-13 00:01 EST 停服，数据随后删除**，不要选 |
| ElevenLabs | Eleven Realtime v1 Mini / v1 Max | 2026-10-20 | 「端到端联合调优」，可打断；文档仍保留 LLM 级联回退 | 工具（Max 多步） | **$0.08/分钟 + LLM token 用量**（token 价未公开） | 厂商自报：Max 99% 语音推理（第三方转述 BBA 98.8%）/ TTFA 0.57 s；Mini 96% 对话动态 / 0.62 s；AA 榜未见 | 90+ 语（Realtime 中文未单独核实） | OpenAI Realtime 兼容 WebSocket `wss://api.elevenlabs.io/v1/convai/realtime`；SIP 走 ElevenAgents；ElevenAgents 默认档是级联（Scribe v2 Realtime + LLM + Eleven v3）$0.08/分钟 |
| 开源 | NVIDIA NemotronLabs VoiceChat 11B | 2026-08-03 | 真全双工，barge-in ~480 ms | 有（边说边调，≤5 工具） | 自部署，80 GB GPU | 无 Index / 38.8% / —；对话动态 77.8% | 仅英文 | OpenMDW-1.1（研究用）；2 分钟音频上下文；Nemotron 3 VoiceChat 12B 走 NIM 早期访问 |
| 开源 | NVIDIA PersonaPlex 7B | 2026-01-15 | 真全双工 | 无 | 自部署 | — / 19.1% / —；对话动态 91.0% | 仅英文 | Moshi 架构微调；NVIDIA Open Model License |
| 开源 | Kyutai Moshi（+ 2026-06-10 Interactivity 对齐版） | 2024-09 / 2026-06 | 真全双工 | 无 | 自部署 | — / 4.3% / —；对话动态 61% | 仅英文 | CC-BY-4.0；Unmute 是开源级联（STT+LLM+TTS） |
| 开源 | KRAFTON Raon-SpeechChat-9B | 2026（报告 04-08） | 真全双工 | 无 | 自部署（~25 GB） | — / 58% / 0.04 s；对话动态 86.2% | 仅英文 | CC-BY-NC-4.0，不可商用 |
| 开源/托管 | Ultravox（ultravox.ai） | 定价页 2026-10-09 | 可打断（级联式语音原生 LLM） | 有 | 托管 $0.05/分钟 + LLM $2/$15 per 1M；首 30 分钟免费 | AA 未收录 | 未核实 | 权重历史开源；2026 开源状态未核实 |
| 开源 | Sesame CSM-1B | 2025-03 | 否（对话式 TTS，不是 S2S） | 无 | Apache-2.0 | — | — | Sesame 2026 只有 app，无 API（单来源） |
| Mistral | Voxtral Realtime / Voxtral TTS | 2026-02-11 / 2026-03-23 | 否 | — | — | — | — | **只有 STT 和 TTS，没有 S2S** |
| Block | Dessa | 2026-10-23 | 真全双工（纯语音训练） | 无 | 本地跑 MacBook M4 Max；**暂无公开访问** | — | 未知 | 只开放给安全研究者 |
| Rime | Prism | 2026-10-28 | 单模型，~300 ms，并行工具 | 有 | 未公开 | 自报 τ-Voice 56.6（AA 同一 harness 自测） | 未核实 | 企业向，带可检查文本层 |
| Boson AI | Higgs Realtime | 2026-08-04 | 音频原生，可打断，~700 ms | 有 | $0.0023/分钟 入 + $0.014/分钟 出 = **$0.0163** | AA 收录：无 Index / 69% / 1.47 s | 自称 100+ 语（未核实中文） | Pipecat 集成 |
| Deepslate | Opal | — | — | — | 查不到厂商页 | 无 Index / 85% / **0.44 s**；对话动态 85.7%，τ-Voice 17.5% | — | 只在 AA 榜上见到 |
| AssemblyAI / Deepgram | Voice Agent API | — | 级联 | — | $0.075/分钟 全包（STT+LLM+TTS） | — | — | 级联，不是原生 S2S |

---

## ② 每家详情

### 1. OpenAI

**模型谱系**（来源：OpenAI 定价页 https://developers.openai.com/api/docs/pricing ，2026-10 版；模型页）
- `gpt-live-1`：真全双工，**$0.05/分钟，按秒计费不取整**，后端模型（Responses delegation）和工具另计。知识截止 2025-07-31。WebRTC（推荐）/ WebSocket / SIP；可加 sideband WebSocket 做服务端控制。`POST /v1/live/sessions` 建 WebRTC 会话先扣 15 秒，开始后抵扣。模型页：https://developers.openai.com/api/docs/models/gpt-live-1 ；成本指南：https://developers.openai.com/api/docs/guides/voice-latency-cost
  - 发布：ChatGPT 2026-07-08（rits.shanghai.nyu.edu 转述 + TechCrunch 链接）；API 2026-09-10（https://openai.com/index/introducing-gpt-live-1-in-the-api ）。
  - 厂商自报：Full Duplex Bench 比 gpt-realtime-2.1 高 30 个百分点；配 GPT-6 Astra medium 在 Tau3 排 #1；Speak 反馈打断减少 ~80%。12 个新 voice。
  - AA（ridgebench 2026-10-18 转录）：GPT-Live-1 (Astra, medium) Index 83.2、TTFA 1.34 s、Big Bench Audio 90%、Full Duplex Bench 94.9%、τ-Voice 74.5%、任务成功 87.4%、Arena Elo 1056；(Sol, low) 80.1 / 1.24 s / 89% / 97.3% / 59.3% / 90.9% / 1057。
  - 限制：不是轮次制但原生支持 turn detection；后端模型另选另付；语言表未公布。
- `gpt-realtime-2.1` / `gpt-realtime-2.1-mini`：版本号 `2026-07-07`（Microsoft Learn https://learn.microsoft.com/en-us/azure/foundry/openai/concepts/realtime-2 ；OpenAI 社区公告 https://community.openai.com/t/new-realtime-models-on-the-api-gpt-realtime-2-1-and-gpt-realtime-2-1-mini/1385896 截图日期 2026-07-07）。更新：字母数字识别、静音噪声处理、打断行为；p95 延迟降 ≥25%。知识截止 2024-09-30。128K 上下文 / 32K 输出，reasoning effort 可配。
  - 价格（1M token）：2.1 音频 $32 / 缓存 $0.40 / $64，文本 $4 / $0.40 / $24，图片 $5；2.1-mini 音频 $10 / $0.30 / $20，文本 $0.60 / $0.06 / $2.40。
  - **每分钟换算依据**：OpenAI 没有一手公布 token/秒；Azure Voice Live 文档（https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live ）写「Azure OpenAI 模型 输入音频 ~10 tokens/秒，输出 ~20 tokens/秒」；Forasoft 文章（2026-10-09，https://www.forasoft.com/blog/article/openai-realtime-api-pricing ）同样按 600 入 / 1200 出 token 每分钟算。两源一致。→ 2.1：入 $0.0192/分钟，出 $0.0768/分钟，缓存入 $0.00024/分钟；mini：入 $0.006，出 $0.024。
  - 真实账单（Forasoft 估）：缓存正常时旗舰 $0.06–0.11/分钟，mini $0.02–0.05/分钟；缓存失效长通话可到 $0.18–0.46/分钟。
  - 社区反馈：2.1-mini 在 SIP 流程里不触发函数工具，旧 `gpt-realtime-mini` 正常（同一帖子跟帖，单来源）。
  - AA：2.1 High 75.2 / 1.21 s / 96% / 95.7% / 45.7% / 91.5% / 939；2.1 Minimal 70.7 / 0.97 s；2.1 Mini Minimal 54.4 / 0.85 s / 63%；2.1 Mini High TTFA 4.28 s。
- `gpt-realtime-2`：2026-05-07（https://openai.com/index/advancing-voice-intelligence-with-new-models-in-the-api/ + deeplearning.ai The Batch 2026-05-15）。GPT-5 级推理、5 档 effort、并行工具、工具旁白、preamble。价格同 2.1。AA：High 73.6 / 1.14 s（The Batch 当时记 2.33 s，后被优化）；Minimal Full Duplex Bench 96.1%（曾第一）；Scale Audio MultiChallenge xhigh 48.45% 第一。
- `gpt-realtime`（2025-08-28 快照，已标 Deprecated）：首个 GA realtime 模型，WebRTC/WebSocket/SIP。音频同 $32/$64，文本输出 $16。Forasoft 称 OpenAI 2026-07-20 宣布 2027-01-20 停用 gpt-realtime 与 gpt-realtime-mini（单来源，未抓到 OpenAI deprecations 页）。AA：69.1 / 0.98 s / 83%。
- 音频格式：Realtime 会话示例 `audio/pcm` rate 24000，`turn_detection: semantic_vad` 或 `server_vad`（https://developers.openai.com/api/docs/guides/realtime-conversations ）。G.711 8 kHz 电话格式在本次抓取中未见明文（历史支持，未核实）。
- 速率限制：2.1 Build 档 200K TPM / Launch 4M / Grow 15M。
- 免费额度：无。
- 中文：本次没找到 OpenAI 官方语言清单页，标未核实。

### 2. Google Gemini Live API

来源：定价页 https://ai.google.dev/gemini-api/docs/pricing ；模型页 https://ai.google.dev/gemini-api/docs/models/gemini-3.8-live （Latest update: September 2026，Stable `gemini-3.8-live`）；能力页 https://ai.google.dev/gemini-api/docs/live-api/capabilities ；发布博文 https://blog.google/innovation-and-ai/models-and-research/gemini-models/gemini-3-8-live-gemini-3-8-live-extended-thinking/ （2026-09-15，更新 09-17）；企业版指南 https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/guides/gemini-3-8-live
- 当前模型：`gemini-3.8-live`（默认，低延迟）、`gemini-3.8-live-extended-thinking`（高推理，边想边说，只支持异步工具）、`gemini-3.1-flash-live-preview`（legacy，建议迁移）。Live API 整体仍标 **Preview**。
- 价格（三款同一价目）：输入 文本 $0.75、**音频 $3.00/1M 或 $0.005/分钟**、图片/视频 $1.00；输出（含思考 token）文本 $4.50、**音频 $12.00/1M 或 $0.018/分钟**。免费层：Free of charge（内容用于改进产品）。换算依据：Google 自己标的每分钟数；另在 Live Translate 脚注写「音频 25 tokens/秒」。→ 入 + 出 ≈ **$0.023/分钟**。
- 接入：双向 WebSocket；WebRTC 走 LiveKit / Pipecat / Agora / Fishjam 等合作方。输入 16 kHz 16-bit mono PCM（可重采样），输出 24 kHz PCM。
- 全双工：自动 VAD（可调灵敏度、可关、可混合客户端 VAD），打断时取消生成并发 `interrupted`；3.8 Live 的 barge-in 是「礼貌降级」（用户说话时等待），Extended Thinking 是「立即打断」。
- 工具：异步 NON_BLOCKING 函数调用（3.8 Live 默认，支持 SILENT/WHEN_IDLE/INTERRUPTED 调度），Extended Thinking 只有异步；Google Search grounding 每月 5,000 次免费后 $14/1k。
- 语言：99 种，含 Chinese (Simplified) `zh-Hans`、Chinese (Traditional) `zh-Hant`，对话中自动切换。
- 评测：Google 自报 ET 在 AA S2S Index 82.6（当时 #1）、τ-Voice 68.6%、Big Bench Audio 97.7%；AA 10-18 快照：ET (High) 82.6 / 1.35 s / 98% / 91.9% / 68.6% / 89.1% / Elo 993；3.8 Live 76.0 / 1.18 s / 92% / 96.1% / 30.1% / 93.2% / **Elo 1088（全榜最高）**；3.1 Flash Live High 72.0 / 2.99 s；Minimal 64.2 / 0.96 s / Elo 1100。
- 限制：Preview；Extended Thinking 不支持阻塞工具和函数调度；输出音频带 SynthID 水印。

### 3. xAI Grok Voice

来源：https://x.ai/news/grok-voice-3 （2026-10-14）；https://x.ai/news/grok-voice-think-fast-2 （2026-07-29）；定价 https://docs.x.ai/developers/pricing ；S2S 文档 https://docs.x.ai/developers/model-capabilities/audio/speech-to-speech ；release notes https://docs.x.ai/developers/release-notes ；blockchain.news 2026-10-14 转述。
- 模型：`grok-voice-3.0`（旗舰）、`grok-voice-latest`（别名→3.0）、`grok-voice-2.0`（已弃用）。
- 价格：**$0.08/分钟（$4.80/h）+ $0.004 / text input**（定价页原文，单位未写清，疑为每次文本输入）。3.0 与 2.0 同价（两源一致）。控制台有 "Try It Free"，额度未公布。
- 接入：`wss://api.x.ai/v1/realtime?model=…`，事件格式仿 OpenAI（session.update / input_audio_buffer.append / response.create）；音频 `audio/pcm` 8–48 kHz 可配、`audio/pcmu` `audio/pcma` 8 kHz、`audio/opus` 24 kHz；可选二进制帧传输；WebRTC（cookbook）、SIP（`CreatePhoneNumberV2`，DTMF）、LiveKit；浏览器用 ephemeral token。
- 全双工：`turn_detection.type: server_vad` 服务端 VAD；轮次制可打断（`output_audio_buffer.clear` 等事件 WebRTC/SIP）。
- 工具：`function`、`web_search`、`x_search`、`file_search`（Collections）、`mcp`（远程 MCP，xAI 托管执行）。
- 语言：20+ 种列表含 **Chinese (Simplified) `zh`**；`language_hint` 可中途改。
- 评测（xAI 引 AA 2026-10-13，ridgebench 10-18 一致）：**3.0 Index 86.9（#1）**、Big Bench Audio 98.5%、Arena Elo 1070、任务成功 94.8%、Full Duplex Bench 97.0%、τ-Voice 71.6%、TTFA 0.81 s；2.0：81.3 / 97.2% / 1011 / 94.6% / 95.1% / 0.70 s。EVA-Bench 准确率 74%（最高）。
- 限制：没有免费额度数字；text input 计费单位含糊。

### 4. Amazon Nova 2.5 Sonic

来源：AWS What's New 2026-10-05 https://aws.amazon.com/about-aws/whats-new/2026/10/amazon-nova-2.5-Sonic/ ；Bedrock 模型卡 https://docs.aws.amazon.com/bedrock/latest/userguide/model-card-amazon-nova-2-5-sonic.html ；语言页 https://docs.aws.amazon.com/nova/latest/nova2-userguide/sonic-language-support.html ；价格第三方：heyaiwiki、developersdigest（2026-10-20）、llmreference（2026-10-06）。
- 模型 ID `amazon.nova-2-5-sonic`，GA 2026-10-05，EOL 不早于 2027-10-05。256K 上下文、64K 输出。区域：us-east-1、us-west-2、eu-north-1、ap-northeast-1。
- 接入：只支持 `InvokeModelWithBidirectionalStream`（bedrock-runtime，HTTP/2 双向流，非 WebSocket/WebRTC）；默认 **20 并发会话/账号/区域**。Strands Bidi Agents GA。
- 价格：AWS 价格页是 JS 渲染没抓到；三家第三方一致：**speech 输入 $3.00 / 输出 $12.00 per 1M，text 输入 $0.33 / 输出 $2.75 per 1M**；AWS 公告说「与 Nova 2 Sonic 同价」。东京区更贵。**AWS 未公布音频 token/秒，每分钟价无法按厂商口径换算**；若按 25 tok/s 估 ≈ $0.0045 入 + $0.018 出（估算，标注）。
- 全双工：轮次制，「可控 turn-taking」+ barge-in 文档；异步工具调用（边跑边聊）；语音文本混输。
- 语言：**7 种**（英 US/UK/AU/IN、法、意、德、西 US、葡 BR、印地），**无中文**；多语 voice 可 code-switch。
- 采样率：本次没抓到明文（历史文档 16 kHz 入 / 24 kHz 出，未核实）。
- 评测：AA 只有 Nova 2.0 Sonic (Mar 2026)：Big Bench Audio 88%、TTFA 1.14 s、任务成功 57.1%、Elo 982，无 Index；2.5 尚未收录。
- 免费额度：无。

### 5. Microsoft Azure Voice Live API

来源：概览 https://learn.microsoft.com/en-us/azure/ai-services/speech-service/voice-live ；FAQ …/voice-live-faq ；价格页 https://azure.microsoft.com/en-us/pricing/details/speech/ （带斜杠版本能抓到数字）。
- 定位：托管语音代理 API，一个 WebSocket（也有 WebRTC 端点）接入，可选底层模型；定价「不选档，按模型落档」：
  - Pro：`gpt-realtime-2.1`（global/datazone/regional）、`gpt-realtime`、`gpt-realtime-1.5`、`gpt-4o`、`gpt-4.1`、`gpt-6.1-sol`、`gpt-5.x`、`azure-realtime`（自家 realtime，配 `azure-realtime-native` 音色）
  - Standard：`gpt-realtime-2.1-mini`、`gpt-realtime-mini`、`gpt-4o-mini`、`gpt-4.1-mini`、`gpt-5.6-luna`、`gpt-5-mini`
  - Lite：`gpt-4.1-nano`、`gpt-6-luna`、`gpt-5-nano`、`phi4-mm-realtime`（preview）
- 价格（per 1M token）：
  - Pro：文本 入 $4 / 缓存 $0.40 / 出 $16（gpt-realtime, 4o, 4.1, 5）或 $24（gpt-realtime-2 / 2.1）；**原生音频 S2S 入 $32 / 缓存 $0.40 / 出 $64**（与 OpenAI 同）；级联 Audio-Standard 入 $17 / 出 $31；Audio-Custom 入 $40 / 出 $55
  - Standard：文本 $0.66 / $0.33 / $2.64；**原生音频 $11 / $0.33 / $22**（比 OpenAI 的 $10/$20 贵 10%）；Audio-Standard $15 / $26
  - Lite：文本 $0.11 / $0.04 / $0.44；**原生音频 $4 / $0.04 / $12**；Audio-Standard $15 / $25
  - BYO：Audio-Standard $12.50 / $23
- 每分钟换算依据：Azure 文档「Azure OpenAI 模型 ~10 入 / ~20 出 tok/s；Phi ~12.5 入 / ~20 出 tok/s」→ Pro 原生 $0.0192 + $0.0768 = $0.096；Standard $0.0066 + $0.0264 = $0.033；Lite(Phi) $0.003 + $0.0144 = $0.017。
- 全双工：平台层「高级语义 VAD」「打断检测」「噪声抑制 / 回声消除」；是否真全双工取决于底层模型（gpt-realtime 系都是轮次制）。gpt-live-1 不在 Voice Live 支持列表。
- 语言：级联路径 146 语输入 / 151 语输出 / 600+ 音色（含中文，Azure Speech 一贯支持）；原生音频路径随 OpenAI。
- 配额：100,000 tokens/分钟/资源（可申请）。
- 限制：Voice Live 定价自 2025-07-01 生效；标准音色/定制音色训练托管另计。

### 6. Hume EVI

来源：https://dev.hume.ai/voice/docs/speech-to-speech-evi/overview ；changelog https://dev.hume.ai/voice/changelog ；developersdigest 2026-10-20。
- **停服核实**：changelog 2026-10-02 条目与 overview 顶部 Warning 原文一致：「Hume is sunsetting the TTS and EVI APIs. Access ends November 13, 2026 at 12:01 a.m. EST, and both APIs remain fully supported until then. Any data in your account is permanently deleted after November 13, 2026.」两源（Hume 两页 + developersdigest）一致。
- 版本：EVI 3（仅英文）、EVI 4-mini（英/日/韩/西/法/葡/意/德/俄/印地/阿，**无中文**）。WebSocket 接入；可配 `interruption.min_interruption_ms`（50–2000 ms）；可换补充 LLM（claude-opus-4-6、gpt-5.2 等）。
- 价格：价格页为 JS，未抓到；停服在即不再追。
- 评测：AA 未收录。Hume 自家 Real World VoiceEQ 榜只抓到 TTS 页签（Gemini 3.8 Flash TTS 0.92 居首，Eleven v4 Turbo 0.91），STS 页签未抓到。

### 7. ElevenLabs

来源：产品页 https://elevenlabs.io/realtime ；文档 https://elevenlabs.io/docs/eleven-agents/customization/realtime-models ；API 定价 https://elevenlabs.io/pricing/api ；博客 https://elevenlabs.io/blog/realtime （2026-10-20）；the-decoder 2026-10-20；llms.blog 2026-10-21；ElevenAgents 概览 https://elevenlabs.io/docs/eleven-agents/overview 。
- 两条产品线：
  - **ElevenAgents（Custom 模式）= 级联**：官方写明「4 个组件：微调 ASR + 你选的 LLM + 低延迟 TTS + 专有 turn-taking 模型」。AA Speech Agent Arena 把它作为「默认级联系统（Scribe v2 Realtime / GPT-4o Mini / Eleven v3）」收录：Elo 937、任务成功 90.5%。定价 $0.08/分钟（platform 费，LLM 另计；thunderphone 2026-08-19 核对「$0.08/分钟超额」）。
  - **Eleven Realtime v1 Mini / v1 Max（2026-10-20）**：官方称「a new class of Speech to Speech models」「Speech recognition, turn-taking, LLM, and voice are all tuned end-to-end」；但 Realtime 文档保留「Realtime 模型超时或出错时回退到备用 LLM（LLM cascading）」和 `cascade_timeout_seconds` 托管设置，定价是「**$0.08/分钟 + LLM usage**」（两源：定价页 + llms.blog）。**判断：联合调优的级联管线，不是单模型原生 S2S**（见 ④ 推断）。
- 自报指标：v1 Max「99% 语音推理、570 ms TTFA、带工具」；the-decoder 转述 Big Bench Audio 98.8% / 0.57 s；v1 Mini「96% 对话动态、620 ms TTFA」。AA 10-18 榜未见 Eleven Realtime。
- 接入：OpenAI Realtime 兼容 WebSocket（`wss://api.elevenlabs.io/v1/convai/realtime`，模型名 `eleven_realtime_v1_mini`），可直接用 `@openai/agents-realtime` SDK；WebRTC 在 FAQ 里列为问题（答案未抓到）；SIP trunk 走 ElevenAgents。90+ 语言。
- 限制：按工作区灰度；Max 在印度/新加坡数据驻留区不可用；Realtime 模式不支持自定义 LLM / Bedrock；Mini 与 Max 之间可转接，与 Custom 模式之间不可。

### 8. 开源 / 可自部署

- **Kyutai Moshi**（2024-09，7B，CC-BY-4.0，https://github.com/kyutai-labs/moshi ）：真全双工鼻祖；AA：Big Bench Audio 4.3%，对话动态 61%。2026-06-10 Kyutai 发「Interactivity alignment」（https://kyutai.org/blog/2026-06-10-interactivity/ ，arXiv 2606.11167，模型在 HF collection）：用 GRPO 对 Moshi 和 PersonaPlex 做序列级 RL，改善停顿处理、轮换、backchannel、打断响应。**Unmute**（https://kyutai.org/next/unmute/ ）是开源**级联**（Kyutai STT + 任意 LLM + Kyutai TTS），不是 S2S。
- **NVIDIA PersonaPlex 7B**（2026-01-15，https://research.nvidia.com/labs/adlr/personaplex ，权重 https://huggingface.co/nvidia/personaplex-7b-v1 ）：Moshi 架构 + 角色/音色提示；代码 MIT、权重 NVIDIA Open Model License；Fisher 英文语料训练；AA：对话动态 91.0%（开源最高）、Big Bench Audio 19.1%。
- **NVIDIA NemotronLabs VoiceChat 11B**（2026-08-03，https://huggingface.co/nvidia/NVIDIA-NemotronLabs-VoiceChat-11B ；rits.shanghai.nyu 2026-08-04 综述）：Fast Conformer 编码器 + Nemotron Nano v2 9B + TTS 解码器，16 kHz 入 / 22.05 kHz 出；**首个能边说边调工具的开源全双工**；barge-in ~480 ms；AA Big Bench Audio 38.8%（开源最高）、对话动态 77.8%；BFCL-v3 56.1%；限制：仅英文、2 分钟音频上下文、≤5 工具、不支持并行调用、工具执行中不能打断、需 80 GB GPU + vLLM、OpenMDW-1.1 研究许可。**Nemotron 3 VoiceChat 12B**（https://build.nvidia.com/nvidia/nemotron-voicechat/modelcard ）走 NIM 早期访问，目标 <300 ms，评估许可。
- **KRAFTON Raon-SpeechChat-9B**（https://huggingface.co/KRAFTON/Raon-SpeechChat-9B ，技术报告 2605.23912 2026-04-08）：全双工、英文、~25 GB、Transformers 直接加载、Docker 自托管 demo；**CC-BY-NC-4.0 不可商用**；AA：Big Bench Audio 58%、对话动态 86.2%、TTFA 0.04 s。
- **Sesame CSM-1B**（2025-03，Apache-2.0，https://github.com/SesameAILabs/csm ）：「Conversational Speech Generation Model」，本质是带上下文的 TTS，不是全双工 S2S；2026 年 Sesame 只有 app「Sesame Preview」无 API（orcarouter 2026-10-13，单来源）。
- **Ultravox**（https://www.ultravox.ai/pricing ，2026-10-09）：托管 **$0.05/分钟**（首 30 分钟免费），LLM token $2/$15 per 1M（缓存免费），SIP 0.5¢/分钟；历史上权重开源（HF fixie-ai/ultravox），2026 开源状态未核实；架构是语音原生 LLM + TTS，不是单模型 S2S。
- **Mistral Voxtral**：Voxtral（2025-07，24B/3B，Apache-2.0）语音理解；**Voxtral Realtime（2026-02-11，arXiv 2602.11298）= 流式 STT**；**Voxtral TTS（2026-03-23/26，4B，开源）= TTS**。**没有 S2S 模型**，只能自己级联。
- **Meta**：本次搜索（"Meta full-duplex speech model 2026 open weights"）没找到 2026 年 Meta 的全双工开源发布，查不到。
- **Block Dessa**（2026-10-23，https://block.xyz/dessa ；TechCrunch 2026-10-24；Dealroom；The Verge）：纯人类对话语音从零训练（无文本、无预训练 LLM）的真全双工模型，能 backchannel、打断；本地跑 MacBook Pro M4 Max；**目前无公开访问，只招安全研究者测试**，是否开源未定。
- 其它开源研究模型（AA 榜）：FLM-Audio（Cofe AI，BBA 16%）、Freeze-Omni（VITA，33%）、CharDuplex / MultiTalk（arXiv 2026-09，多方双语全双工研究）。
- 国内线（超出「海外」范围，仅记名）：StepFun Step-Audio R1.1 Realtime（BBA 98%，TTFA 1.53 s）、StepAudio 3 Realtime（BBA 100%，TTFA 8.83 s）；阿里 Qwen Audio 3.0/3.1 Realtime Plus/Flash（Index 69.5 / 67.7）、Qwen3.5 Omni Realtime。

### 9. 其它 2026 新冒出的原生 S2S 厂商（每家一行）

- **Rime Prism**（2026-10-28，https://www.rime.ai/resources/prism-first-speech-to-speech-for-enterprise ）：企业向单模型 S2S，~300 ms 响应，边说边并行调工具，带可检查/可改写的文本检查点；自报在 AA harness 上 τ-Voice 56.6、telecom 82.5（vs GPT-Live-1 45.6）；价格未公开。
- **Boson AI Higgs Realtime**（2026-08-04，https://www.boson.ai/blog/higgs-realtime ，定价 https://www.boson.ai/pricing ）：音频原生，10M+ 小时训练，~700 ms 端到端，自称 100+ 语；**$0.0023/分钟 入 + $0.014/分钟 出**；Pipecat `pipecat-boson`；AA：BBA 69%、对话动态 92.8%、τ-Voice 18.6%、TTFA 1.47 s。
- **Deepslate Opal**：AA 榜 TTFA **0.44 s**（最快）、BBA 85%、对话动态 85.7%、τ-Voice 17.5%；厂商页 / 价格查不到。
- **AssemblyAI Voice Agent API**：$4.50/h（$0.075/分钟）全包，级联（STT+LLM+TTS）；**Deepgram Voice Agent API** $0.075/分钟（Forasoft，单来源）——都不是原生 S2S。
- **OpenAI gpt-realtime-translate / gpt-realtime-whisper**（2026-05-07）：$0.034 / $0.017 每分钟，翻译/转写，不是对话 S2S。
- **Google gemini-3.5-live-translate-preview**：S2S 翻译，$3.50/$21 per 1M 音频，≈ $0.0368/分钟（25 tok/s）；非对话模型。

### 评测汇总（AA S2S Index，ridgebench 转录 2026-10-18）

| # | 模型 | Index | TTFA s | Big Bench Audio | Full Duplex Bench | τ-Voice | 任务成功 | Arena Elo |
|---|---|---|---|---|---|---|---|---|
| 1 | Grok Voice 3.0 | 86.9 | 0.81 | 99% | 97.0% | 71.6% | 94.8% | 1070 |
| 2 | GPT-Live-1 (Astra, medium) | 83.2 | 1.34 | 90% | 94.9% | 74.5% | 87.4% | 1056 |
| 3 | Gemini 3.8 Live Extended Thinking (High) | 82.6 | 1.35 | 98% | 91.9% | 68.6% | 89.1% | 993 |
| 4 | Grok Voice 2.0 | 81.3 | 0.70 | 97% | 95.1% | 56.5% | 94.6% | 1011 |
| 5 | GPT-Live-1 (Sol, low) | 80.1 | 1.24 | 89% | 97.3% | 59.3% | 90.9% | 1057 |
| 6 | Gemini 3.8 Live | 76.0 | 1.18 | 92% | 96.1% | 30.1% | 93.2% | 1088 |
| 7 | GPT-Realtime-2.1 High | 75.2 | 1.21 | 96% | 95.7% | 45.7% | 91.5% | 939 |
| 9 | GPT-Realtime-2 (High) | 73.6 | 1.14 | 97% | 95.3% | 39.8% | 89.8% | 938 |
| 11 | GPT-Realtime-2.1 Minimal | 70.7 | 0.97 | 87% | 92.7% | 38.0% | 89.4% | 941 |
| 14 | GPT Realtime (Aug '25) | 69.1 | 0.98 | 83% | 93.9% | 30.4% | 89.4% | 977 |
| 19 | GPT-Realtime-2.1 Mini Minimal | 54.4 | 0.85 | 63% | 91.8% | 22.5% | 76.7% | 844 |
| — | Nova 2.0 Sonic (Mar 2026) | — | 1.14 | 88% | — | — | 57.1% | 982 |
| — | Higgs Realtime (Boson) | — | 1.47 | 69% | 92.8% | 18.6% | — | — |
| — | Deepslate Opal | — | 0.44 | 85% | 85.7% | 17.5% | — | — |
| — | Raon SpeechChat (Krafton) | — | 0.04 | 58% | 86.2% | — | — | — |
| — | Nemotron VoiceChat (NVIDIA) | — | — | 27%* | 52.9%* | — | — | — |
| — | PersonaPlex (NVIDIA) | — | — | 19% | 91.0% | — | — | — |
| — | Moshi (Kyutai) | — | — | 4% | 61.0% | — | — | — |

\* ridgebench 10-18 快照对 Nemotron VoiceChat 记 27% / 52.9%，rits 8 月综述引 AA 为 38.8% / 77.8%，两处不一致（可能是不同版本或 AA 后来重测）。
AA 成本口径：按「每小时输入音频」算，Grok Voice 2.0 $4.80/h、GPT-Realtime-2.1 High $10.75/h、Gemini 3.1 Flash Live Minimal $1.50/h（AA 2026-08-24 文章）。
AA S2S Index 2026-06-23 发布（BBA + Full Duplex Bench + τ-Voice 等权），2026-08-24 加入 Speech Agent Arena（Elo + 任务成功率）后改为四项等权。

---

## ③ 分歧与未核实清单

1. **OpenAI 音频 token/秒换算**没有 OpenAI 一手文档，只有 Azure Voice Live 文档（~10 入 / ~20 出 tok/s）和 Forasoft 文章两源一致；OpenAI 定价页只写每 1M token。
2. **Nova 2.5 Sonic 价格**：AWS Bedrock 价格页 JS 渲染未抓到，依据 heyaiwiki / developersdigest / llmreference 三家第三方（一致）+ AWS 公告「与 Nova 2 Sonic 同价」；**音频 token/秒 AWS 未公布**，每分钟价是估算；采样率未核实。
3. **ElevenLabs Eleven Realtime 是否原生 S2S**：厂商叫「Speech to Speech models」「tuned end-to-end」，但文档保留 LLM 级联回退与 `cascade_timeout`，计费「$0.08/分钟 + LLM usage」；BBA 98.8% 是 the-decoder 转述厂商图表，AA 公开榜未见；token 价未公开。
4. **gpt-realtime / gpt-realtime-mini 2027-01-20 停用**：单来源（Forasoft 引 OpenAI deprecations 页），未抓到原页。
5. **gpt-realtime-2.1 发布日**：Microsoft Learn 版本号 `2026-07-07` + 社区公告截图 2026-07-07，没有 OpenAI 官方博文日期。
6. **Grok 「$0.004 / text input」**计费单位定价页没写清。免费额度数字未公布。
7. **Hume EVI 价格**、**Hume Real World VoiceEQ 的 STS 榜**：页面 JS，未抓到；停服在即不再追。
8. **OpenAI gpt-realtime / gpt-live-1 的官方语言清单（中文支持）**：没找到页面；G.711 8 kHz 电话格式本次未见明文。
9. **Nemotron VoiceChat 的 AA 分数**两处不一致（27%/52.9% vs 38.8%/77.8%）。
10. **Deepslate Opal** 厂商信息查不到；**Meta 2026 全双工开源**查不到；**Sesame 无 API** 单来源；**Ultravox 2026 开源状态**未核实；**Dessa 是否会开源**未定。
11. **Azure Voice Live Lite 档的 `azure-realtime` 模型**是什么、是否真全双工，文档没说。
12. Gemini Live API 整体仍标 Preview，Google 博文说「生产就绪」，口径不一。
13. AA 的成本「每小时输入音频」与本文「输入+输出各满 1 分钟」口径不同，不要直接对比。

---

## ④ 我的推断（非事实）

- **真全双工且可直接用的闭源 API 只有 OpenAI gpt-live-1 一家**；Grok、Gemini、gpt-realtime、Nova 都是「轮次制 + 服务端 VAD + 可打断」。要「边说边听、backchannel」体验，gpt-live-1 是唯一成品，代价是后端模型另算、语言表未公布、不在 Azure 上。
- **性价比排序（按本文每分钟口径）**：Gemini 3.8 Live ($0.023) < Boson Higgs ($0.016, 但质量低) < Azure Lite ($0.017, Phi) < gpt-realtime-2.1-mini ($0.030) ≈ Azure Standard ($0.033) < gpt-live-1 ($0.05 + 后端) < Grok 3.0 ($0.08 固定) < gpt-realtime-2.1 ($0.096)。固定按分钟计费（gpt-live-1、Grok）对长会话、静音多的遥控场景不划算；按 token 计费的模型静音段几乎免费，但要盯缓存命中。
- **质量**：AA 当前 #1 是 Grok Voice 3.0（86.9，TTFA 0.81 s，支持中文，工具含 MCP），#2 gpt-live-1，#3 Gemini 3.8 Live ET；若用户偏好（Arena Elo）优先，Gemini 3.8 Live（1088）最高但 τ-Voice 只有 30%，工具型任务弱。
- **对 rex-voice（手机遥控 Mac、要工具调用、要中文）**：候选是 Grok Voice 3.0（中文明确、MCP/函数、WebSocket 协议仿 OpenAI、迁移成本低）、Gemini 3.8 Live（最便宜、中文、异步工具、但 Preview + barge-in 是礼貌降级）、gpt-live-1（体验最好、要再接一个后端模型）。ElevenLabs 实质是级联，Nova 无中文，Hume 要停服，开源全部仅英文且质量差一截，都可排除。
- ElevenLabs 「Realtime」更像把 ASR/turn-taking/LLM/TTS 联合微调后打包，对外按 OpenAI 协议暴露；它能「回退到备用 LLM」这一点基本排除了单模型原生 S2S 的可能。
- Dessa、Prism、Opal 三家都是 2026 Q4 刚冒头，半年内不宜作为依赖。

---

# 第二部分：国内与评测榜单

# 国内实时语音（speech-to-speech）模型与 API 盘点 · 2026-11-01

调研方式：Parallel.ai 检索 + 原文抓取（脚本 `~/.claude/skills/deep-research/parallel_search.py`），所有价格/日期至少两个独立来源一致才写成事实，否则标「单来源」或「未核实」；抓不到的写「查不到」。阿里、字节两家的协议细节已在 `docs/research/2026-11-01-realtime-providers.md`，这里只补评测与开放状态。
汇率假设：1 USD = 7.1 CNY（只用于把 Artificial Analysis 的美元数换成人民币参考，不是官方价）。

## ① 国内总表

「¥/分钟」列的算法：上行 = 音频输入单价 × 每秒输入 token × 60；下行 = 音频输出单价 × 每秒输出 token × 60（模型实际说话的每一分钟）；「典型」= 上行 + 0.5 × 下行（假设模型说话占通话一半）。每秒 token 数取厂商计费页写的换算比，没写的标「待定」。

| 厂商 | 模型 | 发布 | 全双工/打断 | 工具 | 价格 ¥/分钟（上行 + 下行 → 典型） | 评测分 | 开放状态 | 备注 |
|---|---|---|---|---|---|---|---|---|
| 阿里百炼 | `qwen3.8-omni-flash-realtime` | 2026-09-21 | server_vad / semantic_vad / semantic_vad_ec，可打断（无 truncate） | Function Call + 远程 MCP；和联网搜索互斥 | 北京：文本入 1.5 / 音频入 6 / 文本出 4.5 / 音频出 12 元/百万 token；按 7 tok/s 入、12.5 tok/s 出：上行 0.0025 + 下行 0.009（+文本出 ≈0.002）→ 典型 ≈ 0.008 | AA 未收录 3.8；同系前代 Qwen3.5 Omni Plus Realtime：BBA 99%、Arena Elo 852、任务成功 62.0%、TTFA 2.64s；Flash：59% / 843 / 29.1% / 0.79s | 开放，北京 100 万 token 免费额度 | 音视频全模态，音频对话定位弱于 Qwen-Audio 线 |
| 阿里百炼 | `qwen-audio-3.1-realtime-plus` | 2026-09-20（北京）；3.0 Plus 2026-07-14 | 实时双工，server_vad / semantic VAD（smart_turn_v2），附和不打断 | Function Calling、联网搜索、声音复刻 | 北京：文本入 5 / 音频入 40 / 文本出 40 / 音频出 150 元/百万 token；每秒 token 换算**查不到**（若按 omni 的 7/12.5：上行 0.017 + 下行 0.11 → 典型 ≈ 0.07）；AA 实测 3.0 Plus 跑 BBA 子集 $4.42/小时 ≈ 0.52 元/分钟（见「分歧」） | 3.0 Plus（AA）：S2S Index 69.5（第 14）、BBA 99%、Full Duplex 98.4%、τ-Voice 54.6%、任务成功 77.8%、Elo 793、TTFA 1.54s；3.1 论文：半双工 τ-Voice 78.4→82.0%，FDB v1.5 背景人声误响应 73.0→13.0% | 开放，100 万 token 免费额度 | AA 榜上还是 3.0；3.1 沿用 3.0 协议、上下文 262K |
| 阿里百炼 | `qwen-audio-3.1-realtime-flash` | 2026-10-27 | 同上 | 同上 | 北京：1.5 / 6 / 4.5 / 12 元/百万 token（与 omni-flash 同价）→ 典型 ≈ 0.008（同样缺每秒 token） | 3.0 Flash（AA）：Index 67.7（第 16）、BBA 96%、FDB 96.9%、τ-Voice 35.9%、任务成功 81.7%、Elo 847、TTFA 1.55s | 开放，100 万 token 免费 | 便宜档 |
| 字节豆包语音 | 豆包实时语音 3.0（Seeduplex，`model:"1.2.6.1"`） | App 2026-04-09；API 2026-06-18 邀测 | 原生全双工（边听边说、动态判停、抗干扰），`response.cancel` 打断，关麦要发静音事件 | Function Calling（JSON Schema）、内置联网、唱歌 | 计费页只有一行「豆包实时语音大模型」：文本入 10 / 音频入 80 / 缓存 5 / 文本出 80 / 音频出 300 元/百万 token；入 6.25 tok/s、出 25 tok/s：上行 0.04 + 下行 0.45 → 典型 ≈ 0.27（是否就是 3.0 的价，未核实） | 无第三方评测。自评（Seed 博客 2026-04-09）：判停延迟 −250ms、抢话 −40%、打断响应延迟 −300ms、误回复/误打断率减半（对比上一代半双工） | 6-18 公告「开启邀测」；现在文档页直接写「控制台 API Key 管理取 key 跑 demo」，接入必读页要登录，是否已转开放**未核实** | QPM 60 / TPM 10 万；闲置 10 分钟断 |
| 字节豆包语音 | 豆包实时语音 2.0（O2.0 `1.2.1.0` / SC2.0 `2.2.0.0`） | 文档变更记录 2025-12-17；产品动态 2026-01 | 半双工（轮流说），只支持 server_vad，首字事件打断播报 | SC 版有内置联网 + 外部 RAG；Function Call 未见 | 同上价格表 | StepAudio 3 论文里的「Doubao 2.0 Lite」是文本/理解基线，不是这个语音模型（见「分歧」） | 开放（控制台购买） | O/SC 1.x 已停止独立迭代 |
| 字节 Seed | SeedRealtime（音视频全双工） | 2026-08-05 | 全双工 + 看画面主动开口 | — | 无 API、无价目 | 自评：对话节奏问题比级联减半 | 只在豆包 App，无 API（单来源 orcarouter + Seed 页未提 API） | 关注火山方舟是否上模型 ID |
| 智谱 | GLM-Realtime（`GLM-Realtime-Flash` / `-Air`） | 2025-01-08/16 发布；2026 无新版本记录 | 可实时打断（文档语），VAD 细节未查 | Function Calling | 按通话分钟：Flash 音频 0.18 元/分钟、Air 0.3 元/分钟（视频 1.2 / 2.1）→ 0.18–0.30（单来源：官方文档，价格页没列） | 无第三方评测；开源 GLM-4-Voice（2024-10）VoiceBench 56.48 | 开放，并发 V0 5 / V3 20 | 音频通话上下文 8K ≈ 20 轮，记忆 2 分钟，最大输出 1K |
| 阶跃星辰 | StepAudio 3 Realtime（`stepaudio-3-realtime-preview`） | 2026-09-15（arXiv v1 09-12） | 原生全双工（Seamless Duplex：停顿/附和/打断），server_vad 可关 | 内置 web_search、retrieval + 自定义 function | 预览版限时免费；正式价未公布。参考同线 `stepaudio-2.5-realtime` 10 / 缓存 2 / 70 元/百万 token、`step-audio-r1.5` 10/2/105（每秒 token 未公布，无法折算）；AA 实测 Step-Audio R1.1 (Realtime) $0.06/h 入 + $1.69/h 出 ≈ 0.007 + 0.20 → 典型 ≈ 0.11 元/分钟（模型 ID 对应关系未核实） | AA：BBA 99.7%（第 1）、Full Duplex 98.9%（第 1）、TTFA 8.83s（极慢，疑测的是 thinking 档）、无 Index；论文：τ-Voice 56.0%（vs Grok TF2.0 High 56.5、Qwen 3.0 Plus 54.6、GPT-Realtime-2.1 High 45.7）、MMSU 90.6、StepAudioChat 实时模式 70.4 | 开放（限免预览），个人 V0 并发 5 / RPM 100 / TPM 50 万 | 会话最长 30 分钟；`wss://api.stepfun.com/v1/realtime`，事件名跟 OpenAI beta |
| MiniMax | 无原生 S2S | — | — | — | — | — | — | 开放平台只有 TTS（speech-2.8）+ ASR，级联；在腾讯 TRTC 里作 LLM/TTS 供应商 |
| 科大讯飞 | 超拟人交互 API（「极速多模模拟人」）；SparkAudio 1.0 + Spark-Audio-Realtime | API 文档无日期；SparkAudio 1.0 2026-10-22 | `interact_mode: continuous`（文档称全双工），VAD `vgap` 可调，单连接 30 分钟 | 文档未见 | 产品页价格要登录，**查不到** | 无第三方评测 | 超拟人交互 API 可领取/购买授权包；Spark-Audio-Realtime 是否开放 API 查不到 | 请求体分 iat/nlp/tts 三段，推断内部是级联（见「推断」） |
| 腾讯 | 无原生 S2S 对话模型 | — | TRTC 对话式 AI：内置 STT + 智能打断 | 走第三方 LLM | TRTC 按分钟计费（未查具体） | — | 开放 | 级联：ASR 200–400ms + LLM 首 token 300–600ms + TTS 首包 400–600ms；混元 2026-10 开源的 Hy-SimulMT 是同传 S2S，不是对话 |
| 月之暗面 | 无 | — | — | — | — | Kimi-Audio（开源 2025-04）VoiceBench 76.91 | 开放平台只有 K3/K2 文本模型 | 查过 platform.kimi.com 首页、pricing、model capabilities |
| 百度智能云 | 端到端语音语言大模型 Pro（`audio-realtime-near/far`）/ Lite（`audio-mini-realtime-near/far`） | Pro 商用 2026-06-11（单来源）；Lite 商用 2026-07-29 | server_vad，`interrupt_response` 只能 true | 内置 38 个垂类助手 + 搜索增强（自动开，0.036 元/次）；开发者自定义 function 未见 | Pro：音频入 0.064 / 文本入 0.008 / 音频出 0.24 / 文本出 0.064 元/千 token；Lite：0.016 / 0.002 / 0.06 / 0.016；每秒 token **查不到** → ¥/分钟待定 | 无第三方评测；自称等待时长 1 秒左右 | 开放；免费 50 万 token（企业认证 100 万），10 并发 | `wss://aip.baidubce.com/ws/2.0/speech/v1/realtime`，事件名仿 OpenAI |
| 商汤 | 查不到 | — | — | — | — | — | — | 两轮检索无日日新实时语音 API 结果 |
| 小米 | 无 S2S | — | — | — | — | — | — | mimo.mi.com 只有 MiMo-V2.5-ASR（单来源） |

## ② 评测榜单（含海外）

### Artificial Analysis Speech to Speech Index（官网页 2026-11-01 抓取；Index = Big Bench Audio、τ-Voice、Arena Elo、任务成功率四项等权）

| # | 模型 | Index | BBA（推理） | Full Duplex | τ-Voice | Arena Elo | 任务成功 | TTFA s | $/h（跑 BBA 子集） |
|---|---|---|---|---|---|---|---|---|---|
| 1 | Grok Voice 3.0 (SpaceXAI) | 86.9 | 99% | 97.0% | 71.6% | 1064 | 94.8% | 0.81 | 4.80 |
| 2 | GPT-Live-1 (Astra, medium) | 83.2 | 90% | 94.9% | 74.5% | 1044 | 87.4% | 1.34 | 5.83 |
| 3 | Gemini 3.8 Live Extended Thinking (High) | 82.6 | 98% | 91.9% | 68.6% | 979 | 89.1% | 1.35 | 3.50 |
| 4 | Grok Voice 2.0 | 81.3 | 97% | 95.1% | 56.5% | 997 | 94.6% | 0.70 | 4.80 |
| 5 | GPT-Live-1 (Sol, low) | 80.1 | 89% | 97.3% | 59.3% | 1047 | 90.9% | 1.24 | 4.47 |
| 6 | Eleven Realtime v1 Max | 77.3 | 99% | 94.1% | 47.6% | 997 | 87.1% | 0.57 | 4.80 |
| 7 | GPT-Realtime-2.1 High | 75.2 | 96% | 95.7% | 45.7% | 937 | 91.5% | 1.21 | 10.75 |
| 8 | Gemini 3.8 Live | 74.5 | 92% | 96.1% | 30.1% | 1078 | 93.2% | 1.18 | 0.84 |
| 9 | Grok Voice 1.0 | 73.7 | 97% | 77.8% | 52.1% | 908 | 80.7% | 1.25 | 3.00 |
| 10 | GPT-Realtime-2 (High) | 73.6 | 97% | 95.3% | 39.8% | 926 | 89.8% | 1.14 | 4.14 |
| 11 | Gemini 3.1 Flash Live High | 72.0 | 97% | 74.3% | 37.7% | 1066 | 71.8% | 2.99 | 1.75 |
| 12 | GPT-Realtime-2.1 Minimal | 70.7 | 87% | 92.7% | 38.0% | 937 | 89.4% | 0.97 | 11.31 |
| 13 | GPT-Realtime-1.5 | 70.3 | 81% | 95.7% | 38.8% | 1000 | 85.1% | 0.81 | 11.44 |
| 14 | **Qwen Audio 3.0 Realtime Plus（阿里）** | 69.5 | 99% | 98.4% | 54.6% | 793 | 77.8% | 1.54 | 4.42 |
| 15 | GPT Realtime (Aug '25) | 69.1 | 83% | 93.9% | 30.4% | 969 | 89.4% | 0.98 | 11.08 |
| 16 | **Qwen Audio 3.0 Realtime Flash（阿里）** | 67.7 | 96% | 96.9% | 35.9% | 847 | 81.7% | 1.55 | 4.77 |
| 17–21 | Eleven Realtime v1 Mini 64.4；Gemini 3.1 Flash Live Minimal 64.2；GPT-Realtime-2 (Minimal) 63.3；GPT Realtime Mini 56.1；GPT-Realtime-2.1 Mini Minimal 54.4 | | | | | | | | |

无 Index 的国内条目：**StepAudio 3 Realtime** BBA 99.7%（43 个模型里第 1）、Full Duplex 98.9%（第 1）、TTFA 8.83s；**Step-Audio R1.1 (Realtime)** BBA 98%、TTFA 1.53s、$0.06/h 入 / $1.69/h 出；**Qwen3.5 Omni Plus Realtime** BBA 99%、Elo 852、任务成功 62.0%、TTFA 2.64s；**Qwen3.5 Omni Flash Realtime** 59% / 843 / 29.1% / 0.79s；Qwen3 Omni Flash 59% / FDB 72.7%；Qwen3 Omni Realtime 57%。豆包、智谱、百度、讯飞均不在 AA 榜。
Ridge（ridgebench.com，AA 2026-10-18 快照）与官网现值有小差：当时 Gemini 3.8 Live 76.0、Qwen 3.0 Plus Elo 792，且没有 Eleven Realtime；以官网现值为准。

### Full Duplex Bench（AA 子集，StepAudio 3 论文表 3，2026-09）

| | StepAudio 3 Realtime | GPT-realtime-2 (High) | Qwen Audio 3.0 Realtime Plus | Grok Voice Think Fast 2.0 High |
|---|---|---|---|---|
| Pause Handling | 98.9 | 99.3 | 98.0 | 98.0 |
| Turn Taking | 100.0 | 100.0 | 98.0 | 91.0 |
| User Interruption | 99.0 | 95.0 | 98.0 | 97.0 |
| Backchannel | 98.0 | 86.7 | 100.0 | 95.0 |
| Overall | 98.9 | 95.3 | 98.4 | 95.1 |

### τ-Voice

- AA 实现（StepAudio 3 论文表 9）：StepAudio 3 Realtime 56.0（Airline 60.0 / Retail 37.7 / Telecom 70.2）、Grok Voice Think Fast 2.0 High 56.5、Qwen Audio 3.0 Realtime Plus 54.6、GPT-Realtime-2.1 High 45.7。
- Sierra 官方 τ³-Voice 榜（taubench.com，2026-11-01 抓取，多语言 3 领域）：Ava Zero (Avartha) 86.0%、gpt-live-1 79.9%、grok-voice-think-fast-2.0 71.4%；无国内模型。
- Qwen-Audio-3.1 论文（arXiv 2609.25176，2026-09-21）：自家半双工语音转文本版 τ-Voice 任务成功 3.0→3.1 为 78.4%→82.0%（不可与 AA 数直接比）。

### Big Bench Audio（AA「Speech Reasoning」）
见上表 BBA 列。StepAudio 3 论文表：Big Bench Audio StepAudio 3 Realtime 98.1、Doubao 2.0 Lite 98.8、Gemini 3 Flash 99.4、Gemini 3.1 Pro 99.6；MMSU 90.6 / 80.0 / 77.0 / 83.6；MMAU 79.0 / 77.5 / 77.6 / 80.5。llm-stats 的 Big Bench Audio 页只收了 1 个模型（Nova 2 Sonic 0.870），没用。

### VoiceBench（matthewcym.github.io/VoiceBench，47 模型；benchlm 快照 2026-10-28）
榜首：Launa V1 90.60、BR-Voice-Reasoner 90.46、LFG-3 89.88、Nemotron 3 Nano Omni 30B 89.39、Ultravox-GLM-4P7 88.86、**Qwen3-Omni-30B-A3B-Thinking 88.80**、Whisper+GPT-4o 87.80。国内其它：Qwen3-Omni-30B-A3B-Instruct 85.50、Kimi-Audio 76.91、Baichuan-Omni-1.5 71.32、MiniCPM-o 71.23、Baichuan-Audio 69.27、GLM-4-Voice 56.48、Qwen2-Audio 55.80、Freeze-Omni 55.20、Step-Audio 50.84。S2S/全双工类只有 Nemotron 3 VoiceChat 58.1、Freeze-Omni 55.2、Moshi 29.5。注意：VoiceBench 基本是开源权重、文本输出评测，商用实时 API 不在内。

### AudioAgentBench（benchlm 2026-10-28 快照，人声音频）
gpt-live-1 90.3%、grok-voice-3.0 89.6%、gpt-realtime-2.1@high 80.4%、grok-voice-think-fast-2.0 80.2%、grok-voice-think-fast-1.0 79.9%、grok-realtime 78.3%、ultravox-v0.7 78.1%、gpt-realtime 77.5%、gemini-2.5-flash-native-audio 74.7%、gpt-realtime-2 73.1%。无国内模型。

### Hume Real World VoiceEQ（2026-07-14 发布，48,000 条 STS 人评）
STS 榜具体分数在 HF Space（HumeAI/rw-voice-eq），抓不到，**查不到数字**；博客只给结论「S2S 模型差异最大，不少系统仍是转写驱动、没用上语气信息」。TTS 榜（官网 2026-11-01）：gemini-3.8-flash 0.92 第 1；国内只有开源 Qwen3-TTS-12Hz-1.7B 0.49（第 31）。

### 国内中文评测
没找到 SpeechArena 之类公开的中文 S2S 榜单。能找到的只有各家自评：Seeduplex（Seed 博客）、豆包 1.0（2025-01-20，27 名测试者 800+ 通电话，满意度 4.36 vs GPT-4o 3.18）、Qwen-Audio-3.1 ASR 方言 CER 4.55%（KeSpeech/WSYue，36kr 转述）。

## ③ 每家详情（事实 + 来源）

### 1. 阿里 Qwen
- 系列关系（阿里官方「语音转语音」选型页 2026-10-28）：S2S 单模型三条线——Omni（`qwen3.8-omni-flash-realtime`，音视频实时）、Qwen-Audio Realtime（`qwen-audio-3.1-realtime-plus` 对标 GPT-Live 1 / Gemini 3.8 Live；`-flash` 对标 GPT-Realtime-2.1 Mini）、Livetranslate（同传）。Qwen-Audio Realtime 是「语音助手 / 客服」主推线，支持语义 VAD smart_turn_v2、Function Calling；Omni 线是全模态。两线协议都是 Realtime API（WebSocket / WebRTC / AOQ）。来源：https://www.alibabacloud.com/help/en/model-studio/s2s-model 、https://help.aliyun.com/zh/model-studio/omni
- 上线日期（阿里「模型上下架与更新」）：qwen3-omni-flash-realtime 2025-09-17；qwen3.5-omni-flash-realtime 2026-03-30；qwen3.8-omni-flash-realtime 2026-09-21；qwen-audio-3.0-realtime-plus/flash 2026-07-14；qwen-audio-3.1-realtime-plus 2026-09-20；qwen-audio-3.1-realtime-flash 2026-10-27。来源：https://help.aliyun.com/zh/model-studio/newly-released-models ；千问 AI 平台 changelog 同日期 https://platform.qianwenai.com/docs/changelog/models
- 价格（北京，元/百万 token，文本入/音频入/文本出/音频出）：omni-flash-realtime 1.5/6/4.5/12，免费 100 万 token；qwen-audio-3.1-realtime-plus 5/40/40/150，免费 100 万；3.1-flash 1.5/6/4.5/12，免费 100 万；3.0 系列同价无免费。模型卡注：「仅输出文本按文本出价；输出文本+音频只收音频费」；限流 RPM 60、TPM 10 万；上下文 262,144。来源：https://help.aliyun.com/zh/model-studio/model-pricing 、https://help.aliyun.com/zh/model-studio/qwen-audio-3-1-realtime-plus ；36kr 2026-09-24 转述「Plus 按档位 输入 5～40、输出 40～150」一致 https://www.36kr.com/p/3996196939616387
- 降价：2026-09-23 发布会称 Realtime 降幅约 85%（36kr，单来源）。
- 评测：AA 官网（见②）；Qwen-Audio-3.1 论文 arXiv 2609.25176（2026-09-21）摘要：τ-Voice 半双工版 78.4→82.0%，FDB v1.5 背景人声误响应 73.0→13.0%。
- 已知限制（沿旧调研）：无 conversation.item.delete/truncate、无 tool_choice、不能塞 message 类 item；工具与 enable_search 互斥；音频保留 50 轮 / 300 秒（Qwen-Audio 线）。

### 2. 字节豆包 / Seed
- 版本线：1.0（App 2025-01-20，Seed 博客；API 产品动态 2025-05）→ O/SC 2.0（文档变更记录「25.12.17 O/SC 升级到 2.0」；产品动态列在 2026-01）→ 3.0 Seeduplex（App 2026-04-09 全量；API 2026-06-18 上线「开启邀测」）。来源：https://seed.bytedance.com/zh/seeduplex 、https://docs.volcengine.com/docs/6561 （产品动态）、https://www.volcengine.com/docs/6561/1594356 （旧版文档）、https://cloud.tencent.cn/developer/news/4112309 （6-18 新闻）
- 版本定位（模型列表页 2026-09-29）：S2S-全双工版（3.0）、S2S-Omni 2.0、S2S-Strong Character 2.0；O/SC 1.x 已停止独立迭代。https://docs.volcengine.com/docs/doubao-voice/model-list?lang=zh
- 3.0 文档（2026-09-04 更新）：`session.create` 带 `model`、`instructions`（与内部 SP 合计 ≤12K token）、`tools`（JSON Schema）、内置联网、唱歌；`response.cancel` 打断、`conversation.item.create role:tool` 回传；取 key 走控制台「API Key 管理」。https://docs.volcengine.com/docs/6561/2549778?lang=zh 。「接入必读」要登录，抓不到。
- 价格：计费说明页「豆包实时语音大模型」一项：文本入 10、音频入 80、文本/音频缓存 5、文本出 80、音频出 300 元/百万 token；音频入 6.25 tok/s、出 25 tok/s；QPM 60、TPM 10 万。页面没分版本，3.0 是否同价未核实。https://docs.volcengine.com/docs/doubao-voice/billing-instructions?lang=zh （同内容另一 URL https://docs.volcengine.com/docs/DoubaoVoice/Billinginstructions-21?lang=zh ）
- 评测：Seeduplex 博客自评——判停延迟 −250ms、复杂场景抢话 −40%、打断响应延迟 −300ms、误回复/误打断率减半；orcarouter 转述同一组数，再加「大型 A/B 满意度 +8.34」（单来源）。无第三方。
- SeedRealtime（2026-08-05）：音视频全双工，只在豆包 App，无 API / 模型 ID / 价目（Seed 页 + orcarouter 2026-08-06）。https://seed.bytedance.com/zh/SeedRealtime 、https://www.orcarouter.ai/zh-CN/blog/seedrealtime-launch

### 3. 智谱 GLM-Realtime
- 发布：2025-01-08（scriptbyai 时间线）/ 2025-01-16 发布稿（aibase）；GLM-4-Voice 开源 2024-10-25。2026 年没查到新版本。
- 文档（docs.bigmodel.cn）：音视频通话模型，记忆 2 分钟，音频通话上下文 8K ≈ 20 轮，最大输出 1K；可实时打断；Function Calling；并发 V0 5 / V1 10 / V2 15 / V3 20；`wss://open.bigmodel.cn/api/paas/v4/realtime`；SDK github MetaGLM/glm-realtime-sdk。价格：GLM-Realtime-Flash 音频 0.18 元/分钟、视频 1.2；GLM-Realtime-Air 音频 0.3、视频 2.1（单来源：文档页；open.bigmodel.cn/pricing 页没有列 Realtime）。https://docs.bigmodel.cn/cn/guide/models/sound-and-video/glm-realtime
- 评测：无第三方；GLM-4-Voice VoiceBench 56.48。

### 4. 阶跃星辰 StepAudio 3 Realtime
- 发布：2026-09-15（IT之家、新浪同日）；arXiv 2609.14005 v1 2026-09-12、v3 2026-10-12。
- 平台文档：`stepaudio-3-realtime-preview` 限免，到期下线换正式付费版；Realtime API 内置 ASR/VAD/上下文/web_search/retrieval + 自定义 function；全双工（同时说话、真打断才停、附和不停）、边想边说、Voice Agent 异步工具；会话最长 30 分钟；VAD 可关（`turn_detection: null`）；端点 `wss://api.stepfun.com/v1/realtime`。https://platform.stepfun.com/docs/zh/guides/models/stepaudio-3-realtime 、https://platform.stepfun.com/docs/zh/guides/developer/realtime
- 价格页（元/百万 token，入/缓存/出）：stepaudio-3-realtime-preview 限时免费；stepaudio-2.5-realtime 10/2/70；step-audio-r1.5 10/2/105；step-1o-audio 25/5/60。每秒 token 换算没写。https://platform.stepfun.com/docs/zh/guides/pricing/details
- 限流：个人 V0（充值 <100 元）并发 5 / RPM 100 / TPM 50 万，V4（≥1 万元）130 / 2600 / 1300 万。https://platform.stepfun.com/docs/zh/guides/pricing/rate-limits
- 评测（论文 + AA）：AA Full-Duplex 98.9 第 1；BBA 99.7% 第 1；τ-Voice 56.0；MMSU 90.6；StepAudioChat 推理模式 73.0、实时模式 70.4；论文承认多轮约束遵循（实时模式 Instruction Following 54.1）和零售工具任务（37.7）是短板。AA 测得 TTFA 8.83s。

### 5. MiniMax
- 开放平台接口总览：语言模型 M3.1-Flash/M3；语音只有合成（speech-2.8-hd/turbo 等）、复刻、设计，HTTP/WebSocket 流式 TTS；ASR 按小时计。没有 speech-to-speech 或 realtime 对话接口。https://platform.minimaxi.com/docs/api-reference/api-overview 、https://platform.minimax.io/docs/api-reference/api-overview

### 6. 科大讯飞
- 「超拟人交互 API」：WebSocket，`interact_mode: continuous`（文档称默认全双工），单连接 ≤30 分钟，10 秒不发数据断开，16k PCM 单帧 ≤40ms，`vgap` 静音断句 400–10000ms，`prompt` 设回复风格；文档自称「极速多模模拟人是端到端语音交互方案」。授权在产品页领取/购买，价格页要登录。https://www.xfyun.cn/doc/spark/sparkos_interactive.html
- SparkAudio 1.0（2026-10-22 发布者节，InfoQ 2026-10-23、新浪 2026-10-28）：语音基座（文本+语音入、文本出），其上有 Spark-ASR-2.0、Spark-LiveTranslation、Spark-Audio-Realtime 三个任务模型，采用「S-S2S 动态分段端到端建模」压延迟；是否对外开放 API、价格均查不到。https://www.infoq.cn/article/Md0kdMxGI6flIbZXH1yg 、https://k.sina.com.cn/article_5952915705_162d248f906703ruk6.html

### 7. 腾讯
- 腾讯云 TRTC「实现 AI 对话」：内置 STT + 智能打断，LLM（OpenAI 协议任意供应商 / MiniMax）和 TTS（腾讯云 / MiniMax）由客户配置，是级联；宣称对话延迟 <1000ms，分段 ASR 200–400ms、LLM 首 token 300–600ms、TTS 首包 400–600ms；单 SDKAppId 100 路。https://cloud.tencent.com/document/product/647/108901
- 混元：2026-10-22/26 开源 Hy-SimulMT-30B-A3B 中英同传（语音进、译文语音出），Apache 2.0；Hy-ASR-3.0-preview。没有端到端语音对话模型 API。https://github.com/Tencent-Hunyuan/Hy-SimulMT 、https://www.huxiu.com/ainews/15870.html

### 8. 月之暗面 Kimi
- Kimi-Audio 开源（2025-04-25，arXiv 2504.18425），VoiceBench 76.91。开放平台（platform.kimi.com / platform.moonshot.cn）首页、价格页、模型能力 FAQ 只有 K3/K3.1/K2 文本模型，无语音 API。

### 9. 百度
- 端到端语音语言大模型 Pro / Lite：Cross-Attention 跨模态，`wss://aip.baidubce.com/ws/2.0/speech/v1/realtime?model=audio-realtime-far` 等 4 个模型名（Pro/Lite × 近场/远场），事件名仿 OpenAI（`turn_detection.type: server_vad`、`max_response_output_tokens`），`interrupt_response` 仅支持 true，API Key 或 access_token 鉴权。https://ai.baidu.com/ai-doc/SPEECH/nmcytnwei （2026-09-04）
- 价格（元/千 token，后付费）：Pro 音频入 0.064 / 文本入 0.008 / 音频出 0.24 / 文本出 0.064；Lite 0.016 / 0.002 / 0.06 / 0.016；搜索增强自动开启 0.036 元/次（预付 1 万次 352 元）；Pro 预付 200 万 token 350 元、Lite 70 元；10 并发；免费 50 万 token（个人认证 3 并发，企业 100 万 / 5 并发）。https://ai.baidu.com/ai-doc/SPEECH/Tmq8y6vdh 、https://ai.baidu.com/tech/speech/chatbot 。音频每秒 token 换算查不到。
- 商用：Lite 2026-07-29 公告（ai.baidu.com/support/news id=3276）；Pro 2026-06-11（百度文档 PDF 摘要，单来源）。

### 10. 其它
- 商汤日日新：三轮检索（中文「商汤 日日新 实时语音 端到端」、「SenseNova Omni 实时语音 对话 API」）无结果，查不到。
- 小米：mimo.mi.com 只有 MiMo-V2.5-ASR（单来源）。
- 百川：VoiceBench 上有 Baichuan-Omni-1.5 / Baichuan-Audio 开源模型，无商用 realtime API 结果。

## ④ 分歧与未核实

1. **Qwen-Audio Realtime 的每秒 token 换算查不到**：omni 线文档写 入 7 / 出 12.5 tok/s，Qwen-Audio 模型卡和指南都没写；AA 实测 3.0 Plus 跑 BBA 子集 $4.42/小时、3.0 Flash $4.77/小时，而两者牌价差 8 倍、AA 成本却几乎相同，说明 AA 的成本数或我的换算假设至少有一个不对。表里 0.07 元/分钟是按 omni 换算的推断，接入后按账单核。
2. **豆包 3.0 价格**：计费页只有一行「豆包实时语音大模型」，没按 1.x/2.0/3.0 分；3.0 是否同价、是否仍邀测，「接入必读」页要登录看不到。只有 6-18 新闻说「邀测」。
3. **豆包 2.0 时间**：文档变更记录写 2025-12-17 升级到 2.0，产品动态把「豆包实时语音模型 2.0」列在 2026 年 1 月；可能是 API 发布与产品公告的时间差。
4. **StepAudio 3 论文里的「Doubao 2.0 Lite」**：用作音频理解 / 对话推理基线，表格里 Big Bench Audio 98.8、StepAudioChat 70.5，更像 Seed 2.0 Lite 文本/全模态模型，不是豆包实时语音 2.0；别拿它当豆包语音的评测分。
5. **StepAudio 3 Realtime 正式价**：预览限免，付费价未公布；AA 的 Step-Audio R1.1 (Realtime) $0.06/h 入、$1.69/h 出对应哪个平台模型 ID 未核实。AA 测得 TTFA 8.83s，远慢于论文「实时」定位，疑测的是深度思考档。
6. **GLM-Realtime 价格**：只在文档页出现（0.18 / 0.3 元/分钟），价格总页没有，单来源；且没说明按会话分钟还是按音频分钟。
7. **百度**：Pro 商用日期 2026-06-11 单来源；音频 token 换算没写，¥/分钟折不出来；开发者自定义 function calling 未见文档。
8. **讯飞**：超拟人交互 API 价格页要登录；「全双工」只是参数名 `continuous`，请求体分 iat/nlp/tts，是不是原生 S2S 没有证据；Spark-Audio-Realtime 是否开放 API 查不到。
9. **Hume VoiceEQ STS 榜**：具体分数在 HF Space，抓不到。
10. **中文 S2S 公开榜**：没找到 SpeechArena 之类，国内厂商只有自评。
11. **Ridge 快照（10-18）与 AA 官网（11-01）有小差**，以官网为准；AA 榜上阿里仍是 3.0，3.1 还没上。
12. **SeedRealtime 无 API**：orcarouter 单来源，Seed 官方页只说 App 全量上线，未明说无 API。

## ⑤ 我的推断（不是事实）

- 国内真正「原生全双工 + 工具调用 + 对外开放 API」的只有三家：阿里 Qwen-Audio 3.1 Realtime（最成熟、有第三方分、便宜）、阶跃 StepAudio 3 Realtime（第三方双工分最高、现在免费、但正式价和延迟表现不确定）、豆包 3.0 Seeduplex（主观体验口碑好、贵、开放状态要问）。智谱 GLM-Realtime 是 2025 年初的模型，没有迭代、没有第三方分，按分钟计价便宜但不建议当主力。
- 给 rex-voice 这种「读 Rex 窗格、发指令」的工具型场景，τ-Voice / 任务成功率比音色更重要：Qwen 3.0 Plus 54.6% 与 StepAudio 3 56.0% 都在第一梯队（Grok TF2.0 56.5），GPT-Realtime-2.1 High 只有 45.7%；国内两家在这一项不吃亏。
- 价格量级：Qwen flash 档 ≈ 1 分钱/分钟、Qwen plus 档 ≈ 几分到五毛（取决于换算）、豆包 ≈ 3 毛/分钟、GLM 0.18–0.3、OpenAI GPT-Realtime-2 按 AA $4.14/h ≈ 0.49 元/分钟。即使按最坏假设，Qwen plus 也不比 OpenAI 贵。
- 讯飞「超拟人交互」请求体分识别/语义/合成三段，更像级联封装；腾讯、MiniMax 明确是级联。要原生 S2S 就别考虑这三家。
- 百度端到端模型文档像是 OpenAI 事件名的子集，功能上没有开发者自定义工具，不适合本项目。
- 接入优先级建议：先 Qwen-Audio-3.1-realtime-plus（协议已调研、有免费额度、能核实 token 换算），再试 StepAudio 3 Realtime（限免期零成本、事件名跟 OpenAI beta 接近，`internal/voice` 的 OpenAI 适配大概率能复用大半），豆包 3.0 等用户在火山控制台确认开通后再接。
