# Rex Voice iOS 主屏：第三轮设计稿 F / G / H——侧栏布局（2026-11-02）

用户真机（iPhone Duo）反馈：「现在所有内容都在上方，希望改到侧边；把上面显示的供应商名和『经 r.maruiao.top』去掉；供应商移到侧边栏，显示当前用的是哪家，直接用它们的 logo 选」。这轮两版给用户挑，定稿后再出实现 brief。同批修了「选好音色一换供应商就跑回默认」（见文末）。

- 代码：两版都是现行主屏 `ios/RexVoice/Sources/ContentView.swift` 的另外两种排法（`MainLayout.rail` = F、`.panel` = G），形象卡、状态行、转录卡、附件卡、主按钮全部复用现有实现，材质和 token 沿用 `V2Kit.swift` / `SettingsPage.swift` 的 `CardSurface`。供应商 logo 和选择器在 `Design/ProviderPicker.swift`。
- 切版：只有 DEBUG 包带 `REXVOICE_DESIGN=f|g|h` 才显示；发版包写死 `ContentView()`（默认 `.cards`），不受影响。设置页、配对页两版共用现行的，不变。
- 预览环境变量（DEBUG）：沿用 `REXVOICE_PREVIEW`（假状态 + 假转录，`CallController.applyPreview`）、`REXVOICE_PERSONA`、`REXVOICE_PROVIDER`；新加：
  - `REXVOICE_DESIGN=f|g|h`：切版（H = `MainLayout.duoRail`）。带它时，通话中的假状态多一张图片附件 + 一条链接，好看附件卡落在哪。
  - `REXVOICE_PANEL=open`：G 窄屏一打开就是面板滑出的样子。
  - `REXVOICE_ORIENT=landscape`：启动后把窗口转成横屏（模拟器拍宽态用）。
- 截图：模拟器 iOS 27.2，状态栏 9:41，存 603 宽 JPEG。三种尺寸：
  - 窄：iPhone 17 竖屏（402pt 宽）。
  - 宽：iPhone 17 Pro Max 横屏（956×440pt）。app 只支持 iPhone（`TARGETED_DEVICE_FAMILY: 1`），iPad mini 上会按 iPhone 兼容模式跑、出不了宽态，所以没用 iPad。**模拟器没法把 Duo 展开（Device Hub 不能脚本驱动），宽态是用横屏代替 Duo 内屏**，内屏比例更接近方形，真机上两列会比截图高。
  - Duo 外屏：模拟器 iPhone Duo 合上状态（右侧一列是系统状态栏安全区）。
  - 重拍脚本是 scratchpad 的 `design-shots-v3.sh <udid> <输出目录> <前缀> [landscape]`（`simctl launch` 带 `SIMCTL_CHILD_REXVOICE_*`，深浅色用 `simctl ui appearance`；Duo 外屏要加 `DISP=1`）+ `design-thumb-v3.sh` 缩到 603 宽。H 用 `design-shots-h.sh`（同参数，只拍 h，`STATES=idle` 只拍待机），宽态那组是在 iPhone 18 Pro Max 模拟器（本机没有 17 Pro Max）上拍的，横屏同样是 956×440pt。

## 两版共有的

- **去掉顶栏**：「Rex Voice」字标、顶上的配对 / 设置按钮都没了，挪进侧栏 / 面板。
- **状态行去掉「<供应商> · 经 <域名>」那行**：当前供应商由高亮的 logo 表示，中继域名不再显示；未配对时仍显示「还没配对 Mac」。
- **供应商选择器**：四家 logo，当前那家有底色（深色白 16% / 浅色黑 8%）+ 1.5pt 描边（主色 60%），圆角 0.32 倍格宽；点一下 = 设置页改供应商（同一个 `settings.provider`，设置页打开时同步）。通话中整组变淡到 40%，点了左右抖一下（6pt，约 0.26 秒）+ 一次错误触感，不切；开「减少动态效果」时不抖、只有触感。VoiceOver 读供应商全名、选中态、通话中提示「通话中不能换」。
- **logo 是占位**：自绘单色标记——品牌色圆底 + 白色简化字形（OpenAI 三个转 60° 的椭圆拼成六瓣结、通义「Q」、Gemini 四角星芒、Grok「X」），字形 24pt 见方。不是官方 logo，正式 logo 等用户给素材再换（换 `ProviderLogo.glyph` 一处即可）；用官方 logo 涉及商标使用，留给用户定。

## F「左侧竖栏」

取向：像 iPad 应用的侧边导航——左边一条 56pt 宽的竖栏（`CardSurface` 圆角 28），顶部四家 logo 竖排（48pt 格、36pt 圆），底部「配对」「设置」两个 44pt 圆钮；右边是原来的内容。

