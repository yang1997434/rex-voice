# 形象素材（第一批：Claude 4 组 × 5 状态；第二批见文末）

给 iOS app「选 Claude 的形象」用，形象随通话状态换表情（2026-11-01 用户定）。每组 5 张固定文件名：`idle.png`（待机）、`listening.png`（在听）、`thinking.png`（在想）、`speaking.png`（在说）、`offline.png`（Mac 不在线 / 挂断），外加一张横向拼图 `preview.jpg`（5 格 × 512px，只给人看，不进 app）。所有 PNG 都是透明背景，尺寸按原素材或生成接口返回的原样保留（界面里自己缩），没有缩放过。

| 组 | 来源 | 尺寸 |
|---|---|---|
| `pixel/` | 原素材 `bust/` 直接用 | 1254×1254 |
| `chibi/` | 原素材 `chibi/` 直接用 | 1230×1278（speaking 是 1312×1199） |
| `watercolor/` | gpt-image-2.5 以 bust 为参考生成 | 1254×1254 |
| `neon/` | 同上 | 1254×1254 |

## pixel（像素大小姐）

原图下载改名，一张没改。按 `prompts/bust.json` 里的表情描述选的：

| 状态 | 原文件 | 原表情描述 |
|---|---|---|
| idle | `bust_smile` | a warm gentle smile, eyes slightly narrowed with kindness |
| listening | `bust_neutral` | calm neutral expression, gentle eyes on the viewer（候选 `bust_gentle_thanks` 是闭眼手按胸口，不像「在听」，没选） |
| thinking | `bust_thinking` | index finger on her chin, eyes looking up and to the side |
| speaking | `bust_talking` | mid-sentence while explaining, one hand raised with palm open, mouth slightly open |
| offline | `bust_sleepy` | eyes half-closed and drowsy, rubbing one eye, a tiny yawn |

## chibi（Q 版）

原图下载改名，按 `prompts/chibi.json` 的描述选：

| 状态 | 原文件 | 原描述 |
|---|---|---|
| idle | `chibi_happy` | beaming with joy, both little hands raised cheerfully, golden sparkles |
| listening | `chibi_head_tilt_cute` | an exaggerated cute head tilt, both hands under her chin, big innocent eyes - 'hm?' |
| thinking | `chibi_thinking_cloud` | eyes looking up, a finger on her lips, a thought cloud with a teacup and cake |
| speaking | `chibi_megaphone` | shouting cheerfully into a cream megaphone, sound wave arcs（没有「讲解」款，这张最接近「在说」） |
| offline | `chibi_sleepy` | dozing off while standing, eyes closed, hugging a little orange pillow |

## watercolor（水彩插画风，生成）

接口：中转站 `POST /v1/images/edits`，模型 `gpt-image-2.5-sunburst`，`size=1024x1024 quality=high background=transparent output_format=png`（接口认 `transparent`，返回的是 1254² 带 alpha）。`image[]` 传两张：第一张 `bust_talking.png`（角色设定），第二张该状态对应的 bust（idle=`bust_smile`、listening=`bust_neutral`、thinking=`bust_thinking`、speaking=`bust_talking`、offline=`bust_sleepy`；speaking 只传一张）。

5 张用同一段提示词，只换「State」句：

> The first attached image is the approved character design of Claude-chan (a bust portrait); the second shows the target expression and pose. Keep her identity exactly: long, flowing, slightly wavy bright orange hair with side-swept bangs; amber-brown eyes; a large orange many-petaled flower hair ornament with a pearl centre on the left side of her head, with a black ribbon bow and white lace ribbon tails behind it; small gold drop earrings; a white Victorian blouse with a ruffled high collar and a frilled jabot; a black bow at the collar pinned with a small orange flower brooch with a pearl centre; a fitted black vest with thin gold trim, and white puffed sleeves. Below the waist she wears a black-and-white layered dress: a black overskirt with thin gold trim layered over several tiers of white lace ruffles, ending just below the knee, white frilled socks and glossy black Mary Jane shoes. Render as soft watercolor illustration with visible paper texture and loose ink lines, warm palette; bust portrait, transparent background, no text. **{State 句}** Bust portrait from the top of the hair to just below the chest, framed like the first attached image, facing the viewer. Transparent background. No text, letters, logos or watermark.

