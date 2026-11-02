# Rex Voice iOS 主屏：第四轮设计稿 J / K——供应商轮盘 + 通话内容区（2026-11-02）

## 定稿：J（2026-11-02 用户定）；轮盘按 2a–2c 改；logo 用官方

发版 0.3.2。K 版代码（环境卡、`paneSummary` 占位）和 `REXVOICE_DESIGN` 开关已删，J 是窄屏（Duo 外屏、普通 iPhone 竖屏）唯一布局；横屏 / Duo 内屏仍是两列。下面 J / K 两版的说明和截图留作记录，「切版与预览变量」一节里 `REXVOICE_DESIGN` 已不存在，`REXVOICE_LINES` / `REXVOICE_WHEEL` 照旧（只在 DEBUG）。

- **2a 圆盘收小、不碰别的**：三家改成 36pt、离中心 60pt，方向从左上 / 左 / 左下改成 105° / 150° / 195°（整体往上转，最低那家只比中心低 16pt）；磨砂底是三家 logo 的最小包络圆 + 8pt 外边距（半径约 68pt，圆心在中心 logo 左上），不再以中心 logo 为圆心。Duo 外屏上圆盘右边离屏边约 11pt，下沿离「配对」钮约 5pt；iPhone 17 竖屏同样不出屏、不压钮。
- **2b 选了谁**：展开时中心那家（当前）一圈 2pt 主色描边（主色 = 当前状态色，待机是暖橙）+ 右下角主色小勾；手指滑到别家，勾移到那家（那家同时放大 1.18 倍）。松手选中后，竖栏 logo 下的家名从 9pt 浅灰变成 10pt 加粗深色 1.6 秒再回去。VoiceOver 读「当前：Grok」。
- **2c 收起也看得出是哪家**：竖栏 logo 下常驻一行 9pt 浅灰家名（Grok / 千问 / GPT / Gemini），通话中随 logo 一起变淡到 40%。
- **官方 logo**：lobehub/icons-static-svg 的单色 SVG（MIT），放在 `Assets.xcassets/Logos/logo-<家>.imageset`，矢量 + 模板渲染，白色图形压在原来的品牌色圆底上；设置页「声音」四格也用同一个 `ProviderLogo`。原 SVG 的路径把弧线标志位挤在一起写（`0 01-4.45`、`.503.503`），系统的 SVG 解析读错、Gemini 整个画不出来，所以入库前把路径数据改写成带分隔符的等价写法，图形没变。
- **j/duo-call-light 右上的黑圆点**：不是 app 画的，是模拟器在 Duo 外屏摄像头位置画的传感器区。证据：那块区域在 app 的视图树里没有任何元素；只在 app 有逐帧动画（在听的声波、在说的呼吸）时间歇出现，在想 / 待机连拍 10–16 张一次没有，暂停声波后在听也几乎不出现；切到系统「设置」app 后马上消失。定稿截图挑了没有圆点的那一帧。

| 屏 | J 定稿（0.3.2） |
|---|---|
| Duo 外屏 · 待机 · 浅 | [j/final-duo-idle-light](j/final-duo-idle-light.jpg) |
| Duo 外屏 · 通话中（8 条 + 图片 + 链接）· 浅 | [j/final-duo-call-light](j/final-duo-call-light.jpg) |
| Duo 外屏 · 待机 · 轮盘展开 · 浅 | [j/final-duo-wheel-light](j/final-duo-wheel-light.jpg) |
| iPhone 17 竖屏 · 通话中 · 浅 | [j/final-narrow-call-light](j/final-narrow-call-light.jpg) |

新文件，没覆盖原来的 J / K 截图。滑选用临时 XCUITest（不进仓）在 Duo 模拟器上走真实触摸验过：按住 0.7 秒滑到左上松手 → 换成千问、竖栏家名变「千问」；把滑选命中半径改成 0 时这条变红。

用户装了 0.3.1 后反馈两件事：

1. 四家顺序改成 Grok、千问在前，GPT、Gemini 在后；竖栏里四个 logo 浓缩成一个（当前这家），长按弹出轮盘选，省空间。
2. 通话内容区：待机时不该显示转录（或者放个更好看的东西）；有内容时要能滚动；区域要更大更好看。

