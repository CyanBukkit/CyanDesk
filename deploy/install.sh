#!/bin/sh
# Builds and starts CyanDesk. It does not print the bootstrap token.
set -eu
umask 077
cd "$(dirname "$0")/.."
if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed or not on PATH" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "docker info failed. Start the daemon, or add this user to the docker group. sudo can be used when the socket is root-only." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "the docker compose plugin is not available" >&2
  exit 1
fi
if [ -n "${CYANDESK_CERT_HOST:-}" ]; then
  echo "Certificate names for a new data volume: $CYANDESK_CERT_HOST"
  echo "An existing volume keeps the certificate it already created."
fi
docker compose -f deploy/compose.yaml up -d --build
echo "Admin: https://127.0.0.1:13111"
echo "Gateway: 13110/tcp and 13110/udp"
echo "13112 is not published."
echo "On this server, read the bootstrap file yourself. Do not paste it into chat:"
echo "  docker compose -f deploy/compose.yaml exec -T control cat /var/lib/cyandesk/bootstrap.token"
echo "The certificate fingerprint is in: docker compose -f deploy/compose.yaml logs control"
