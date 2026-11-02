#!/usr/bin/env bash
# 要用户明文授权「部署 rex-voice 中继」才能跑。nginx 片段不在本脚本里，改 nginx 由主会话手动做、先备份。
set -euo pipefail

# APNs 推送配置（本脚本不碰，首次在 ccs-maruiao 上手动放，root 所有、0600）：
#   /etc/rex-voice/relay.env   三行 APNS_KEY_PATH=... APNS_KEY_ID=... APNS_TEAM_ID=KAA6GJ59XS；文件不在中继照常跑、只是不推。
#                              EnvironmentFile 由 systemd 以 root 读，0600 没问题。
#                              可选第四行 RELAY_INVITES=码1,码2（邀请码，每个 [A-Za-z0-9_-]{8,64}）：写了就只放行带对码的连接，
#                              有一个码格式不对中继起不来；不写谁都能连。改完 systemctl restart，已配对的手机和 Mac 要 pair -force -invite 重扫。
#   /etc/rex-voice/AuthKey.p8  ASC 下载的 APNs key。DynamicUser 每次启动分配临时 UID，没法把文件 chown 给它，
#                              0600 root 的文件服务读不到；用 LoadCredential 让 systemd 以 root 读、拷到
#                              /run/credentials/rex-voice-relay.service/ 只给本服务读：
#     systemctl edit rex-voice-relay   # 写 drop-in，内容两行：
#       [Service]
#       LoadCredential=apns.p8:/etc/rex-voice/AuthKey.p8
#     relay.env 里写 APNS_KEY_PATH=/run/credentials/rex-voice-relay.service/apns.p8
#     （env 文件里不展开 %d，只能写全路径）。LoadCredential 的源文件不在服务会起不来，所以 .p8 放好之后才加 drop-in。
# 注意：下面只在服务器上还没有 unit 文件时才拷 scripts/rex-voice-relay.service；已装过的机器要手动拷一次新 unit
#       （新增了 EnvironmentFile 行）再 daemon-reload，否则 relay.env 不生效。

HOST=ccs-maruiao
cd "$(dirname "$0")/.."

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o dist/rex-voice-relay ./cmd/rex-voice-relay

ssh "$HOST" 'mkdir -p /opt/rex-voice'
scp dist/rex-voice-relay "$HOST:/opt/rex-voice/rex-voice-relay.new"
ssh "$HOST" 'chmod 755 /opt/rex-voice/rex-voice-relay.new && mv /opt/rex-voice/rex-voice-relay.new /opt/rex-voice/rex-voice-relay'

if ! ssh "$HOST" 'test -f /etc/systemd/system/rex-voice-relay.service'; then
  scp scripts/rex-voice-relay.service "$HOST:/etc/systemd/system/rex-voice-relay.service"
  ssh "$HOST" 'systemctl daemon-reload && systemctl enable rex-voice-relay'
fi

ssh "$HOST" 'systemctl restart rex-voice-relay'
sleep 2
curl -fsS -A rex-voice-check https://r.maruiao.top/_voice/healthz
echo
