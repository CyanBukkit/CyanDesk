#!/bin/sh
# Rebuilds and restarts the containers. The data volume is kept.
# Device keys, the certificate, and accounts stay in that volume.
set -eu
cd "$(dirname "$0")/.."
if ! docker info >/dev/null 2>&1; then
  echo "docker daemon is not reachable" >&2
  exit 1
fi
docker compose -f deploy/compose.yaml up -d --build
echo "Volume cyandesk-data was not deleted."
echo "Rollback: docker compose -f deploy/compose.yaml down"
echo "down does not delete the volume. Unpack the previous release and start it the same way."