- 窄屏：右边从上到下 形象卡（最多 0.53 屏高）→ 状态行 → 转录卡 → 附件卡 → 主按钮，和现行一样，只是整体往右让出竖栏。
- 宽屏 / 横屏：竖栏不变，右边是现行的两列（形象卡 0.9:1 | 状态 + 转录 + 附件 + 主按钮）。
- 好处：供应商一直看得见、一下就能切；操作都在左手拇指那一侧。
- 风险：窄屏形象卡宽度被竖栏吃掉约 68pt（402pt 宽的 iPhone 17 上卡宽约 308pt）；**Duo 外屏通话中有附件时，形象卡被压到约 37% 屏高**（附件卡和转录卡不缩，形象让位，见 `f/duo-outer-listening-*`），定稿后要么附件卡在短屏上收成一行，要么形象卡设最小高度。竖栏中间大段留空。

## G「右侧面板」

取向：形象是主角、占满大半屏，其余信息收进右侧面板。

- 宽屏 / 横屏：左边形象卡占满，右边 300pt 固定面板：顶上一个托盘（四家 logo 横排 40pt 格 + 「配对」「设置」两个 36pt 小圆钮），下面状态行、转录卡、附件卡（放不下时这一段可上下滚，横屏只有约 400pt 高），最底主按钮。
- 窄屏：形象卡占全宽、几乎全高，下面只有提示气泡和主按钮；右边缘中间一个抓手（5×56pt 竖条，点按区 28×132pt），点开后整屏压暗（深色黑 50% / 浅色 28%），面板（宽 min(300pt, 屏宽−48)，页底色 + 1pt 亮边 + 左投影，圆角 28）从右滑入，内容同宽屏面板但不带主按钮。点压暗处、向右拖 60pt 以上、或 VoiceOver 的「返回」手势都能收起；开「减少动态效果」时只淡入淡出不滑。
- 好处：形象最大；横屏 / 内屏信息密度和 F 差不多。
- 风险：窄屏平时看不到状态行和转录（只有形象卡左上的状态小胶囊和提示气泡），要看得点抓手；抓手很淡、第一次可能找不到；形象图是按宽度适配的半身像，窄屏卡片很高时上面约 40% 是空的光晕（见 `g/narrow-idle-*`）。

## H「Duo 外屏右侧竖栏」

用户看了 F / G 后的反馈：都不对，「应该放到 Wi-Fi 下面那部分，把 Duo 外屏利用率最大化」，要特别为 Duo 优化。

取向：Duo 外屏合上时系统状态栏是竖在右边的一列安全区，时间 / Wi-Fi / 电池只占这列顶上一小段，下面到底全空。H 把竖栏（和 F 同一条：56pt 宽 `CardSurface` 圆角 28，四家 logo 竖排 48pt 格、36pt 圆，弹性空白，底部「配对」「设置」两个 44pt 圆钮）画进这一列、状态图标下面，内容区不往右让。

- Duo 外屏：内容区还是 G 窄屏那么宽（603 宽截图里 x 22–473），从上到下 形象卡 → 状态行 → 转录卡 → 附件卡 → 主按钮，形象卡比 F 宽约 68pt。竖栏在右侧那列居中，内容右边缘到竖栏左边缘约 30pt。
- 普通 iPhone 竖屏（没有右侧安全区）：退成 F 那条竖栏放右边，内容往左让。
- 宽屏 / 横屏（代替 Duo 内屏）：竖栏靠右，左边两列（形象 | 状态 + 转录 + 附件 + 主按钮）。横屏 iPhone 左右也有安全区（灵动岛那侧），所以只有竖屏（宽 < 高）且右侧安全区 ≥ 56pt 才算「Duo 外屏那一列」。
- 实测（iPhone Duo 模拟器 iOS 27.2，合上状态）：屏幕 466×678pt；`safeAreaInsets` 上 0、左 0、下 34、**右 84**，app 可用区 382×644pt。状态图标簇（时钟、Wi-Fi 圈、电池点）从屏顶约 87pt 到 **151pt**，所以竖栏顶 = 151 + 12 = 163pt（代码里常量 `duoStatusClusterBottom = 151`，图标簇的位置读不到，只能写死，换 iOS 版本要重量）；竖栏底留下安全区 34pt + 8pt，和主按钮底齐平。写法：竖栏放在 `ZStack` 顶层，`.frame(width: 右侧 inset)` 再 `.frame(maxWidth: .infinity, alignment: .trailing).ignoresSafeArea(.container, edges: [.top, .trailing])`，就落进那一列。
- 点按：用临时 XCUITest（不进仓）在 Duo 模拟器上按屏幕坐标点（走真实命中测试）最上面的 OpenAI logo（400,169）和「设置」（402,586），前者变成选中、后者打开设置页，都能点。
- 好处：Duo 外屏那条本来浪费的竖列用上了，内容区一点不让位，形象卡是三版里最宽的；供应商和配对 / 设置一直看得见，在右手拇指那一侧。
- 风险：
  - 依赖 Duo 外屏把状态栏放在右侧这个系统行为和图标簇高度（151pt 是写死的）；真机若和模拟器不同（比如图标簇更长、或有来电 / 录音指示条长在下面），竖栏顶部会被盖住，要真机核一眼。通话中截图里状态列顶上多出一个黑色圆点（系统画的，不是 app 的），在图标簇上方，不影响竖栏。
  - 竖栏比 F 短（从 163pt 开始），四家 logo 之后还有大段空白；以后供应商多到七八家才会挤。
  - 和 F 一样，**Duo 外屏通话中有附件时形象卡被压到约 30% 屏高**（见 `h/duo-outer-listening-*`），比 F 宽但更矮；定稿后同样要附件卡在短屏收成一行或形象卡设最小高度。
  - 普通 iPhone 竖屏上就是「F 镜像到右边」，没有额外收益。

