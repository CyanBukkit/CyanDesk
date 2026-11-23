#!/bin/sh
# Translates CYANDESK_CERT_HOST and CYANDESK_GATEWAY_ADVERTISE into flags.
# The certificate names are used only when the data directory is first created.
set -eu
listen="${CYANDESK_LISTEN:-0.0.0.0:13111}"
internal="${CYANDESK_INTERNAL:-0.0.0.0:13112}"
set -- control --data /var/lib/cyandesk --listen "$listen" --internal "$internal" --admin /usr/share/cyandesk/admin
if [ -n "${CYANDESK_CERT_HOST:-}" ]; then
  old=$IFS
  IFS=', '
  for name in $CYANDESK_CERT_HOST; do
    if [ -n "$name" ]; then
      set -- "$@" --cert-host "$name"
    fi
  done
  IFS=$old
fi
if [ -n "${CYANDESK_GATEWAY_ADVERTISE:-}" ]; then
  set -- "$@" --gateway-advertise "$CYANDESK_GATEWAY_ADVERTISE"
fi
if [ -n "${CYANDESK_TLS_CERT:-}" ] || [ -n "${CYANDESK_TLS_KEY:-}" ]; then
  set -- "$@" --tls-cert "$CYANDESK_TLS_CERT" --tls-key "$CYANDESK_TLS_KEY"
fi
exec /usr/local/bin/cyandesk "$@"
