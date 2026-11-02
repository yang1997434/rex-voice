# Claude 形象素材（4 组 × 5 状态）

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
