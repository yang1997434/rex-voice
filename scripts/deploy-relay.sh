#!/usr/bin/env bash
# 要用户明文授权「部署 rex-voice 中继」才能跑。nginx 片段不在本脚本里，改 nginx 由主会话手动做、先备份。
set -euo pipefail

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
