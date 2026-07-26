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
#   ./scripts/bake-golden.sh --ci         # headless CI bake → artifact + sha256
#   BAKE_VM=my-bake ./scripts/bake-golden.sh
#
# Result (local):
#   ~/.grain/images/grain-ubuntu/disk.qcow2  (has_agent=true)
#   grain new -i grain-ubuntu
#
# Result (--ci):
#   $ARTIFACT_DIR/grain-ubuntu-<arch>.qcow2
#   $ARTIFACT_DIR/grain-ubuntu-<arch>.qcow2.sha256
#
# Env knobs:
#   BAKE_VM, IMAGE_ID, GRAIN_BIN, GRAIN_DATA_DIR, KEEP_BAKE_VM=1
#   ARTIFACT_DIR (default: ./dist/golden) — CI export directory
#   CI_READY_TIMEOUT (default: 15m) — ready_timeout in CI config
#
set -euo pipefail

BAKE_VM="${BAKE_VM:-bake-vm}"
IMAGE_ID="${IMAGE_ID:-grain-ubuntu}"
GRAIN_BIN="${GRAIN_BIN:-}"
DATA_DIR="${GRAIN_DATA_DIR:-${HOME}/.grain}"
ARTIFACT_DIR="${ARTIFACT_DIR:-./dist/golden}"
CI_READY_TIMEOUT="${CI_READY_TIMEOUT:-15m}"
DRY_RUN=0
CI_MODE=0
CONFIG_FILE=""
GRAIN=() # grain command prefix (binary + optional --config)

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --ci) CI_MODE=1 ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0
      ;;
    *)
      printf 'error: unknown argument %q\n' "$arg" >&2
      exit 2
      ;;
  esac
done

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
warn() { printf 'warning: %s\n' "$*" >&2; }

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
  die "qemu-img not found (brew install qemu / apt install qemu-utils)"
fi

# --- arch ---
ARCH="$(go env GOARCH 2>/dev/null || uname -m)"
case "$ARCH" in
  aarch64|arm64) AGENT_ARCH=arm64; QEMU_SYSTEM="${QEMU_SYSTEM:-qemu-system-aarch64}" ;;
  x86_64|amd64)  AGENT_ARCH=amd64; QEMU_SYSTEM="${QEMU_SYSTEM:-qemu-system-x86_64}" ;;
  *) AGENT_ARCH="$ARCH"; QEMU_SYSTEM="${QEMU_SYSTEM:-qemu-system-${ARCH}}" ;;
esac
ARTIFACT_NAME="grain-ubuntu-${AGENT_ARCH}.qcow2"

# --- CI isolation: dedicated data_dir + config (never mock hypervisor) ---
if [[ "$CI_MODE" -eq 1 ]]; then
  if [[ -z "${GRAIN_DATA_DIR:-}" || "$DATA_DIR" == "${HOME}/.grain" ]]; then
    DATA_DIR="${RUNNER_TEMP:-/tmp}/grain-bake-$$"
  fi
  export GRAIN_DATA_DIR="$DATA_DIR"
  mkdir -p "$DATA_DIR" "$ARTIFACT_DIR"
  CONFIG_FILE="${DATA_DIR}/config.yaml"
  # Prefer an unused local API port; socket under data dir.
  # Unix socket only (no TCP API) — avoids port clashes on shared runners.
  cat >"$CONFIG_FILE" <<EOF
data_dir: ${DATA_DIR}
socket: ${DATA_DIR}/grain.sock
api: ""
metrics_addr: ""
hypervisor: qemu
image: ubuntu-cloud
cpus: 2
memory_mb: 2048
disk_gb: 8
ssh_user: ubuntu
ready_timeout: ${CI_READY_TIMEOUT}
log_level: info
EOF
  log "CI mode: isolated data dir $DATA_DIR"
  log "CI mode: config $CONFIG_FILE"
  log "CI mode: artifact dir $ARTIFACT_DIR → $ARTIFACT_NAME"
fi

