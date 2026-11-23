#!/bin/sh
# Installs CyanDesk from this checkout. It does not print secret values.
set -eu
umask 077
cd "$(dirname "$0")/.."
if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed; the one-command container install cannot run here" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker is installed but the daemon is not reachable" >&2
  exit 1
fi
docker compose -f deploy/compose.yaml up -d --build
echo "HTTPS admin and API: https://127.0.0.1:13111"
echo "Gateway: 13110/tcp and 13110/udp"
echo "The bootstrap token is inside the cyandesk-data volume at bootstrap.token."
echo "Read it from the volume on the server. This script does not print it."
