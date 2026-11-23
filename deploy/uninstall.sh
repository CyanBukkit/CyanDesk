#!/bin/sh
# Stops CyanDesk containers and keeps the data volume.
# Delete the volume only when you pass --delete-data.
set -eu
cd "$(dirname "$0")/.."
docker compose -f deploy/compose.yaml down
if [ "${1:-}" = "--delete-data" ]; then
  echo "Deleting the cyandesk-data volume."
  docker volume rm "$(basename "$(pwd)")_cyandesk-data" || docker volume rm cyandesk-data
else
  echo "Containers stopped. The data volume is still there."
fi
