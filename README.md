# rex-voice：用手机打电话遥控 Mac 上的 Claude

在 iPhone 上开 RexVoice app，像打电话一样跟语音助手说话。它能看这台 Mac 上 Rex 各个终端窗格和 Claude Code 会话在干什么，用一两句话汇报进展；你确认后，它还能替你往某个窗格里发一句指令。说「挂断吧」它就挂。

Mac 上跑的是后台服务 `rex-voiced`。手机和 Mac 之间经一台中继转发，中继只转加密数据，看不到内容。

## 要求

- macOS 14 或更新，Apple 芯片或 Intel 都行
- 装好 Rex，并且打开过一次（`rex-voiced` 要用它的 `rex` 命令）
- Claude Code（`claude`），在 Rex 的窗格里跑
- 一个 OpenAI API key，账户要能用 Realtime 模型（默认 `gpt-realtime-2.1`），按用量付费
- 一台 iPhone，装 RexVoice app（TestFlight 内测，找发给你压缩包的人要邀请）

## 安装（四步）

1. **装程序**：解压收到的 `rex-voiced-<版本>-macos.tar.gz`，进目录运行

   ```sh
   tar xzf rex-voiced-*-macos.tar.gz
   cd rex-voiced-*-macos
   ./install.sh
   ```

   程序装到 `~/.local/bin/rex-voiced`。如果提示 `~/.local/bin` 不在 PATH 里，照提示把那行加进 `~/.zshrc`，重开终端。

2. **填 key 和称呼**：

   ```sh
   rex-voiced setup
   ```

   先粘贴 OpenAI key（输入时不显示，正常），再输入希望助手怎么称呼你（直接回车用系统里的名字）。key 存进 macOS 钥匙串，不写进任何文件。

3. **配对手机**：

   ```sh
   rex-voiced pair
   ```

   终端里会出现一个二维码，打开 RexVoice app 扫它。一台 Mac 只配一台手机。

4. **装成后台服务**：

   ```sh
   rex-voiced install
   ```

   装好后它会在你每次登录时自动运行，崩了也会自己重启。现在打开 app 点「通话」就能说话了。

## 能说什么

- 「现在都有哪些窗格在跑？」
- 「geo 那边 Claude 做到哪了？」
- 「有没有卡住在等我的？」
- 「jobs 那个窗格屏幕上现在显示什么？」
- 「给 metis 那个 Claude 发：先跑一遍测试」——它会先把原文逐字念一遍，你说「对」「发」才真发
- 「先这样，挂断吧」

助手只能读窗格、读 Claude 会话、往窗格里发文字，别的事（开 app、改文件）做不了。

## 安全

- **OpenAI key 只在你的 Mac 上**，存在钥匙串里（服务名 `rex-voice`，账户 `openai`）。手机和中继都拿不到。
- **端到端加密**：手机和 Mac 配对时交换密钥，之后所有声音和文字都在两端加解密，中继服务器只看到乱码。
- 你的声音和窗格里的内容会发给 OpenAI 做语音识别和回答，这一点和直接用 ChatGPT 一样。
- **禁发词**：发往窗格的文字里只要有「生产、部署、上线、发布、回滚、迁移、deploy、rm -rf、git push、sudo」，一律拒发，让你回 Mac 手打。想多加几个词，写进配置文件的 `forbid_extra`（见下）。
- 每次发出和被拒的指令都记在 `~/Library/Logs/rex-voice/audit.log`。

## 配置文件

`~/Library/Application Support/rex-voice/config.json`，没写的字段用默认值：

```json
{
  "name": "小王",
  "model": "gpt-realtime-2.1",
  "voice": "marin",
  "relay": "wss://r.maruiao.top/_voice/ws",
  "project_aliases": { "shop": "网店后台，Next.js" },
  "forbid_extra": ["drop table"]
}
```

- `name`：助手怎么称呼你。
- `model` / `voice`：OpenAI 的 Realtime 模型和音色。
- `project_aliases`：给项目名加一句说明，助手听你说项目时更容易对上号。正在跑的 Claude 目录名和 Rex 会话名它每次通话会自己读，不用写。
- `forbid_extra`：在内置禁发词之外再禁的词，不分大小写。

改完运行 `launchctl kickstart -k gui/$(id -u)/com.maruiao.rex-voiced` 让服务重读。

## 卸载

```sh
rex-voiced uninstall                                     # 停掉并删除后台服务
security delete-generic-password -s rex-voice -a openai  # 删掉钥匙串里的 key
rm -rf ~/Library/Application\ Support/rex-voice ~/Library/Logs/rex-voice ~/.local/bin/rex-voiced
```

## 常见问题

**打不通 / app 一直显示连接中？** 看日志 `~/Library/Logs/rex-voice/rex-voiced.log`。正常启动会有 `openai key: keychain` 和 `connected relay` 两行。查服务在不在跑：`launchctl print gui/$(id -u)/com.maruiao.rex-voiced | grep state`。

**日志里说「没有 OpenAI key」？** 重新运行 `rex-voiced setup`。

**日志里说「还没配对」，或者换了手机？** 运行 `rex-voiced pair -force` 重新扫码（旧手机随即失效），再 `rex-voiced install`。

**换模型？** 改 `config.json` 里的 `model`，再按上面的办法重启服务。也可以临时前台试：先 `rex-voiced uninstall`，再 `rex-voiced serve -model <模型名>`。

**升级？** 照安装第 1 步装新版本，再运行一次 `rex-voiced install`。key、配置和配对都保留。

**花多少钱？** 每轮回答的用量记在 `~/Library/Logs/rex-voice/usage.log`（含估算美元）。

**不用手机先试试？** `rex-voiced chat` 是文字版，打字问它同样的问题。