if [[ -n "$CONFIG_FILE" ]]; then
  GRAIN=("$GRAIN_BIN" --config "$CONFIG_FILE")
else
  GRAIN=("$GRAIN_BIN")
fi

log "grain binary: $GRAIN_BIN"
log "bake VM name: $BAKE_VM"
log "target image: $IMAGE_ID"
log "data dir:     $DATA_DIR"
log "arch:         $AGENT_ARCH"

# --- prerequisite doctor (clear failures for CI) ---
doctor_ci() {
  local ok=1
  if ! command -v "$QEMU_SYSTEM" >/dev/null 2>&1; then
    warn "missing $QEMU_SYSTEM (apt: qemu-system-x86 / brew: qemu)"
    ok=0
  else
    log "found $QEMU_SYSTEM: $(command -v "$QEMU_SYSTEM")"
  fi
  if ! command -v qemu-img >/dev/null 2>&1; then
    warn "missing qemu-img"
    ok=0
  fi
  if [[ ! -x "$GRAIN_BIN" ]] && ! command -v "$GRAIN_BIN" >/dev/null 2>&1; then
    warn "grain binary not executable: $GRAIN_BIN"
    ok=0
  fi
  if [[ "$(uname -s)" == "Linux" ]]; then
    if [[ -e /dev/kvm ]]; then
      if [[ -r /dev/kvm && -w /dev/kvm ]]; then
        log "KVM available (/dev/kvm rw) — bake should be reasonably fast"
      else
        warn "/dev/kvm exists but is not rw for this user — QEMU may fall back to TCG (very slow) or fail with -cpu host"
      fi
    else
      warn "no /dev/kvm — nested virt unavailable; TCG bake is slow and may fail (-cpu host needs KVM/HVF)"
      warn "prefer self-hosted runners with KVM, or bake locally and upload the artifact manually"
    fi
  fi
  if [[ "$ok" -ne 1 ]]; then
    die "CI doctor failed — install qemu-system + qemu-utils and rebuild grain (make build agent-linux)"
  fi
}

if [[ "$CI_MODE" -eq 1 && "$DRY_RUN" -eq 0 ]]; then
  doctor_ci
elif [[ "$CI_MODE" -eq 1 && "$DRY_RUN" -eq 1 ]]; then
  log "[dry-run] would run CI doctor (qemu-system, qemu-img, /dev/kvm check)"
fi

# 1) Linux agent binary for deploy
AGENT_BIN="bin/grain-agent-linux-${AGENT_ARCH}"
if [[ ! -f "$AGENT_BIN" ]]; then
  log "building agent-linux (make agent-linux)"
  run "make agent-linux"
fi
[[ -f "$AGENT_BIN" || "$DRY_RUN" -eq 1 ]] || die "missing $AGENT_BIN"

# Also stage agent under data_dir so daemon can find it regardless of cwd.
if [[ "$DRY_RUN" -eq 0 ]]; then
  mkdir -p "${DATA_DIR}/agent"
  cp -f "$AGENT_BIN" "${DATA_DIR}/agent/$(basename "$AGENT_BIN")"
  # next to grain binary when using ./bin/grain
  if [[ -d bin ]]; then
    cp -f "$AGENT_BIN" "bin/$(basename "$AGENT_BIN")" 2>/dev/null || true
  fi
fi

# 2) Daemon + base image
if [[ "$DRY_RUN" -eq 0 ]]; then
  if ! "${GRAIN[@]}" ls >/dev/null 2>&1; then
    log "starting daemon (grain up)"
    "${GRAIN[@]}" up || true
    sleep 0.5
  fi
  # Fail early if daemon still not reachable
  if ! "${GRAIN[@]}" ls >/dev/null 2>&1; then
    die "grain daemon not reachable after 'up' (check qemu / config / logs under $DATA_DIR)"
  fi
fi
log "ensuring ubuntu-cloud is pulled"
if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '[dry-run] %s image pull ubuntu-cloud\n' "${GRAIN[*]}"
else
  "${GRAIN[@]}" image pull ubuntu-cloud
