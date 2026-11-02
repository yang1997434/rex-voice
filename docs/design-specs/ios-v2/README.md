# Rex Voice iOS 界面：第二轮设计稿 D / E（2026-11-01）

第一轮三版（`../ios-liquid-glass/`）用户全否：「内容单调、质感不够，参考 Hark 和 Rex 的 UI；要多组可选的 Claude 形象；音色也要能选」。这轮两版给用户挑，定稿后再出实现 brief。

- 代码：`ios/RexVoice/Sources/Design/` 里 `V2Kit.swift`（形象、材质、按钮、音色表，两版共用）、`V2Settings.swift`（设置页，两版共用内容、各自外壳）、`V2Pair.swift`（配对页）、`RoomDesign.swift`（D）、`CardDesign.swift`（E）。和第一轮一样编进每个包，只有 DEBUG 包带 `REXVOICE_DESIGN=d|e` 才显示，发版包仍是旧 `ContentView`。
- 形象图：`Assets.xcassets/Personas/<style>/<state>`（像素 pixel / Q 版 chibi / 水彩 watercolor / 霓虹 neon × idle / listening / thinking / speaking / offline），原图从 `docs/design-specs/personas/` 拷来，没缩。
- 状态 → 表情：未配对 / 连接中 / 待机 → idle；接通中 / 在听 → listening；在想 → thinking；在说 → speaking；Mac 不在线 → offline（再降饱和 + 变淡）。
- 预览环境变量（DEBUG）：沿用第一轮 `REXVOICE_PREVIEW`（假状态 + 假转录，见 `CallController.applyPreview`）；新加 `REXVOICE_PERSONA=pixel|chibi|watercolor|neon`（选哪组形象，写进 `UserDefaults` 的 `v2.persona`）、`REXVOICE_SCROLL=voices|custom|bottom`（设置页落到音色 / 音色表末尾 / 页底）、`REXVOICE_PROVIDER=openai|qwen|gemini|grok`（设置页先选哪家）。
- 截图：模拟器 iPhone 17（iOS 27.2），状态栏 9:41，存 603×1311（原图一半）。重拍脚本是 scratchpad 的 `design-shots-v2.sh`（`simctl launch` 带 `SIMCTL_CHILD_REXVOICE_*`，深浅色用 `simctl ui appearance`）。

## 两版共有的

- **形象是主角**：占屏约 46–53%。换状态 300 ms 交叉淡入 + 0.97→1 轻微缩放（锚在底边）；在说时以 2.4 秒周期 ±1.8% 呼吸缩放；在听时形象背后有三圈光环 + 径向光晕，随音量涨落。开「减少动态效果」时呼吸和光环都停，只剩淡入淡出。
- **转录只留最近几句**（D 三句、E 两句），在半透明卡上。
- **底部一颗主按钮**：未配对「配对 Mac」、待机「开始通话」（白 / 浅玻璃胶囊 + 绿色电话图标）、通话中「挂断」（红色渐变）、连接中 / 不在线「等 Mac 上线」（变淡、不可点）。按下缩到 0.96（150 ms ease-out），禁用时不缩。
- **设置页是一组卡片**，不用系统 Form。每张卡头部是 Hark 那种「小图标块 + 标题 · 说明」：
  - 形象：横向轮播，每组一张大图 + 名字 + 一句说明，选中的有描边 + 右上角勾 + 每 2.8 秒一次 2.5% 的呼吸；打开时滚到当前选中的那组。
  - 声音：四个供应商胶囊（2×2，每个带品牌色字母块：OpenAI 绿 O、通义紫 Q、Gemini 蓝紫渐变 G、Grok 黑 X），下面模型单选列表、音色列表（名字 + 一句气质描述 + 灰掉的试听键位）。Gemini 的音色列表右上标「列表待核」；Grok 用真 key 拿到的 28 种里列前 10 种（女声 ara 默认 / eve / aurora / luna / iris / celeste，男声 rex / leo / orion / atlas），末尾一行「自定义音色 · 全部 28 种，或填音色名」。
  - API key：已保存显示 `••••1234` + 「更换」「删除」两颗胶囊；点了出 SecureField +「保存到钥匙串」。
  - 麦克风：大号数字门限值、实时电平条（竖线是门限位置，过线才亮）、滑杆、「恢复默认」。
  - 配对：Mac / 配对时间 / 中继 / 加密 四格 +「重新配对」。
  - 关于：版本 + 素材署名。
