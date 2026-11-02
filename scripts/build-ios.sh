#!/bin/bash
# Archive RexVoice and upload it to TestFlight via cloud signing.
#   scripts/build-ios.sh <version, e.g. 0.1.0> -m "what changed"   archive + upload, append docs/releases.md
#   scripts/build-ios.sh <version> --archive-only                   archive only, nothing uploaded
set -euo pipefail
# Homebrew rsync on PATH makes -exportArchive fail; system tools first.
export PATH="/usr/bin:/bin:/usr/sbin:/sbin:$PATH"

usage() { echo "用法: scripts/build-ios.sh <版本号 x.y.z> [--archive-only] [-m 改了什么]" >&2; exit 2; }
die() { echo "build-ios: $*" >&2; exit 1; }

version="" archive_only=0 note=""
while [ $# -gt 0 ]; do
	case "$1" in
	--archive-only) archive_only=1 ;;
	-m) [ $# -ge 2 ] || usage; note="$2"; shift ;;
	-*) usage ;;
	*) [ -z "$version" ] || usage; version="$1" ;;
	esac
	shift
done
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
[ "$archive_only" = 1 ] || [ -n "$note" ] || die "上传要带 -m「改了什么」，会写进 docs/releases.md"

repo=$(cd "$(dirname "$0")/.." && pwd)
team=KAA6GJ59XS
key_id=$(flow-cred ASC_ADMIN_KEY_ID)
issuer=$(flow-cred ASC_ISSUER_ID)
key_path=$(flow-cred ASC_ADMIN_KEY_PATH)
key_path="${key_path/#\~/$HOME}"
[ -n "$key_id" ] && [ -n "$issuer" ] || die "flow-cred 没取到 ASC_ADMIN_KEY_ID / ASC_ISSUER_ID"
[ -f "$key_path" ] || die "ASC key 文件不存在：$key_path"
auth=(-allowProvisioningUpdates -authenticationKeyPath "$key_path" -authenticationKeyID "$key_id" -authenticationKeyIssuerID "$issuer")

# A locked login keychain makes codesign hang inside archive.
if pw=$(flow-cred LOGIN_KEYCHAIN_PASSWORD 2>/dev/null) && [ -n "$pw" ]; then
	security unlock-keychain -p "$pw" "$HOME/Library/Keychains/login.keychain-db"
else
	echo "build-ios: 警告：flow-cred 没有 LOGIN_KEYCHAIN_PASSWORD，不解锁登录钥匙串；钥匙串锁着时 archive 会卡到超时" >&2
fi
unset pw

(cd "$repo/ios/RexVoice" && xcodegen generate --quiet) || die "xcodegen generate 失败"

build=$(date -u +%Y%m%d%H%M)
work=$(mktemp -d "${TMPDIR:-/tmp}/rexvoice-ios.XXXXXX")
archive="$work/RexVoice.xcarchive"
echo "build-ios: 版本 $version build ${build}，工作目录 $work"

if ! timeout -k 10 900 xcodebuild archive -project "$repo/ios/RexVoice/RexVoice.xcodeproj" -scheme RexVoice \
	-destination 'generic/platform=iOS' -archivePath "$archive" "${auth[@]}" \
	MARKETING_VERSION="$version" CURRENT_PROJECT_VERSION="$build" >"$work/archive.log" 2>&1; then
	tail -n 60 "$work/archive.log" >&2
	die "archive 失败（退出码 124 是超时，多半是钥匙串锁着），完整日志 $work/archive.log"
fi
app="$archive/Products/Applications/RexVoice.app"
profile=$(security cms -D -i "$app/embedded.mobileprovision" | plutil -extract Name raw -)
echo "build-ios: archive 好了：$archive"
echo "build-ios: 描述文件：$profile"

if [ "$archive_only" = 1 ]; then
	echo "build-ios: --archive-only，不上传"
	exit 0
fi

cat >"$work/ExportOptions.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key><string>app-store-connect</string>
	<key>destination</key><string>upload</string>
	<key>teamID</key><string>$team</string>
	<key>signingStyle</key><string>automatic</string>
	<key>uploadSymbols</key><true/>
</dict>
</plist>
PLIST

if ! timeout -k 10 1800 xcodebuild -exportArchive -archivePath "$archive" -exportOptionsPlist "$work/ExportOptions.plist" \
	-exportPath "$work/export" "${auth[@]}" >"$work/export.log" 2>&1; then
	tail -n 60 "$work/export.log" >&2
	die "导出 / 上传失败，完整日志 $work/export.log"
fi

commit=$(git -C "$repo" rev-parse --short HEAD)
[ -z "$(git -C "$repo" status --porcelain)" ] || commit="$commit-dirty"
# Insert after the last table row, not at EOF: sections below the table (真机反馈) would otherwise swallow the row.
row="$(printf '| %s | %s | %s | %s | %s |' "$(date +%F)" "$version" "$build" "$commit" "${note//|/\\|}")"
awk -v row="$row" '{ lines[NR]=$0; if ($0 ~ /^\| 20[0-9][0-9]-/) last=NR } END { for (i=1;i<=NR;i++) { print lines[i]; if (i==last) print row } }' "$repo/docs/releases.md" >"$repo/docs/releases.md.tmp" && mv "$repo/docs/releases.md.tmp" "$repo/docs/releases.md"
echo "build-ios: 上传完成，已记进 docs/releases.md；TestFlight 一般 1-30 分钟后可见"
echo "build-ios: Delivery UUID 在这些目录的日志里："
ls -dt "$(getconf DARWIN_USER_TEMP_DIR)"RexVoice_*.xcdistributionlogs 2>/dev/null | head -n 3 || echo "  （没找到 RexVoice_*.xcdistributionlogs）"
