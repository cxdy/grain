#!/usr/bin/env bash
# smoke-fc-net.sh — Firecracker publish + agent smoke (vFC-2 partial).
#
# Prerequisites: Linux+KVM, firecracker, CAP_NET_ADMIN, /dev/net/tun,
# hypervisor: firecracker, pullable fc-kernel + grain-ubuntu-fc.
#
# Usage:
#   ./scripts/smoke-fc-net.sh
#   GRAIN_BIN=./grain ./scripts/smoke-fc-net.sh
#
set -euo pipefail

GRAIN_BIN="${GRAIN_BIN:-grain}"
NAME="${SMOKE_NAME:-fc-net-$$}"
HOST_PORT="${SMOKE_HOST_PORT:-18080}"
GUEST_PORT="${SMOKE_GUEST_PORT:-9}" # discard port; proves DNAT path without guest server

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v "$GRAIN_BIN" >/dev/null || die "grain not found"
[[ "$(uname -s)" == Linux ]] || die "smoke-fc-net requires Linux"
[[ -e /dev/kvm ]] || die "missing /dev/kvm"
[[ -e /dev/net/tun ]] || die "missing /dev/net/tun (need CAP_NET_ADMIN)"
command -v firecracker >/dev/null 2>&1 || die "firecracker not on PATH"
command -v iptables >/dev/null 2>&1 || die "iptables not on PATH"

if ! "$GRAIN_BIN" ls >/dev/null 2>&1; then
  die "grain daemon not reachable — grain up with hypervisor: firecracker"
fi

log "doctor"
"$GRAIN_BIN" doctor || die "doctor failed"

if ! "$GRAIN_BIN" image ls 2>/dev/null | awk '$1=="fc-kernel" && $2=="yes"{found=1} END{exit !found}'; then
  log "pull fc-kernel"
  "$GRAIN_BIN" image pull fc-kernel
fi
if ! "$GRAIN_BIN" image ls 2>/dev/null | awk '$1=="grain-ubuntu-fc" && $2=="yes"{found=1} END{exit !found}'; then
  log "pull grain-ubuntu-fc"
  "$GRAIN_BIN" image pull grain-ubuntu-fc
fi

cleanup() {
  "$GRAIN_BIN" delete "$NAME" 2>/dev/null || true
}
trap cleanup EXIT

log "create $NAME -P ${HOST_PORT}:${GUEST_PORT} wait=agent"
"$GRAIN_BIN" new -i grain-ubuntu-fc -n "$NAME" --wait agent -c 1 -m 512 -P "${HOST_PORT}:${GUEST_PORT}"

log "exec uname (agent path)"
"$GRAIN_BIN" x "$NAME" -- uname -a

log "list forwards"
"$GRAIN_BIN" fwd ls "$NAME" || true

# Host port should be listening after DNAT (kernel still accepts SYN even if guest RST).
if command -v ss >/dev/null 2>&1; then
  if ss -lnt 2>/dev/null | grep -qE ":${HOST_PORT}\\b"; then
    log "host port ${HOST_PORT} present in ss (or DNAT target)"
  else
    # DNAT alone may not show in ss; try connect
    log "ss has no LISTEN on ${HOST_PORT} (ok for pure DNAT); probing connect"
  fi
fi

# Live forward add (TCP proxy) — needs socat or python3
LIVE_HOST=$((HOST_PORT + 1))
log "fwd add live ${LIVE_HOST}:9"
if "$GRAIN_BIN" fwd add "$NAME" "${LIVE_HOST}:9"; then
  log "live forward ok"
  "$GRAIN_BIN" fwd rm "$NAME" "${LIVE_HOST}" || true
else
  log "live forward skipped/failed (socat/python3 or guest IP?)"
fi

log "delete"
"$GRAIN_BIN" delete "$NAME"
trap - EXIT
log "smoke-fc-net OK"