- **配对页**：取景框在上 1/3，形象（在说那张，手抬着）站在左下角，旁边一张卡写「扫 Mac 上的配对二维码 / 在 Mac 终端运行 rex-voiced pair…」。

## D「房间」

取向：形象站在一个有景深的房间里，光的颜色就是状态。整屏是深色渐变，形象背后一大团柔光，地上一圈光斑，形象底边和左右两边淡进光里；状态字用衬线体放在形象下方，转录像字幕一样居中浮在下面（新的一句最亮，旧的依次变淡）；设置是从底部拉起的大面板（带拖动条，可拉到半屏），面板里是磨砂卡片。

材质参数：
- 背景渐变：上→下两色，每个状态一套（深色：待机 `#0D1322→#2B2A44`、在听 `#071A1D→#113A3A`、在想 `#100C26→#2C2152`、在说 `#1C1209→#46290F`、不在线 `#0F1013→#1E2026`；浅色是 Hark 那种天色：待机 `#B9CDE6→#F4DCCF` 等）。
- 光斑：三个模糊圆。主光 = 状态光色 55% 不透明、直径 1.1 倍屏宽、`blur 90`，放在形象中心（36% 高处）；左下补光 25%、`blur 110`；右上点缀 18%、`blur 100`。地面光斑是 260×46 椭圆、光色 45%（浅色 55%）、`blur 24`。
- 噪点：一张 128² 随机灰度图（启动时生成一次），平铺、`overlay` 混合、8% 不透明。
- 形象：竖向遮罩 0–74% 实、到底边全透；横向遮罩两边各 14% 渐隐；外加光色 35% 的 `shadow radius 30` 当逆光。
- 字幕卡和设置卡：`.ultraThinMaterial` + 白 4%（浅色 35%）+ 1pt 白 10%（浅色 70%）描边，圆角 24–26。
- 状态切换时背景、光、表情一起 0.6 秒平滑过渡。

风险：浅色模式下光斑会把天色洗得发灰，对比度比深色弱（看截图）；光斑用 `blur` 叠了三层，老机型上首帧可能慢（没真机测）；字幕区固定三句，长句会挤压形象（形象用屏高比例定高，暂时没挤到）。

## E「卡片」

取向：全屏近黑（浅色是 95% 灰白），形象装在一张「有厚度」的大卡片里，卡片里的光随状态变色；卡片左上角是状态小胶囊、右上角是通话计时（深色是烟黑底白字，浅色是奶白底深字）；卡片下面一行提示 + Mac 名，再下面一张转录卡，最底主按钮。设置页是同一种厚卡片的纵向列表，形象轮播在最上面、卡片更大（200×240，D 是 148×172），返回键在左上。

材质参数：
- 大卡：圆角 34（连续曲率）。里面 = 状态渐变（同 D 的两色）+ 径向光（光色 70%、半径 10→230、中心 40% 高处）+ 噪点 10% + 顶部 35% 高的白色镜面高光（深色 10% / 浅色 30%）。
- 厚度：①内发光：10pt 白 16%（浅色 50%）描边、`blur 8` 后裁回卡内；②顶亮底暗的 1pt 渐变描边（白 35%→4%）；③两层阴影：光色 25% `radius 30 y 14` + 黑 50% `radius 2 y 1`。
- 小卡（转录卡、设置卡）：上→下 `white 0.115→0.075` 渐变（浅色白→97.5% 白），内发光 8pt 白 7% `blur 6`，1pt 顶亮描边（白 20%→3%），两层阴影（黑 45% `r18 y10` + 黑 30% `r1.5 y1`）。
- 页底：近黑 + 5% 噪点；设置页顶上加一团暖色径向光（18%）。

风险：设置页没有顶栏，往上滚时内容会滑到状态栏下面（实现时加顶部渐隐）；大卡里形象是方形半身裁切，底边直接被卡片切掉（Q 版是全身小人，在卡里显得小）；卡片占屏 53%，转录只能放两句；纯黑底在 OLED 上很好看，但浅色模式比 D 平淡。

## 共同风险 / 待定

