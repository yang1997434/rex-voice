#!/bin/sh
# Build a universal, Developer ID–signed and notarized rex-voiced, packed as dist/rex-voiced-<version>-macos.tar.gz
# with install.sh and README.md. Notarization needs the App Store Connect API key:
# env ASC_KEY_ID / ASC_ISSUER_ID / ASC_KEY_PATH, else flow-cred ASC_ADMIN_KEY_ID / ASC_ISSUER_ID / ASC_ADMIN_KEY_PATH.
# Bare binaries cannot be stapled, so the ticket stays online only (Gatekeeper checks it when the file is first run).
set -eu
version=${1:?usage: release-mac.sh <version>}
identity=${SIGN_IDENTITY:-"Developer ID Application: MD5 CAPITAL PTE. LTD. (KAA6GJ59XS)"}
cd "$(dirname "$0")/.."

key_id=${ASC_KEY_ID:-$(flow-cred ASC_ADMIN_KEY_ID)}
issuer=${ASC_ISSUER_ID:-$(flow-cred ASC_ISSUER_ID)}
key_path=${ASC_KEY_PATH:-$(flow-cred ASC_ADMIN_KEY_PATH)}
case "$key_path" in "~/"*) key_path="$HOME/${key_path#\~/}" ;; esac
[ -f "$key_path" ] || { echo "ASC key 文件不存在: $key_path" >&2; exit 1; }

name="rex-voiced-$version-macos"
work="dist/$name"
rm -rf "$work" "dist/$name.tar.gz" "dist/$name-notarize.zip"
mkdir -p "$work"
for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" -o "dist/rex-voiced-$arch" ./cmd/rex-voiced
done
lipo -create -output "$work/rex-voiced" dist/rex-voiced-arm64 dist/rex-voiced-amd64
rm -f dist/rex-voiced-arm64 dist/rex-voiced-amd64
codesign --force --sign "$identity" --options runtime --timestamp "$work/rex-voiced"
codesign --verify --strict --verbose=2 "$work/rex-voiced"

# notarytool takes zip / pkg / dmg, not tar.gz: submit the signed binary zipped, ship the same binary in the tarball.
ditto -c -k "$work/rex-voiced" "dist/$name-notarize.zip"
xcrun notarytool submit "dist/$name-notarize.zip" --key "$key_path" --key-id "$key_id" --issuer "$issuer" --wait --timeout 30m \
  | tee "dist/$name-notarize.log"
grep -q "status: Accepted" "dist/$name-notarize.log" || { echo "公证没通过，见 dist/$name-notarize.log（xcrun notarytool log <id> 看原因）" >&2; exit 1; }
rm -f "dist/$name-notarize.zip"

cp scripts/install.sh README.md "$work/"
chmod 755 "$work/install.sh"
tar -C dist -czf "dist/$name.tar.gz" "$name"
echo "dist/$name.tar.gz"
shasum -a 256 "dist/$name.tar.gz" "$work/rex-voiced"