轮盘按用户描述只出一版，J / K 两版共用；内容区出 J / K 两版给用户挑。设置页、竖栏位置（H 版定稿，贴底）不变。

## 两版共有的（也进发版包）

- **顺序**：`VoiceProvider.allCases` 改成 Grok、通义 Qwen、OpenAI、Gemini，设置页「声音」那组四格和轮盘都按这个顺序。存的是 raw value（`provider` 键），换顺序不影响已经选好的那家。
- **轮盘**（`ios/RexVoice/Sources/Design/ProviderWheel.swift`，替换竖栏里原来的四 logo 竖排 `ProviderPicker`，那段代码已删，`ProviderLogo` 留在 `ProviderPicker.swift`）：
  - 竖栏里只放当前这家一个 logo（44pt 圆，48pt 点按区），下面照旧是「配对」「设置」。
  - 按住 0.35 秒：以它为圆心往左上 / 左 / 左下扇形弹出其余三家（半径 72pt，按上面的顺序从上到下），底下垫一块圆形磨砂底（半径 72 + 22 + 14pt），带弹簧动画和一次中等触感。手指滑到哪家，那家放大 1.18 倍、加一圈描边、一次选择触感；在那家上松手就选中、收起。
  - 按住弹开后在中间松手：轮盘留着，可以直接点某一家；再点中间、或点屏上别处收起。
  - 短按：左右轻抖一下（5pt）+ 轻触感，左边浮出「长按换供应商」小胶囊 1.6 秒。
  - 通话中：logo 变淡到 40%；长按不展开、只抖一下 + 错误触感；接通时已展开的轮盘自动收起。
  - 往左展开、不碰屏边：竖栏在右，三家都在 logo 左边；最下那家比 logo 低 51pt，Duo 外屏上 logo 下面还有「配对」「设置」两个钮（约 110pt），不会出屏底。
  - 手势：一个 `DragGesture(minimumDistance: 0)` 自己计时，区分短按 / 长按 / 滑选，不和点按抢；竖栏不在任何 ScrollView 里，和对话区滚动互不相干。
  - VoiceOver：读当前家名 +「长按换供应商」（通话中「通话中不能换」）；双击直接展开 / 收起；展开后三家各是按钮、可点，收起时它们不在视图树里。
  - 「减少动态效果」：不弹簧、不抖，三家直接淡入。
- **两版待机都不显示转录**；**都没有独立的「附件」卡**，附件作为 Rex 那侧的气泡卡片按到达时间插在对话里（`Line` / `Attachment` 各加了到达时间 `time`）。
- **对话气泡**（`ios/RexVoice/Sources/Design/DialogList.swift`，两版共用）：我在右（当前状态主色 20–26% 底 + 同色细边），Rex 在左（浅色白底 / 深色 13% 灰底 + 小阴影），12pt 圆角，不显示时间；图片气泡 200×125 点开全屏（原有 `ZoomImage`），链接气泡带图标、标题、域名，点开 Safari。新消息自动滚到底；用户往上翻离底超过 24pt 就不再自动滚，底部出现「回到最新」小胶囊，点了滚回底。对话区顶部 14pt 渐隐。通话中的提示（报错等）浮在对话区顶上。

## J「通话时换挡」

- 待机：形象卡占满上面，下面只有一句状态（「Mac 在线，随时可以打」，有提示时多一个提示胶囊）和主按钮，没有转录卡。
- 接通后：形象卡缩成顶部一条 88pt 横幅——左边 56pt 圆形形象头像（状态色光晕底），中间状态（在听 / 在想 / 在说 + 声波）和一行提示，右边计时；横幅下面整段都是对话。
- 挂断：回到待机形态；对话数据留着，下次通话开始时清空（和现有逻辑一致）。
- 好处：通话时对话区最大（Duo 外屏约 450pt 高），看得最多。
- 代价：通话中形象只剩小头像，表情变化不显眼。

## K「形象常驻、对话下沉」

