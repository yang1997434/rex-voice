# rex-voice 上手（发给朋友的版本）

用 iPhone 打电话遥控你 Mac 上的 Claude Code：问进度、发指令、答它的提问，Claude 卡住了手机会收到推送。

## 你需要

- 一台 Mac（macOS 14 以上，Apple 芯片或 Intel 都行），装好 Rex（向分享人要下载地址）并打开过一次，Claude Code 在 Rex 的窗格里跑。
- 一个语音模型的 API key，五家选一：OpenAI（Realtime）、通义千问（国际站或国内站）、Gemini、Grok、MOSS-Live（只能聊天，看不了窗格、发不了指令）。按用量付费，填在手机 app 的设置里，只会发到你自己的 Mac。
- 一台 iPhone（iOS 26 以上）。
- 向分享人要两样：TestFlight 邀请（他在 App Store Connect 加你的 Apple ID 邮箱）、中继邀请码。

## 一、装手机 app

1. App Store 装 TestFlight。
2. 收到邀请邮件后点「View in TestFlight」，装 RexVoice。

## 二、装 Mac 端（四步，都在终端里）

1. 下载并安装：到 https://github.com/yang1997434/rex-voice/releases 下载最新的 `rex-voiced-<版本>-macos.tar.gz`，然后

   ```sh
   tar xzf ~/Downloads/rex-voiced-*-macos.tar.gz -C ~/Downloads
   cd ~/Downloads/rex-voiced-*-macos
   ./install.sh
   ```

   如果提示 `~/.local/bin` 不在 PATH 里，照提示把那行加进 `~/.zshrc`，重开终端。

2. 初始化：

   ```sh
   rex-voiced setup
   ```

   问 key 时直接回车跳过（key 填在手机里），再输入希望助手怎么称呼你。

3. 配对手机（把 `<邀请码>` 换成分享人给你的）：

   ```sh
   rex-voiced pair -invite <邀请码>
   ```

   终端出现二维码，打开 RexVoice，右上「配对」→ 扫码。app 显示「待机」就配好了。一台 Mac 只配一台手机。

4. 装成后台服务（登录自动跑、崩了自己重启）：

   ```sh
   rex-voiced install
   ```

## 三、手机上

1. 设置页：选供应商（Grok / 千问 / GPT / Gemini / MOSS），填那家的 key，选音色，可以填称呼。
2. 主屏右侧竖栏那个 logo 是当前供应商，长按转一圈可以换。
3. 按「开始通话」，直接说话：
   - 「现在有哪些窗格在跑？」「geo 那边做到哪了？」「有没有卡住的？」
   - 「在 xxx 会话新开一个 Claude」「把它改名叫 test」「关掉它」
   - 「给 xxx 那个 Claude 发：先跑一遍测试」——它会先逐字复述，你说「对」才真发
   - 「把那张截图发我」「把链接发我」
   - 「挂断吧」
4. Claude 弹问题而你没在通话：手机收到「xxx 的 Claude 在等你」。解锁后长按通知可以直接「选 1 / 选 2 / 选 3 / 输入回复」；或打开 app，主屏那张卡按选项就行。

## 常见问题

- **app 一直「连接中」或提示要邀请码**：Mac 上重新 `rex-voiced pair -force -invite <邀请码>` 再扫一次。
- **「Mac 不在线」**：Mac 上看 `~/Library/Logs/rex-voice/rex-voiced.log`，正常有 `connected relay`；没有就 `rex-voiced install` 重装服务。
- **升级**：下载新包重跑 `./install.sh`，再 `rex-voiced install`。key、配置、配对都保留。
- **安全**：key 只在你的手机和 Mac 之间走；手机和 Mac 之间端到端加密，中继只转发乱码；你的声音和窗格内容会发给你选的那家模型。
