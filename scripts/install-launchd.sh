#!/bin/sh
# Build rex-voiced into ~/.local/bin and (re)load it as a LaunchAgent; `install-launchd.sh uninstall` removes it.
# REX_VOICE_RELAY=ws://127.0.0.1:8790/_voice/ws install-launchd.sh points the service at another relay.
set -eu
label=com.maruiao.rex-voiced
domain="gui/$(id -u)"
dst="$HOME/Library/LaunchAgents/$label.plist"
cd "$(dirname "$0")/.."

launchctl bootout "$domain/$label" 2>/dev/null || true
if [ "${1:-}" = uninstall ]; then
  rm -f "$dst"
  echo "uninstalled $label"
  exit 0
fi

mkdir -p "$HOME/.local/bin" "$HOME/Library/LaunchAgents"
mkdir -p -m 700 "$HOME/Library/Logs/rex-voice"
go build -ldflags "-X main.version=$(git rev-parse --short HEAD)" -o "$HOME/.local/bin/rex-voiced" ./cmd/rex-voiced
cp scripts/$label.plist "$dst"
if [ -n "${REX_VOICE_RELAY:-}" ]; then
  plutil -insert EnvironmentVariables.REX_VOICE_RELAY -string "$REX_VOICE_RELAY" "$dst"
fi
plutil -lint "$dst"
launchctl bootstrap "$domain" "$dst"
echo "installed $label; log: ~/Library/Logs/rex-voice/rex-voiced.log"
