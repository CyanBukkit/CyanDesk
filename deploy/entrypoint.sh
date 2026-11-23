#!/bin/sh
set -eu
mkdir -p /var/lib/cyandesk
if [ "$(id -u)" = "0" ]; then
  chown -R cyandesk:cyandesk /var/lib/cyandesk
  exec runuser -u cyandesk -- "$@"
fi
exec "$@"