- **素材体积**：4 组 × 5 张原图（1254² 左右透明 PNG）进资源目录约 46 MB。设计稿可以，实现时要压到 512² 或 @2x 以内（再看要不要转 HEIC）。
- **音量是假的**：在听的光环、设置页电平条现在用正弦模拟。`AudioEngine` 没有对外的电平，实现时要它在送帧时报一个能量值（本轮不许动它）。
- **音色 / 模型表是占位**：OpenAI、通义的默认值来自 `internal/voice/voice.go`；`gpt-realtime-mini`、`qwen3-omni-flash-realtime` 两个备选模型名和全部「气质描述」是我按公开资料写的，没核；Gemini 的音色列表按 brief 占位、界面上标了「列表待核」；Grok 列表是真的，但描述只有男 / 女声，没有气质描述。
- **配对信息是样例**：Mac 名、配对时间、中继域名在 app 里还没有这份数据（同第一轮）。
- **品牌色块**：不放官方 logo，只用品牌色 + 字母；要不要换成官方图标（涉及商标使用）留给用户。
- **署名**：关于页写了「水彩 / 霓虹两组由 gpt-image-2.5 基于原素材生成」，原素材 CC BY-NC-SA 4.0（非商用），和仓库 README「署名」一致；形象若上架商用要另谈授权。

## 截图索引

| 屏 | D「房间」 | E「卡片」 |
|---|---|---|
| 未配对 | [深](d/unpaired-dark.jpg) / [浅](d/unpaired-light.jpg) | [深](e/unpaired-dark.jpg) / [浅](e/unpaired-light.jpg) |
| 待机（idle，带「已自动挂断」通知） | [深](d/idle-dark.jpg) / [浅](d/idle-light.jpg) | [深](e/idle-dark.jpg) / [浅](e/idle-light.jpg) |
| 通话中 · 在听（listening） | [深](d/listening-dark.jpg) / [浅](d/listening-light.jpg) | [深](e/listening-dark.jpg) / [浅](e/listening-light.jpg) |
| 通话中 · 在想（thinking） | [深](d/thinking-dark.jpg) / [浅](d/thinking-light.jpg) | [深](e/thinking-dark.jpg) / [浅](e/thinking-light.jpg) |
| 通话中 · 在说（speaking） | [深](d/speaking-dark.jpg) / [浅](d/speaking-light.jpg) | [深](e/speaking-dark.jpg) / [浅](e/speaking-light.jpg) |
| Mac 不在线（offline） | [深](d/offline-dark.jpg) / [浅](d/offline-light.jpg) | [深](e/offline-dark.jpg) / [浅](e/offline-light.jpg) |
| 设置页上半 | [深](d/settings-dark.jpg) / [浅](d/settings-light.jpg) | [深](e/settings-dark.jpg) / [浅](e/settings-light.jpg) |
| 设置页下半 | [深](d/settings-bottom-dark.jpg) / [浅](d/settings-bottom-light.jpg) | [深](e/settings-bottom-dark.jpg) / [浅](e/settings-bottom-light.jpg) |
| 音色列表（OpenAI） | [深](d/voices-dark.jpg) / [浅](d/voices-light.jpg) | [深](e/voices-dark.jpg) / [浅](e/voices-light.jpg) |
| 音色列表（Grok，带「自定义音色」） | [深](d/voices-grok-dark.jpg) / [浅](d/voices-grok-light.jpg) | [深](e/voices-grok-dark.jpg) / [浅](e/voices-grok-light.jpg) |
| 形象轮播（选中水彩） | [深](d/carousel-dark.jpg) / [浅](d/carousel-light.jpg) | [深](e/carousel-dark.jpg) / [浅](e/carousel-light.jpg) |
| 配对页（固定深色） | [深](d/pair-dark.jpg) / [浅](d/pair-light.jpg) | [深](e/pair-dark.jpg) / [浅](e/pair-light.jpg) |
| Q 版 · 在听 | [深](d/persona-chibi-dark.jpg) / [浅](d/persona-chibi-light.jpg) | [深](e/persona-chibi-dark.jpg) / [浅](e/persona-chibi-light.jpg) |
| 水彩 · 在想 | [深](d/persona-watercolor-dark.jpg) / [浅](d/persona-watercolor-light.jpg) | [深](e/persona-watercolor-dark.jpg) / [浅](e/persona-watercolor-light.jpg) |
| 霓虹 · 在说 | [深](d/persona-neon-dark.jpg) / [浅](d/persona-neon-light.jpg) | [深](e/persona-neon-dark.jpg) / [浅](e/persona-neon-light.jpg) |