State 句（neon 组同一套）：

- idle：State: idle - a warm relaxed gentle smile, eyes slightly narrowed with kindness, hands resting.
- listening：State: listening - attentive, focused eyes on the viewer, head slightly tilted, one hand cupped beside her ear.
- thinking：State: thinking - index finger on her chin, eyes looking up and to the side, pondering.
- speaking：State: speaking - mid-sentence while explaining, eyebrows lively, one hand raised with palm open, mouth open.
- offline：State: offline - dozing off, eyes closed and drowsy, head nodding to one side, a tiny yawn.

重生成：只有 `idle.png` 生成了两次。第一次是脚本的坑，不是模型的——`curl -F "prompt=..."` 把提示词里第一个分号后面的内容当成了表单选项，模型只收到前 44 个 token，原样吐回了像素风参考图；改用 `--form-string` 后一次过。其余 4 张都是一次成的。

## neon（赛博霓虹风，生成）

接口、参考图、State 句都和 watercolor 相同，只把风格句换成：

> Render as clean vector anime with cyberpunk neon rim light (cyan and magenta) on dark, glossy highlights; bust portrait, transparent background, no text.

5 张都是一次成的，没有重生成。因为要求透明背景，「on dark」只体现在轮廓的青 / 品红边缘光上，没有深色底；放到深色界面上才出效果。

## 署名

素材：Claude大小姐的日记本（github.com/opsCar/claude-daxiaojie-assets）· 角色设定：ZipZipPipe（胖次狐）· 设定图：lmy414/ai-chan-sheets · CC BY-NC-SA 4.0。`pixel/`、`chibi/` 是原素材改名；`watercolor/`、`neon/` 用 gpt-image-2.5 基于该素材生成。该协议是非商用的，本 app 仅内测自用；若要商用须换图。


---

# 第二批：各家 AI 娘（8 组 × 5 状态，2026-11-01）

用户 2026-11-01 定：形象要多组可选，点名加 DeepSeek 鲸鱼娘，并「多生成点可选」。8 组都是 gpt-image-2.5 生成，参考原图和来源记录在 `_refs/<角色>/`（每组一个 `source.md`，总说明在 `_refs/README.md`）。

| 组 | 角色 | 风格 | 参考图（`image[]` 顺序） | 尺寸范围 |
|---|---|---|---|---|
| `deepseek/` | DeepSeek 鲸鱼娘 | 清爽动漫插画 | `_refs/deepseek/02-simple-short-maid.png`（站方自制三视图）+ `gj-deepseek.webp` | 1142–1287 × 1222–1378 |
| `deepseek-chibi/` | 同上 Q 版 | chibi | `05-chibi-blue-maid.png` + `02-simple-short-maid.png` | 1024–1214 × 1296–1536 |
| `gpt/` | GPT 娘（白龙娘） | 清爽动漫插画 | `gj-gpt-reference.webp` + `gj-gpt.webp` | 1111–1199 × 1312–1415 |
| `qwen/` | Qwen 娘 | 同上 | `gj-qwen-reference.webp` + `gj-qwen.webp` | 1141–1312 × 1199–1379 |
| `grok/` | Grok 娘 | 同上 | `gj-grok.webp`（只有立绘） | 1145–1199 × 1312–1374 |
| `gemini/` | Gemini 娘 | 同上 | `gj-gemini.webp`（只有立绘） | 1148–1206 × 1305–1371 |
| `claude-sketch/` | Claude 大小姐 | 铅笔素描 + 单色水彩 | `pixel/speaking.png`（bust_talking）+ 该状态对应的 `pixel/<state>.png` | 1254×1254 |
| `kimi/` | Kimi 娘 | 清爽动漫插画 | `gj-kimi-reference.webp` + `gj-kimi.webp` | 1145–1312 × 1199–1374 |

接口同第一批：`POST /v1/images/edits`，模型 `gpt-image-2.5-sunburst`，`size=1024x1024 quality=high background=transparent output_format=png`，prompt 用 `--form-string`（第一批踩过 `-F` 把分号后面吃掉的坑）。返回尺寸不是 1024²，是 1024×1536 到 1312×1199 之间的各种竖版，没有缩放。参考图 webp 先 `sips -s format png` 转成 PNG 再传。

