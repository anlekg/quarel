#!/usr/bin/env bash
# Builds the Windows installers of both Quarel servers, from Linux or macOS:
#   packaging/windows/build.sh [version]      (or: make windows)
# Needs Go, NSIS (makensis) and curl; the official livekit-server for
# Windows is downloaded once and checked against its published checksum.
# Output: dist/windows/Quarel-Serveur-Setup-<version>.exe, Quarel-Identite-Setup-<version>.exe
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
HERE=$ROOT/packaging/windows
VERSION=${1:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}
# Windows wants a numeric x.y.z.w version: taken from a v1.2.3 tag, else 0.0.0.0.
NUMVER=$(sed -nE 's/^v?([0-9]+)\.([0-9]+)\.([0-9]+).*/\1.\2.\3.0/p' <<<"$VERSION"); NUMVER=${NUMVER:-0.0.0.0}
LIVEKIT_VERSION=1.13.7
LIVEKIT_SHA256=e539e7d2f75807b9c9202cd2a0bf2cb3d52fc4c52978a6953e0f47bc339fe77f
OUT=$ROOT/dist/windows
CACHE=$HERE/cache
mkdir -p "$OUT" "$CACHE"
command -v makensis >/dev/null || { echo "makensis introuvable : installez NSIS (apt install nsis)"; exit 1; }

# livekit-server.exe (voice), official build.
ZIP=$CACHE/livekit_${LIVEKIT_VERSION}_windows_amd64.zip
if [ ! -f "$ZIP" ]; then
  curl -fsSL -o "$ZIP.part" "https://github.com/livekit/livekit/releases/download/v${LIVEKIT_VERSION}/livekit_${LIVEKIT_VERSION}_windows_amd64.zip"
  mv "$ZIP.part" "$ZIP"
fi
echo "$LIVEKIT_SHA256  $ZIP" | sha256sum -c --quiet - || { echo "empreinte de LiveKit incorrecte"; rm -f "$ZIP"; exit 1; }

build() { # build <cmd> <exe> <description> <icon>
  local dir=$ROOT/cmd/$1
  (cd "$dir" && go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --icon "$HERE/$4" --manifest gui \
    --product-name "Quarel" --file-description "$3" --product-version "$NUMVER" --file-version "$NUMVER" \
    --copyright "Les auteurs de Quarel — Apache-2.0" --original-filename "$2" --out rsrc)
  (cd "$ROOT" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
    -ldflags "-H=windowsgui -s -w -X github.com/anlekg/quarel/internal/backup.Release=$VERSION" -o "$STAGE/$2" "./cmd/$1")
  rm -f "$dir"/rsrc_windows_*.syso
}

package() { # package <stage> <name> <exe> <appid> <dirname> <datadir> <icon> <outfile> [LIVEKIT]
  cp "$ROOT/LICENSE" "$1/LICENSE.txt"; cp "$ROOT/NOTICE" "$1/NOTICE.txt"
  local extra=()
  [ "${9:-}" = LIVEKIT ] && extra=(-DLIVEKIT)
  makensis -V2 -INPUTCHARSET UTF8 "-DNAME=$2" "-DEXE=$3" "-DAPPID=$4" "-DDIRNAME=$5" "-DDATADIR=$6" "-DICON=$HERE/$7" \
    "-DVERSION=$VERSION" "-DNUMVER=$NUMVER" "-DSRC=$1" "-DOUTFILE=$OUT/$8" "${extra[@]}" "$HERE/installer.nsi"
}

STAGE=$(mktemp -d); trap 'rm -rf "$STAGE"' EXIT
build quarel-server quarel-server.exe "Quarel — serveur communautaire" quarel-server.ico
unzip -q -o -j "$ZIP" livekit-server.exe -d "$STAGE"
unzip -q -o -j "$ZIP" LICENSE -d "$STAGE" 2>/dev/null && mv "$STAGE/LICENSE" "$STAGE/LICENSE-livekit.txt" || cp "$HERE/LICENSE-livekit.txt" "$STAGE/"
package "$STAGE" "Quarel — serveur communautaire" quarel-server.exe QuarelServer "Serveur communautaire" Serveur quarel-server.ico "Quarel-Serveur-Setup-$VERSION.exe" LIVEKIT

STAGE2=$(mktemp -d); trap 'rm -rf "$STAGE" "$STAGE2"' EXIT
STAGE=$STAGE2 build quarel-identity quarel-identity.exe "Quarel — service d'identité" quarel-identity.ico
package "$STAGE2" "Quarel — service d'identité" quarel-identity.exe QuarelIdentity "Service d'identite" Identite quarel-identity.ico "Quarel-Identite-Setup-$VERSION.exe"
ls -la "$OUT"
