#!/bin/sh
# Copies the data directory. The archive contains the database and key files.
# This script does not print those files.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DATA="${DATA:-$ROOT/data}"
if [ ! -d "$DATA" ]; then
  echo "no data directory: $DATA" >&2
  exit 1
fi
out="${1:-$ROOT/cyandesk-backup-$(date +%Y%m%d%H%M%S).tar.gz}"
tar -C "$(dirname "$DATA")" -czf "$out" "$(basename "$DATA")"
echo "backup: $out"
echo "Keep this file with the server. Do not mail it or paste it into a chat."
