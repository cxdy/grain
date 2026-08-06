#!/usr/bin/env bash
# smoke-fc-net.sh — Firecracker publish + agent smoke with real host→guest data.
#
# Proves:
#   1) create-time -P (DNAT+SNAT) reaches a guest HTTP listener
#   2) live grain fwd add (TCP proxy) reaches the same listener
#   3) agent path still works (uname over vsock)
#
# Prerequisites: Linux+KVM, firecracker, CAP_NET_ADMIN, /dev/net/tun,
# hypervisor: firecracker, pullable fc-kernel + grain-ubuntu-fc.
# Guest needs python3 or nc for the ephemeral HTTP listener.
#
# Usage:
#   ./scripts/smoke-fc-net.sh
#   GRAIN_BIN=./grain ./scripts/smoke-fc-net.sh
#
set -euo pipefail

GRAIN_BIN="${GRAIN_BIN:-grain}"
NAME="${SMOKE_NAME:-fc-net-$$}"
HOST_PORT="${SMOKE_HOST_PORT:-18080}"
GUEST_PORT="${SMOKE_GUEST_PORT:-8080}"
LIVE_HOST="${SMOKE_LIVE_HOST:-$((HOST_PORT + 1))}"
MARKER="grain-fc-net-ok"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v "$GRAIN_BIN" >/dev/null || die "grain not found"
[[ "$(uname -s)" == Linux ]] || die "smoke-fc-net requires Linux"
[[ -e /dev/kvm ]] || die "missing /dev/kvm"
[[ -e /dev/net/tun ]] || die "missing /dev/net/tun (need CAP_NET_ADMIN)"
command -v firecracker >/dev/null 2>&1 || die "firecracker not on PATH"
command -v iptables >/dev/null 2>&1 || die "iptables not on PATH"
command -v curl >/dev/null 2>&1 || die "curl not on PATH (needed for host→guest proof)"

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

# Start a real HTTP listener in the guest (python3 preferred; nc fallback).
log "start guest HTTP listener on :${GUEST_PORT}"
"$GRAIN_BIN" x "$NAME" -- sh -c "
set -e
PORT=${GUEST_PORT}
MARKER=${MARKER}
if command -v python3 >/dev/null 2>&1; then
  nohup python3 -c \"
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = b'${MARKER}\\n'
        self.send_response(200)
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a):
        pass
http.server.HTTPServer(('0.0.0.0', \$PORT), H).serve_forever()
\" >/tmp/fc-net-http.log 2>&1 &
elif command -v nc >/dev/null 2>&1; then
  nohup sh -c 'while true; do printf \"HTTP/1.0 200 OK\\r\\nContent-Length: 16\\r\\n\\r\\n${MARKER}\\n\" | nc -l -p \$PORT -q 1 2>/dev/null || nc -l \$PORT 2>/dev/null; done' >/tmp/fc-net-http.log 2>&1 &
else
  echo 'no python3 or nc in guest' >&2
  exit 1
fi
sleep 0.5
echo guest-listener-started
" || die "failed to start guest HTTP listener"

# Wait until guest itself can hit the listener (eth0 + process up).
log "wait for guest listener readiness"
ready=0
for _ in $(seq 1 40); do
  if "$GRAIN_BIN" x "$NAME" -- sh -c "command -v curl >/dev/null && curl -sf --max-time 1 http://127.0.0.1:${GUEST_PORT}/ | grep -q ${MARKER}" 2>/dev/null; then
    ready=1
    break
  fi
  # Fallback: ss/netstat inside guest
  if "$GRAIN_BIN" x "$NAME" -- sh -c "ss -lnt 2>/dev/null | grep -q ':${GUEST_PORT}' || netstat -lnt 2>/dev/null | grep -q ':${GUEST_PORT}'" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.5
done
[[ "$ready" -eq 1 ]] || die "guest listener on :${GUEST_PORT} never became ready"

# Criterion: create-time -P host→guest HTTP success
log "curl create-time publish http://127.0.0.1:${HOST_PORT}/"
body=""
ok=0
for _ in $(seq 1 30); do
  if body=$(curl -sf --max-time 2 "http://127.0.0.1:${HOST_PORT}/" 2>/dev/null); then
    if printf '%s' "$body" | grep -q "$MARKER"; then
      ok=1
      break
    fi
  fi
  sleep 0.5
done
[[ "$ok" -eq 1 ]] || die "create-time -P failed: no ${MARKER} from http://127.0.0.1:${HOST_PORT}/ (body=${body:-empty})"
log "create-time -P OK: $(printf '%s' "$body" | tr -d '\r' | head -c 80)"

# Criterion: live grain fwd TCP proxy host→guest HTTP success
log "fwd add live ${LIVE_HOST}:${GUEST_PORT}"
"$GRAIN_BIN" fwd add "$NAME" "${LIVE_HOST}:${GUEST_PORT}" || die "grain fwd add failed"
"$GRAIN_BIN" fwd ls "$NAME" || true

log "curl live fwd http://127.0.0.1:${LIVE_HOST}/"
body2=""
ok2=0
for _ in $(seq 1 30); do
  if body2=$(curl -sf --max-time 2 "http://127.0.0.1:${LIVE_HOST}/" 2>/dev/null); then
    if printf '%s' "$body2" | grep -q "$MARKER"; then
      ok2=1
      break
    fi
  fi
  sleep 0.5
done
[[ "$ok2" -eq 1 ]] || die "live fwd failed: no ${MARKER} from http://127.0.0.1:${LIVE_HOST}/ (body=${body2:-empty})"
log "live fwd OK: $(printf '%s' "$body2" | tr -d '\r' | head -c 80)"

"$GRAIN_BIN" fwd rm "$NAME" "${LIVE_HOST}" || true

log "delete"
"$GRAIN_BIN" delete "$NAME"
trap - EXIT
log "smoke-fc-net OK (create-time -P + live fwd host→guest HTTP)"
