#!/bin/sh
# Starts the already built server binaries. Edit the variables below.
# The cyandesk program reads flags, not this file.
set -eu
umask 077
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
DATA="${DATA:-$ROOT/data}"
LISTEN="${LISTEN:-0.0.0.0:13111}"
INTERNAL="${INTERNAL:-127.0.0.1:13112}"
GATEWAY="${GATEWAY:-0.0.0.0:13110}"
ADMIN="${ADMIN:-$ROOT/admin}"
# Space-separated names for the first certificate. Example: CERT_HOSTS="203.0.113.10 desk.example.com"
CERT_HOSTS="${CERT_HOSTS:-}"

mkdir -p "$DATA"
cd "$ROOT"
hosts=""
for name in $CERT_HOSTS; do
  hosts="$hosts --cert-host $name"
done
# shellcheck disable=SC2086
# shellcheck disable=SC2086
nohup "$ROOT/bin/cyandesk" control --data "$DATA" --listen "$LISTEN" --internal "$INTERNAL" --admin "$ADMIN" $hosts \
  >>"$DATA/control.log" 2>&1 &
echo $! >"$DATA/control.pid"
i=0
while [ "$i" -lt 50 ]; do
  if [ -f "$DATA/internal.token" ]; then
    break
  fi
  i=$((i + 1))
  sleep 0.1
done
if [ ! -f "$DATA/internal.token" ]; then
  echo "control did not create the internal token file; see $DATA/control.log" >&2
  exit 1
fi
nohup "$ROOT/bin/cyandesk-gateway" --listen "$GATEWAY" --internal-url "http://$INTERNAL" --internal-token-file "$DATA/internal.token" \
  >>"$DATA/gateway.log" 2>&1 &
echo $! >"$DATA/gateway.pid"
echo "control pid $(cat "$DATA/control.pid")"
echo "gateway pid $(cat "$DATA/gateway.pid")"
echo "logs: $DATA/control.log and $DATA/gateway.log"
echo "Read the cert fingerprint from the control log. The bootstrap file is $DATA/bootstrap.token and is not printed here."
