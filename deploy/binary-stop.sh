#!/bin/sh
# Stops the binaries started by binary-start.sh. The data directory stays.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DATA="${DATA:-$ROOT/data}"
for name in gateway control; do
  pidfile="$DATA/$name.pid"
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    kill "$pid" 2>/dev/null || true
    rm -f "$pidfile"
    echo "stopped $name"
  fi
done
echo "Data directory kept: $DATA"