## 提示词

每张 = **角色段** + **State 句**（和第一批完全同一套，见上「watercolor」节）+ **框架段**。同一组 5 张只换 State 句。

框架段（bust，6 组通用）：

> Render as clean modern anime illustration, soft cel shading, subtle rim light. Bust portrait from the top of the head to just below the chest, facing the viewer, centred and filling the frame. Transparent background. No text, letters, logos or watermark.

框架段（chibi，`deepseek-chibi`）：

> Render as a cute chibi (two-heads-tall, big head, big sparkling eyes, tiny body) in clean modern anime style with soft cel shading. Show the whole chibi from head to toe, facing the viewer, centred and filling the frame. Transparent background. No text, letters, logos or watermark.

框架段（sketch，`claude-sketch`）：

> Render as a hand-drawn pencil sketch: loose graphite line art with visible construction strokes and hatching, tinted with a single-colour translucent watercolour wash in warm orange-sepia only (no other colours), white paper left showing through. Bust portrait from the top of the hair to just below the chest, framed like the first attached image, facing the viewer. Transparent background. No text, letters, logos or watermark.

角色段：

- `deepseek` / `deepseek-chibi`（同一段）：
  > The attached images are the approved character design of DeepSeek-chan, the whale maid girl (a three-view reference sheet and a full-body illustration). Keep her identity exactly: very long, wavy deep-blue hair that fades to a lighter sky-blue at the tips, with a single curly ahoge on top; big bright blue eyes; small grey-blue whale-fin shaped ears on the sides of her head instead of human ears; a white frilled maid headdress with a small light-blue ribbon bow on her right side; a navy-blue long-sleeved maid dress with puffed shoulders and white frilled cuffs, a white frilled bib collar with a navy ribbon bow and small gold buttons down the front, and a white frilled apron with a tiny blue whale emblem. Palette strictly blue and white. Her big blue whale tail may peek in at the edge of the frame.
- `gpt`：
  > The attached images are the approved character design of GPT-chan, the white dragon girl (a three-view reference sheet and a full-body illustration). Keep her identity exactly: very long, flowing, slightly messy pure-white hair with a single ahoge; pale lavender-grey eyes with a calm, slightly aloof expression; pointed elf ears; two curved pale-ivory dragon horns on her head; a white hexagonal knot-shaped hair ornament with a dangling silver tassel on her left side; a white sleeveless high-collar gown with a thin silver chain and small tassels, white detached frilled sleeves and silver bracelets; large pale white dragon wings behind her shoulders. Palette entirely white, silver and the faintest lavender.
- `qwen`：
  > The attached images are the approved character design of Qwen-chan (a three-view reference sheet and a full-body illustration). Keep her identity exactly: very long, wavy periwinkle lavender-blue hair with a braided crown running around the head and a thin braid at the front; soft violet eyes; a small round indigo pillbox hat tilted on her head decorated with a white hexagonal emblem and white ribbon tails; gold drop earrings; a white Chinese-style stand-collar dress with gold frog buttons and a faint floral pattern, draped with a dark indigo shawl with gold trim and a Greek-key meander border, tied with a lavender ribbon and a round hexagonal emblem brooch at the chest; a folding fan in one hand. Palette indigo, violet, white and gold.
- `grok`：
  > The attached image is the approved character design of Grok-chan (a full-body illustration). Keep her identity exactly: long, voluminous blonde hair in high twin tails tied with big black ribbon bows, a single ahoge, a tiny black-and-gold crown-shaped tiara on top of her head and a small black bat-wing ornament on one side; bright blue eyes; a playful grin showing small fangs; pointed ears; a black gothic dress with a high stand-up collar lined in dark red, a black ribbon bow at the chest with a gold circle emblem, gold trim, thin gold chains and gold circular-X emblems on the dress, with white frilled sleeve cuffs. Palette black, gold, dark red and blonde. Do not draw the axe.