fi

# 3) Create persistent bake VM (agent deploys over SSH after boot)
if [[ "$DRY_RUN" -eq 0 ]]; then
  if "${GRAIN[@]}" ls 2>/dev/null | grep -qw "$BAKE_VM"; then
    log "removing existing bake VM $BAKE_VM"
    "${GRAIN[@]}" rm "$BAKE_VM" || true
  fi
fi
log "creating persistent bake VM ($BAKE_VM)"
# Wait for agent when possible so golden disks are agent-ready; fall back to ssh.
WAIT_MODE="ssh"
if [[ "$CI_MODE" -eq 1 ]]; then
  WAIT_MODE="agent"
fi
if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '[dry-run] %s new -p -n %s -i ubuntu-cloud --wait %s\n' "${GRAIN[*]}" "$BAKE_VM" "$WAIT_MODE"
else
  if ! "${GRAIN[@]}" new -p -n "$BAKE_VM" -i ubuntu-cloud --wait "$WAIT_MODE"; then
    if [[ "$WAIT_MODE" == "agent" ]]; then
      warn "create --wait agent failed; retrying with --wait ssh"
      "${GRAIN[@]}" rm "$BAKE_VM" 2>/dev/null || true
      "${GRAIN[@]}" new -p -n "$BAKE_VM" -i ubuntu-cloud --wait ssh
    else
      die "grain new failed for bake VM $BAKE_VM"
    fi
  fi
fi

# 4) Prepare golden for clone-friendly boots:
#    - enable grain-agent so clones start the agent without SSH deploy
#    - cloud-init clean so the next boot (new instance-id) re-runs lean seed
#    - touch userdata-ran for wait=userdata / agent Health
#    - clear machine-id so systemd regenerates a unique id per clone
log "preparing golden for clone-friendly boots (agent + cloud-init clean)"
if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '[dry-run] %s x %s -- sudo systemctl enable grain-agent\n' "${GRAIN[*]}" "$BAKE_VM"
  printf '[dry-run] %s x %s -- sudo cloud-init clean --logs\n' "${GRAIN[*]}" "$BAKE_VM"
  printf '[dry-run] %s x %s -- sudo mkdir -p /var/lib/grain && sudo touch /var/lib/grain/userdata-ran\n' "${GRAIN[*]}" "$BAKE_VM"
  printf '[dry-run] %s x %s -- sudo truncate -s 0 /etc/machine-id\n' "${GRAIN[*]}" "$BAKE_VM"
else
  "${GRAIN[@]}" x "$BAKE_VM" -- sudo systemctl enable grain-agent
  # Reset cloud-init state so clones with a new instance-id re-apply hostname/keys
  # from the minimal NoCloud seed (fast path when HasAgent / has_agent).
  "${GRAIN[@]}" x "$BAKE_VM" -- sudo cloud-init clean --logs || warn "cloud-init clean failed (non-fatal)"
  # Readiness stamp used by wait=userdata / agent Health.UserdataRan
  "${GRAIN[@]}" x "$BAKE_VM" -- sudo mkdir -p /var/lib/grain && sudo touch /var/lib/grain/userdata-ran || true
  # Empty machine-id: systemd regenerates a unique id on first boot of each clone.
  "${GRAIN[@]}" x "$BAKE_VM" -- sudo truncate -s 0 /etc/machine-id || warn "machine-id truncate failed (non-fatal)"
fi

# 5) Clean shutdown so disk is consistent
log "stopping $BAKE_VM (persistent — disk kept)"
if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '[dry-run] %s stop %s\n' "${GRAIN[*]}" "$BAKE_VM"
else
  "${GRAIN[@]}" stop "$BAKE_VM"
