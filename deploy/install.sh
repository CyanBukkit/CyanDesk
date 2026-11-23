#!/bin/sh
# Installs CyanDesk from this checkout. It does not print secret values.
set -eu
umask 077
cd "$(dirname "$0")/.."
if ! command -v docker >/dev/null 2>&1 && ! command -v podman >/dev/null 2>&1; then
  echo "missing prerequisite: no docker or podman binary, and /var/run/docker.sock is absent" >&2
  echo "rootlesskit and slirp4netns are also absent, so a rootless daemon was not started" >&2
  exit 1
fi
if command -v docker >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
  echo "docker binary exists but the daemon is not reachable" >&2
  exit 1
fi
docker compose -f deploy/compose.yaml up -d --build
echo "HTTPS admin and API: https://127.0.0.1:13111"
echo "Gateway: 13110/tcp and 13110/udp"
echo "The bootstrap token is inside the cyandesk-data volume at bootstrap.token."
echo "Read it from the volume on the server. This script does not print it."
