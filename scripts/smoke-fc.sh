#!/usr/bin/env bash
# smoke-fc.sh — one-shot Firecracker create → agent → destroy on Linux+KVM.
#
# Prerequisites: firecracker on PATH, /dev/kvm, grain daemon with
# hypervisor: firecracker, and either:
#   - local imports of fc-kernel + grain-ubuntu-fc, or
#   - network access to pull fc-latest
#
# Usage:
#   ./scripts/smoke-fc.sh
#   GRAIN_BIN=./grain ./scripts/smoke-fc.sh
#
set -euo pipefail

GRAIN_BIN="${GRAIN_BIN:-grain}"
NAME="${SMOKE_NAME:-fc-smoke-$$}"
WAIT="${SMOKE_WAIT:-agent}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v "$GRAIN_BIN" >/dev/null || die "grain not found (set GRAIN_BIN)"
[[ "$(uname -s)" == Linux ]] || die "smoke-fc requires Linux"
[[ -e /dev/kvm ]] || die "missing /dev/kvm (nested virt must expose KVM)"
command -v firecracker >/dev/null 2>&1 || die "firecracker not on PATH"

# Daemon must be up with hypervisor: firecracker.
if ! "$GRAIN_BIN" ls >/dev/null 2>&1; then
  die "grain daemon not reachable — run: grain up (with hypervisor: firecracker)"
fi

log "doctor"
if ! "$GRAIN_BIN" doctor; then
  die "grain doctor failed — fix ✗ items (kernel import/pull, KVM, firecracker binary); see logs under ~/.grain/logs/"
fi

# Prefer already-local images; otherwise pull.
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

log "create $NAME wait=$WAIT"
start=$(date +%s)
"$GRAIN_BIN" new -i grain-ubuntu-fc -n "$NAME" --wait "$WAIT" -c 1 -m 512
end=$(date +%s)
log "create wall ${SECONDS}s (elapsed $((end - start))s)"

log "exec uname"
"$GRAIN_BIN" x "$NAME" -- uname -a

log "delete"
"$GRAIN_BIN" delete "$NAME"
trap - EXIT
log "smoke-fc OK"