- 形象卡始终不少于 42% 屏高（Duo 外屏约 285pt）。
- 待机：下面一块「环境卡」——第一行绿点 +「Mac 在线 · 3 个 Claude 在跑 · 1 个在等你」，第二行淡色「上次 · Rex：<上次通话最后一句>」，有提示时再加提示胶囊；形象卡占掉剩下的高度。没配对 / Mac 不在线时第一行换成对应提示、灰点。
- 通话中：形象卡固定 42% 屏高，下面整段到屏底是对话，主按钮悬浮在底部渐变（页底色从透明到实色）上，对话最后一条下面留出按钮的位置。
- 好处：形象一直是主角；待机时有信息、不空。
- 代价：Duo 外屏通话中对话区只有约 300pt，一屏 3–4 条。
- **「几个 Claude 在跑 / 在等你」目前没有真数据**：手机这边拿不到 `list_panes` 的结果（Mac 不往手机发窗格摘要）。代码里是 `CallController.paneSummary`，现在只有预览会填；为空时第一行只显示「Mac 在线」。选 K 的话要在协议里加一条 Mac → 手机的窗格摘要消息（Mac 侧改动，另起批次）。

## 切版与预览变量（DEBUG）

- `REXVOICE_DESIGN=j|k`：切版，只管窄屏（Duo 外屏、普通 iPhone 竖屏）；横屏 / Duo 内屏仍是 0.3.1 的两列。发版包里这个开关编译不进去，主屏是 0.3.1 布局（顺序和轮盘除外）。
- `REXVOICE_LINES=<n>`：Mac 在线时把假对话换成 n 条（8 条一轮、超出重复），截图用 8。
- `REXVOICE_WHEEL=open`：一打开轮盘就是展开的样子。
- 沿用：`REXVOICE_PREVIEW`（假状态，`idle` / `listening` …）、`REXVOICE_ATTACH=1`（通话中加一张图片 + 一条链接）、`REXVOICE_PERSONA`、`REXVOICE_PROVIDER`。预览里环境卡的「3 个在跑 · 1 个在等你」是写死的假数据。

## 截图

模拟器 iOS 27.2，状态栏 9:41，603 宽 JPEG；当前供应商 Grok，形象像素组。Duo 外屏是 iPhone Duo 模拟器合上状态（右侧一列是系统状态栏）；窄是 iPhone 17 竖屏。通话中那几张都带 `REXVOICE_LINES=8 REXVOICE_ATTACH=1`，停在列表底部。

| 屏 | J「通话时换挡」 | K「形象常驻、对话下沉」 |
|---|---|---|
| Duo 外屏 · 待机 · 浅 | [j/duo-idle-light](j/duo-idle-light.jpg) | [k/duo-idle-light](k/duo-idle-light.jpg) |
| Duo 外屏 · 待机 · 深 | [j/duo-idle-dark](j/duo-idle-dark.jpg) | [k/duo-idle-dark](k/duo-idle-dark.jpg) |
| Duo 外屏 · 通话中（8 条 + 图片 + 链接）· 浅 | [j/duo-call-light](j/duo-call-light.jpg) | [k/duo-call-light](k/duo-call-light.jpg) |
| Duo 外屏 · 待机 · 轮盘展开 · 浅 | [j/duo-wheel-light](j/duo-wheel-light.jpg) | [k/duo-wheel-light](k/duo-wheel-light.jpg) |
| iPhone 17 竖屏 · 通话中 · 浅 | [j/narrow-call-light](j/narrow-call-light.jpg) | [k/narrow-call-light](k/narrow-call-light.jpg) |

## 交互核对

轮盘用临时 XCUITest（不进仓）在 Duo 模拟器上走真实触摸：按住 0.7 秒滑到左上松手 → 换成通义 Qwen；按住后在中间松手 → 轮盘留着、点 Gemini 换成 Gemini；短按 → 仍是 Grok、轮盘没开；通话中按住滑 → 仍是 Grok。四条都过；把滑选命中半径改成 0 时只有「滑到左上」那条变红。

## 没覆盖的

- 真机触感、「减少动态效果」、VoiceOver 实际朗读没在真机上试。
- 对话区「往上翻停止自动滚 / 回到最新」只在静态截图里确认了初始停在底部、胶囊不误出现；滚动中途来新消息的行为没在模拟器里动态测。