## 音色锁定（两版共用，也进发版包）

- 根因：`Settings.swift` 的 `provider.didSet` 切供应商时清掉新那家的 model / voice（11-01 定的「换家回默认」）。用户 11-02 推翻：各家各自记住。
- 改法：去掉那两行 `removeObject`；每家的 model / voice 各存在 `model.<家>` / `voice.<家>`，切走再切回来还在。换模型后原音色不在新模型的列表里时才回该列表默认（`setModel` 原逻辑不动）。
- 自检：DEBUG 包 `REXVOICE_SELFTEST=voice`（`AppSettings.voiceSelfTest`）：选 grok 音色 eve → 切 qwen → 切回 grok 仍是 eve；qwen 选 Ryan 后换到 qwen-audio-3.1 模型回默认音色。跑完把用户原来的选择放回去，打印 `[selftest] PASS`。把那两行加回去时只有「切回 grok 仍是 eve」一条失败（已验）。

## 截图索引

| 屏 | F「左侧竖栏」 | G「右侧面板」 |
|---|---|---|
| 窄 · 待机（带「已自动挂断」提示） | [深](f/narrow-idle-dark.jpg) / [浅](f/narrow-idle-light.jpg) | [深](g/narrow-idle-dark.jpg) / [浅](g/narrow-idle-light.jpg) |
| 窄 · 在听（通话中，供应商变淡，带附件） | [深](f/narrow-listening-dark.jpg) / [浅](f/narrow-listening-light.jpg) | [深](g/narrow-listening-dark.jpg) / [浅](g/narrow-listening-light.jpg) |
| 窄 · 在听 · 面板滑出 | — | [深](g/narrow-listening-panel-dark.jpg) / [浅](g/narrow-listening-panel-light.jpg) |
| 宽（横屏）· 待机 | [深](f/wide-idle-dark.jpg) / [浅](f/wide-idle-light.jpg) | [深](g/wide-idle-dark.jpg) / [浅](g/wide-idle-light.jpg) |
| 宽（横屏）· 在听 | [深](f/wide-listening-dark.jpg) / [浅](f/wide-listening-light.jpg) | [深](g/wide-listening-dark.jpg) / [浅](g/wide-listening-light.jpg) |
| Duo 外屏 · 待机 | [深](f/duo-outer-idle-dark.jpg) / [浅](f/duo-outer-idle-light.jpg) | [深](g/duo-outer-idle-dark.jpg) / [浅](g/duo-outer-idle-light.jpg) |
| Duo 外屏 · 在听 | [深](f/duo-outer-listening-dark.jpg) / [浅](f/duo-outer-listening-light.jpg) | [深](g/duo-outer-listening-dark.jpg) / [浅](g/duo-outer-listening-light.jpg) |
| Duo 外屏 · 在听 · 面板滑出 | — | [深](g/duo-outer-listening-panel-dark.jpg) / [浅](g/duo-outer-listening-panel-light.jpg) |

H「Duo 外屏右侧竖栏」：Duo 外屏 · 待机 [深](h/duo-outer-idle-dark.jpg) / [浅](h/duo-outer-idle-light.jpg)；Duo 外屏 · 在听 [深](h/duo-outer-listening-dark.jpg) / [浅](h/duo-outer-listening-light.jpg)；窄 · 待机 [深](h/narrow-idle-dark.jpg) / [浅](h/narrow-idle-light.jpg)；宽（横屏）· 待机 [深](h/wide-idle-dark.jpg) / [浅](h/wide-idle-light.jpg)。

截图里当前供应商是 Grok（`REXVOICE_PROVIDER=grok`），形象是像素组。
