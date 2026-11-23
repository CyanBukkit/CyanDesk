#!/bin/sh
# Builds cyandesk-server-<version>-linux-amd64.tar.gz from this checkout.
# Go modules and npm packages are not vendored. The archive is not an offline build.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
VER=$(awk -F\" '/cyandesk [0-9]/{print $2; exit}' "$ROOT/cmd/cyandesk/main.go" || true)
VER=${VER:-0.1.4}
# version string is printed as a literal in main.go; read it directly
VER=$(sed -n 's/.*cyandesk \([0-9][0-9.]*\) protocol.*/\1/p' "$ROOT/cmd/cyandesk/main.go" | head -1)
NAME="cyandesk-server-${VER}-linux-amd64"
STAGE=$(mktemp -d)
DEST="$STAGE/$NAME"
mkdir -p "$DEST/bin" "$DEST/admin" "$DEST/config"

tar -C "$ROOT" \
  --exclude .git \
  --exclude gateway-rs/target \
  --exclude admin-web/node_modules \
  --exclude admin-web/dist \
  --exclude data \
  --exclude bin \
  --exclude '*.db' \
  -cf - . | tar -C "$DEST" -xf -

cp "$ROOT/bin/cyandesk" "$DEST/bin/cyandesk"
cp "$ROOT/bin/cyandesk-gateway" "$DEST/bin/cyandesk-gateway"
chmod 755 "$DEST/bin/cyandesk" "$DEST/bin/cyandesk-gateway" \
  "$DEST/deploy/binary-start.sh" "$DEST/deploy/binary-stop.sh" \
  "$DEST/deploy/binary-health.sh" "$DEST/deploy/binary-backup.sh" \
  "$DEST/deploy/install.sh" "$DEST/deploy/uninstall.sh" "$DEST/deploy/entrypoint.sh"
rm -rf "$DEST/admin"
cp -a "$ROOT/admin-web/dist" "$DEST/admin"
cp "$ROOT/deploy/使用说明.txt" "$DEST/使用说明.txt"
printf '%s\n' "$VER linux/amd64" >"$DEST/VERSION"
(
  cd "$DEST"
  find . -type f ! -name SHA256SUMS | sort | xargs sha256sum > SHA256SUMS
)
OUT="${1:-/opt/cursor/artifacts/cyandesk/$NAME.tar.gz}"
mkdir -p "$(dirname "$OUT")"
tar -C "$STAGE" -czf "$OUT" "$NAME"
sha256sum "$OUT" | tee "$OUT.sha256"
echo "wrote $OUT"
rm -rf "$STAGE"
