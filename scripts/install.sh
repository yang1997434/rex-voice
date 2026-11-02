#!/bin/sh
# Install rex-voiced from an unpacked release tarball into ~/.local/bin, then point at `rex-voiced setup`.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
src="$here/rex-voiced"
dst_dir="$HOME/.local/bin"
if [ ! -f "$src" ]; then
  echo "找不到 $src：请在解压出来的 rex-voiced 目录里运行 ./install.sh" >&2
  exit 1
fi
mkdir -p "$dst_dir"
cp "$src" "$dst_dir/rex-voiced"
chmod 755 "$dst_dir/rex-voiced"
# Downloaded files carry the quarantine flag; the binary is notarized, this only saves a Gatekeeper prompt.
xattr -d com.apple.quarantine "$dst_dir/rex-voiced" 2>/dev/null || true
echo "已装到 $dst_dir/rex-voiced（$("$dst_dir/rex-voiced" version)）"

case ":$PATH:" in
  *":$dst_dir:"*) cmd=rex-voiced ;;
  *)
    cmd="$dst_dir/rex-voiced"
    echo
    echo "$dst_dir 不在 PATH 里。把下面这行加进 ~/.zshrc 后重开终端，就能直接敲 rex-voiced："
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    ;;
esac
if launchctl print "gui/$(id -u)/com.maruiao.rex-voiced" >/dev/null 2>&1; then
  echo
  echo "后台服务已经装过：运行 $cmd install 让它换成新版本。"
  exit 0
fi
echo
echo "下一步："
echo "  1. $cmd setup     填 OpenAI key 和称呼"
echo "  2. $cmd pair      用 RexVoice app 扫二维码"
echo "  3. $cmd install   装成登录后自动运行的后台服务"
