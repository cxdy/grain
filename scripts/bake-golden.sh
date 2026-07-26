#!/usr/bin/env bash
# bake-golden.sh — produce a grain-ubuntu golden image (ubuntu-cloud + grain-agent).
#
# Prerequisites (macOS + QEMU):
#   brew install qemu
#   make build agent-linux
#   grain up
#   grain image pull ubuntu-cloud
#
# Usage:
#   ./scripts/bake-golden.sh              # full automated bake + import
#   ./scripts/bake-golden.sh --dry-run    # print steps only
#   BAKE_VM=my-bake ./scripts/bake-golden.sh
#
# Result:
#   ~/.grain/images/grain-ubuntu/disk.qcow2  (has_agent=true)
#   grain new -i grain-ubuntu
#
set -euo pipefail

BAKE_VM="${BAKE_VM:-bake-vm}"
IMAGE_ID="${IMAGE_ID:-grain-ubuntu}"
GRAIN_BIN="${GRAIN_BIN:-}"
DATA_DIR="${GRAIN_DATA_DIR:-${HOME}/.grain}"
DRY_RUN=0

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
  esac
done

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] %s\n' "$*"
    return 0
  fi
  # shellcheck disable=SC2086
  eval "$@"
}

# Resolve grain binary
if [[ -z "$GRAIN_BIN" ]]; then
  if [[ -x ./bin/grain ]]; then
    GRAIN_BIN=./bin/grain
  elif command -v grain >/dev/null 2>&1; then
    GRAIN_BIN=grain
  else
    die "grain binary not found (build with: make build)"
  fi
fi

if ! command -v qemu-img >/dev/null 2>&1; then
  die "qemu-img not found (brew install qemu)"
fi

log "grain binary: $GRAIN_BIN"
log "bake VM name: $BAKE_VM"
log "target image: $IMAGE_ID"
log "data dir:     $DATA_DIR"

# 1) Linux agent binary for deploy
ARCH="$(go env GOARCH 2>/dev/null || uname -m)"
case "$ARCH" in
  aarch64|arm64) AGENT_ARCH=arm64 ;;
  x86_64|amd64)  AGENT_ARCH=amd64 ;;
  *) AGENT_ARCH="$ARCH" ;;
esac
AGENT_BIN="bin/grain-agent-linux-${AGENT_ARCH}"
if [[ ! -f "$AGENT_BIN" ]]; then
  log "building agent-linux (make agent-linux)"
  run "make agent-linux"
fi
[[ -f "$AGENT_BIN" || "$DRY_RUN" -eq 1 ]] || die "missing $AGENT_BIN"

# 2) Daemon + base image
if [[ "$DRY_RUN" -eq 0 ]]; then
  if ! "$GRAIN_BIN" ls >/dev/null 2>&1; then
    log "starting daemon (grain up)"
    "$GRAIN_BIN" up || true
    sleep 0.5
  fi
fi
log "ensuring ubuntu-cloud is pulled"
run "\"$GRAIN_BIN\" image pull ubuntu-cloud"

# 3) Create persistent bake VM (agent deploys over SSH after boot)
if [[ "$DRY_RUN" -eq 0 ]]; then
  if "$GRAIN_BIN" ls 2>/dev/null | grep -qw "$BAKE_VM"; then
    log "removing existing bake VM $BAKE_VM"
    "$GRAIN_BIN" rm "$BAKE_VM" || true
  fi
fi
log "creating persistent bake VM ($BAKE_VM)"
# Wait for SSH + soft agent deploy (make agent-linux required above).
run "\"$GRAIN_BIN\" new -p -n \"$BAKE_VM\" -i ubuntu-cloud"

# 4) Ensure agent is enabled on boot (EnableAgent already does enable --now;
#    re-run enable so a golden disk boots with agent without redeploy.)
log "ensuring grain-agent is enabled for future boots"
run "\"$GRAIN_BIN\" x \"$BAKE_VM\" -- sudo systemctl enable grain-agent"
# Optional readiness stamp used by wait=userdata consumers
run "\"$GRAIN_BIN\" x \"$BAKE_VM\" -- sudo mkdir -p /var/lib/grain && sudo touch /var/lib/grain/userdata-ran" || true

# 5) Clean shutdown so disk is consistent
log "stopping $BAKE_VM (persistent — disk kept)"
run "\"$GRAIN_BIN\" stop \"$BAKE_VM\""

# 6) Locate VM disk (qcow2 overlay or raw)
VM_DIR="${DATA_DIR}/vms/${BAKE_VM}"
DISK=""
for candidate in \
  "${VM_DIR}/disk.img.qcow2" \
  "${VM_DIR}/disk.qcow2" \
  "${VM_DIR}/disk.img"
do
  if [[ -f "$candidate" ]]; then
    DISK="$candidate"
    break
  fi
done
[[ -n "$DISK" || "$DRY_RUN" -eq 1 ]] || die "could not find disk under $VM_DIR"

log "found bake disk: ${DISK:-<dry-run>}"

# 7) Flatten overlay chain into standalone base and import as grain-ubuntu
OUT_TMP="${DATA_DIR}/images/${IMAGE_ID}.bake.qcow2"
if [[ "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] qemu-img convert -O qcow2 $DISK $OUT_TMP"
  log "[dry-run] $GRAIN_BIN image import $OUT_TMP --id $IMAGE_ID"
else
  mkdir -p "${DATA_DIR}/images"
  log "flattening to standalone qcow2 (qemu-img convert)"
  qemu-img convert -O qcow2 "$DISK" "$OUT_TMP"
  log "importing as $IMAGE_ID"
  "$GRAIN_BIN" image import "$OUT_TMP" --id "$IMAGE_ID"
  rm -f "$OUT_TMP"
fi

# 8) Cleanup bake VM (optional — keep for re-bake iteration)
if [[ "${KEEP_BAKE_VM:-0}" != "1" ]]; then
  log "removing bake VM $BAKE_VM (set KEEP_BAKE_VM=1 to keep)"
  run "\"$GRAIN_BIN\" rm \"$BAKE_VM\""
fi

log "done. Create sandboxes with:"
echo "  $GRAIN_BIN new -i $IMAGE_ID"
echo
echo "Manual alternative:"
echo "  grain new -p -n bake-vm -i ubuntu-cloud"
echo "  grain x bake-vm -- sudo systemctl enable grain-agent"
echo "  grain stop bake-vm"
echo "  qemu-img convert -O qcow2 ~/.grain/vms/bake-vm/disk.img.qcow2 /tmp/grain-ubuntu.qcow2"
echo "  grain image import /tmp/grain-ubuntu.qcow2 --id grain-ubuntu"