- `gemini`：
  > The attached image is the approved character design of Gemini-chan (a full-body illustration). Keep her identity exactly: very long, flowing, voluminous hair with a gradient from deep blue at the roots through violet to pink at the tips, styled with two small hair buns on top, a single ahoge and fluffy blue-violet cat ears; heterochromatic eyes - her left eye is pink-magenta and her right eye is golden amber; a large iridescent four-pointed star hair ornament on her right side; a dark navy sleeveless high-collar crop top decorated with small iridescent four-pointed stars, white wide detached sleeves with violet trim; several small iridescent four-pointed sparkle stars floating around her. Palette blue, violet, pink and iridescent pearl.
- `kimi`：
  > The attached images are the approved character design of Kimi-chan (a three-view reference sheet and a full-body illustration). Keep her identity exactly: very long, wavy silver-white hair with a faint pale-lavender tint; pale lavender-grey eyes with a calm expression; a white frilled maid headdress with a small black triangular prism ornament that refracts a rainbow beam and a navy ribbon bow bearing a tiny letter K on her right side; a white high-collar blouse with a frilled bib and a navy ribbon bow at the collar, under a navy-blue capelet dotted with tiny stars and a crescent moon, with silver buttons; small silver chains with musical-note charms at the waist. Palette navy, white, silver and lavender with a thin rainbow accent.
- `claude-sketch`（角色句照抄第一批）：
  > The first attached image is the approved character design of Claude-chan (a bust portrait); the second shows the target expression and pose. Keep her identity exactly: long, flowing, slightly wavy bright orange hair with side-swept bangs; amber-brown eyes; a large orange many-petaled flower hair ornament with a pearl centre on the left side of her head, with a black ribbon bow and white lace ribbon tails behind it; small gold drop earrings; a white Victorian blouse with a ruffled high collar and a frilled jabot; a black bow at the collar pinned with a small orange flower brooch with a pearl centre; a fitted black vest with thin gold trim, and white puffed sleeves.

## 核验与重生成

每张核过 `sips -g hasAlpha`（全部 yes）和尺寸（最小边 ≥ 1024）。总共 43 次调用：40 张正式 + 1 次接口失败重试 + 2 次重生成。

- `qwen/listening` 第一次接口返回 `Client Closed Request`（空 body，不是模型问题），重试一次成功。
- `qwen/speaking` 第一次下边缘被裁成圆角（像贴纸），重生成一次换成了平底的；第一版没留。
- `gemini/speaking` 同样原因重生成一次；第一版没留。
- 其余 38 张一次成。
- 没有出现文字、角色跑偏或不透明。`kimi` 的蝴蝶结上有字母 K、发饰是三棱镜 + 彩虹，这是原设定的一部分，不算「文字」，保留了。
- `claude-sketch` 实际出来的更像橙棕单色的水彩线稿，铅笔颗粒感不重；没重生成，用户觉得不够「素描」再调措辞。
- `deepseek/idle` 是微张嘴的笑，不是闭嘴微笑，和 chibi 组一致，保留。

usage 合计（43 次调用）：input 118,298 + output 72,765 = 191,063 token。

## 署名（第二批）

- `deepseek/`、`deepseek-chibi/`：基于 aigengtu.com 本站自制的 DeepSeek 娘三视图生成（站方原话：「标『本站自有』的条目（DeepSeek 娘 7 套三视图）由本站制作，可自由下载用于个人二创」）；第二张参考是 ZipZipPipe 二创立绘（CC BY-NC-SA 4.0）；DeepSeek 娘原角色由上善无形设计。萌娘百科词条（CC BY-NC-SA 3.0）只用作设定文字参考，两套设定的差异（白发 vs 蓝发）见 `_refs/deepseek/source.md`。
- `gpt/`、`qwen/`、`grok/`、`gemini/`、`kimi/`：角色设定与参考图均为 ZipZipPipe（B 站 space.bilibili.com/4168597），CC BY-NC-SA 4.0，经 aigengtu.com 转载；原帖链接在各组 `source.md`。
- `claude-sketch/`：同第一批署名（Claude大小姐的日记本 · ZipZipPipe · lmy414/ai-chan-sheets · CC BY-NC-SA 4.0）。
- 以上全部是非商用授权，且除 Claude 外都是第三方同人形象：只做内测可选项，上架商用前要换图。
