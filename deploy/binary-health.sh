#!/bin/sh
# Checks the public HTTPS port. 13112 is not queried.
set -eu
HOST="${1:-127.0.0.1}"
PORT="${2:-13111}"
base="https://$HOST:$PORT"
curl -fsS --insecure "$base/health/live"
echo
curl -fsS --insecure "$base/health/ready"
echo
code=$(curl -sS -o /dev/null -w '%{http_code}' --insecure -X POST "$base/api/v1/internal/tickets/redeem")
echo "public redeem status $code"
if [ "$code" = "200" ]; then
  echo "internal redeem answered on the public port" >&2
  exit 1
fi
