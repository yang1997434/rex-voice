# Rex Voice iOS 界面：Liquid Glass 三版设计稿（2026-11-01）

用户看了 TestFlight 0.1.0 说「内容显示单调无聊、没有透明玻璃」，定了最低 iOS 26、用系统 Liquid Glass。这里是三版给用户挑的稿，定稿后下一份 brief 照着实现。

- 代码：`ios/RexVoice/Sources/Design/`。三版都编进每个包（这样 archive 能证明玻璃 API 在真机 SDK 下能编），但只有 DEBUG 包带环境变量 `REXVOICE_DESIGN=a|b|c` 才会显示；发版包启动的仍是旧的 `ContentView`。
- 预览假数据：DEBUG 包加 `REXVOICE_PREVIEW=unpaired|connecting|offline|idle|listening|thinking|speaking|settings|pair`，`CallController.applyPreview`（`#if DEBUG`）灌假状态和假转录，不连中继、不开音频；`settings` / `pair` 启动就打开那张页。
- 截图：模拟器 iPhone 17（iOS 27.2），状态栏固定 9:41，存的是 603×1311（原图一半）。重拍：`simctl launch` 时带 `SIMCTL_CHILD_REXVOICE_DESIGN=a SIMCTL_CHILD_REXVOICE_PREVIEW=idle`，深浅色用 `simctl ui <udid> appearance dark|light`。
- 设置页和配对页三版共用一套布局，只换主色（A 蓝、带渐变底；B 蓝；C 青）。设置页里的供应商 / 模型 / 音色 / API key 只是画出来（本地状态），模型和音色名是占位，接钥匙串和 `call.start` 是下一份 brief；「已配对的 Mac 名 / 时间」现在 app 里没有这份数据，稿里是写死的样例。

## A「电话」

取向：像打电话。整屏渐变底色跟着状态变（待机蓝、在听绿、在想紫、在说橙、不在线红），中间一颗 168pt 的圆形玻璃大按钮（没配对时是「配对」、待机是绿色「开始通话」、通话中是红色「挂断」），在听 / 在说时按钮背后有扩散的声波圈；最近三句转录用玻璃卡片从底部浮出。

玻璃用在：大按钮 `glassEffect(.regular.tint(…).interactive(), in: .circle)`；左上设置、右上配对两个圆形 `.buttonStyle(.glass)`，放在 `GlassEffectContainer` 里；转录卡片 `glassEffect(.regular, in: .rect(cornerRadius: 22))`，空状态用 `.clear` 玻璃，也在一个 `GlassEffectContainer` 里；通知条是玻璃胶囊。

风险：只显示最近三句，长对话要往回看得另做入口；整屏饱和色在深色模式下偏重（不在线的红底很扎眼），要不要降饱和看用户；声波圈在通话中一直按帧刷新，耗电要真机看（开「减少动态效果」时停掉）。

## B「对话」

取向：像 iMessage。聊天流是主体，我说的话是蓝色着色玻璃气泡、助手是无色玻璃气泡；顶部系统玻璃导航栏 + 一颗状态胶囊（色点 + 「在听」），底部一条玻璃通话栏：待机时是一整条 `.glassProminent` 绿色「开始通话」，通话中变成「声波 + 一句提示 + 计时」的玻璃胶囊和一颗红色圆形挂断键。

玻璃用在：导航栏和工具栏按钮（iOS 26 系统自带）；状态胶囊 `glassEffect(.regular.tint(…), in: .capsule)`；底部 `safeAreaBar` 里的 `GlassEffectContainer`（提示胶囊 + `.glassProminent` 按钮）；气泡 `glassEffect(.regular / .regular.tint(.blue), in: .rect(cornerRadius: 20))`；滚动边缘 `scrollEdgeEffectStyle(.soft)`。

风险：苹果的指引是玻璃给「浮在内容上的控件层」用，气泡属于内容层，气泡多了以后滚动时每个都在实时折射，老机型上可能掉帧（没在真机测）；整体最像普通聊天 app，辨识度最低。

## C「仪表」

取向：像控制台。顶部一张大玻璃状态卡：Mac 名、经哪个中继、大字状态、通话时长和声波、三格小指标（链路 / 通话 / 门限）、通知条；中间是不带气泡的转录日志（「我 / Rex」标签 + 正文）；底部玻璃工具栏：配对、通话（主键）、设置三颗。

玻璃用在：状态卡 `glassEffect(.regular.tint(…), in: .rect(cornerRadius: 32))`，里面的 Mac 图标是圆形玻璃；底部 `safeAreaBar` 里的 `GlassEffectContainer(spacing: 14)`，三颗按钮各自 `glassEffect(….interactive())` 并带 `glassEffectID`（状态切换时玻璃形状会融合变形）。

风险：放进 `GlassEffectContainer` 的玻璃不吃 `.opacity`，所以「不可用」改成去掉着色、文字变淡来表示（连接中 / 不在线时的通话键）；信息最多，小指标里「门限」对一般用户意义不大，可能要换成别的；转录区域比 B 小。

## 截图索引

| 屏 | A | B | C |
|---|---|---|---|
| 未配对 | [浅](a/unpaired-light.png) / [深](a/unpaired-dark.png) | [浅](b/unpaired-light.png) / [深](b/unpaired-dark.png) | [浅](c/unpaired-light.png) / [深](c/unpaired-dark.png) |
| 连接中 | [浅](a/connecting-light.png) / [深](a/connecting-dark.png) | [浅](b/connecting-light.png) / [深](b/connecting-dark.png) | [浅](c/connecting-light.png) / [深](c/connecting-dark.png) |
| Mac 不在线（带通知条） | [浅](a/offline-light.png) / [深](a/offline-dark.png) | [浅](b/offline-light.png) / [深](b/offline-dark.png) | [浅](c/offline-light.png) / [深](c/offline-dark.png) |
| 待机（带通知条「已自动挂断」、上次的转录） | [浅](a/idle-light.png) / [深](a/idle-dark.png) | [浅](b/idle-light.png) / [深](b/idle-dark.png) | [浅](c/idle-light.png) / [深](c/idle-dark.png) |
| 通话中 · 在听 | [浅](a/listening-light.png) / [深](a/listening-dark.png) | [浅](b/listening-light.png) / [深](b/listening-dark.png) | [浅](c/listening-light.png) / [深](c/listening-dark.png) |
| 通话中 · 在想 | [浅](a/thinking-light.png) / [深](a/thinking-dark.png) | [浅](b/thinking-light.png) / [深](b/thinking-dark.png) | [浅](c/thinking-light.png) / [深](c/thinking-dark.png) |
| 通话中 · 在说 | [浅](a/speaking-light.png) / [深](a/speaking-dark.png) | [浅](b/speaking-light.png) / [深](b/speaking-dark.png) | [浅](c/speaking-light.png) / [深](c/speaking-dark.png) |
| 设置页 | [浅](a/settings-light.png) / [深](a/settings-dark.png) | [浅](b/settings-light.png) / [深](b/settings-dark.png) | [浅](c/settings-light.png) / [深](c/settings-dark.png) |
| 配对页 | [浅](a/pair-light.png) / [深](a/pair-dark.png) | [浅](b/pair-light.png) / [深](b/pair-dark.png) | [浅](c/pair-light.png) / [深](c/pair-dark.png) |