fi

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
  log "[dry-run] qemu-img convert -O qcow2 ${DISK:-$VM_DIR/disk.img.qcow2} $OUT_TMP"
  if [[ "$CI_MODE" -eq 1 ]]; then
    log "[dry-run] cp $OUT_TMP $ARTIFACT_DIR/$ARTIFACT_NAME"
    log "[dry-run] sha256sum → $ARTIFACT_DIR/${ARTIFACT_NAME}.sha256"
  else
    log "[dry-run] ${GRAIN[*]} image import $OUT_TMP --id $IMAGE_ID"
  fi
else
  mkdir -p "${DATA_DIR}/images"
  log "flattening to standalone qcow2 (qemu-img convert)"
  qemu-img convert -O qcow2 "$DISK" "$OUT_TMP"

  if [[ "$CI_MODE" -eq 1 ]]; then
    mkdir -p "$ARTIFACT_DIR"
    ARTIFACT_PATH="${ARTIFACT_DIR}/${ARTIFACT_NAME}"
    log "exporting CI artifact $ARTIFACT_PATH"
    cp -f "$OUT_TMP" "$ARTIFACT_PATH"
    if command -v sha256sum >/dev/null 2>&1; then
      (cd "$ARTIFACT_DIR" && sha256sum "$ARTIFACT_NAME" | tee "${ARTIFACT_NAME}.sha256")
    else
      # macOS
      (cd "$ARTIFACT_DIR" && shasum -a 256 "$ARTIFACT_NAME" | tee "${ARTIFACT_NAME}.sha256")
    fi
    # Also import into the isolated store so local smoke is possible
    log "importing as $IMAGE_ID (isolated data dir)"
    "${GRAIN[@]}" image import "$OUT_TMP" --id "$IMAGE_ID" || warn "import failed (artifact still written)"
    rm -f "$OUT_TMP"
    log "CI artifacts:"
    ls -lh "$ARTIFACT_PATH" "${ARTIFACT_PATH}.sha256" 2>/dev/null || true
  else
    log "importing as $IMAGE_ID"
    "${GRAIN[@]}" image import "$OUT_TMP" --id "$IMAGE_ID"
    rm -f "$OUT_TMP"
  fi
fi

# 8) Cleanup bake VM (optional — keep for re-bake iteration)
if [[ "${KEEP_BAKE_VM:-0}" != "1" ]]; then
  log "removing bake VM $BAKE_VM (set KEEP_BAKE_VM=1 to keep)"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] %s rm %s\n' "${GRAIN[*]}" "$BAKE_VM"
  else
    "${GRAIN[@]}" rm "$BAKE_VM" || true
  fi
fi

# 9) CI: stop daemon to free resources
if [[ "$CI_MODE" -eq 1 && "$DRY_RUN" -eq 0 ]]; then
  log "stopping CI daemon"
  "${GRAIN[@]}" down 2>/dev/null || true
fi

log "done."
if [[ "$CI_MODE" -eq 1 ]]; then
  echo "  CI artifact: ${ARTIFACT_DIR}/${ARTIFACT_NAME}"
  echo "  checksum:    ${ARTIFACT_DIR}/${ARTIFACT_NAME}.sha256"
  echo
  echo "Import on a machine:"
  echo "  grain image import ${ARTIFACT_NAME} --id grain-ubuntu"
  echo "  grain new -i grain-ubuntu"
else
  echo "  ${GRAIN[*]} new -i $IMAGE_ID"
fi
echo
echo "Manual alternative:"
echo "  grain new -p -n bake-vm -i ubuntu-cloud"
echo "  grain x bake-vm -- sudo systemctl enable grain-agent"
echo "  grain x bake-vm -- sudo cloud-init clean --logs"
echo "  grain x bake-vm -- sudo mkdir -p /var/lib/grain && sudo touch /var/lib/grain/userdata-ran"
echo "  grain x bake-vm -- sudo truncate -s 0 /etc/machine-id"
echo "  grain stop bake-vm"
echo "  qemu-img convert -O qcow2 ~/.grain/vms/bake-vm/disk.img.qcow2 /tmp/grain-ubuntu.qcow2"
echo "  grain image import /tmp/grain-ubuntu.qcow2 --id grain-ubuntu"
