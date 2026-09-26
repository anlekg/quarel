#!/usr/bin/env bash
# Builds and publishes the Windows installers of both servers to quarel.app/telechargements:
#   packaging/windows/release.sh 0.2.0      (or: make windows-release VERSION=0.2.0)
# Writes SHA256SUMS next to them, keeps the 3 latest versions, and records the
# version in packaging/windows/VERSION (read by the site: rebuild it afterwards).
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
VERSION=${1:?usage : release.sh <version, ex. 0.2.0>}
[[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version x.y.z attendue"; exit 1; }
DEST=${QUAREL_DOWNLOADS_DIR:?QUAREL_DOWNLOADS_DIR : dossier servi à quarel.app/telechargements (voir local.mk)}

"$ROOT/packaging/windows/build.sh" "$VERSION"
mkdir -p "$DEST"
for f in Quarel-Serveur-Setup-$VERSION.exe Quarel-Identite-Setup-$VERSION.exe; do
  cp "$ROOT/dist/windows/$f" "$DEST/.$f.tmp" && mv "$DEST/.$f.tmp" "$DEST/$f"
done
# Old versions: keep the 3 latest.
ls "$DEST" | sed -nE 's/^Quarel-(Serveur|Identite)-Setup-([0-9.]+)\.exe$/\2/p' | sort -u -V -r | tail -n +4 |
  while read -r old; do rm -f "$DEST"/Quarel-*-Setup-"$old".exe; done
(cd "$DEST" && sha256sum Quarel-*-Setup-*.exe > .SHA256SUMS.tmp && mv .SHA256SUMS.tmp SHA256SUMS)
echo "$VERSION" > "$ROOT/packaging/windows/VERSION"
echo "Installateurs $VERSION publiés dans $DEST"
