#!/usr/bin/env bash
# bake-fc.sh — scaffold for Firecracker kernel + raw rootfs bake (Phase 1).
#
# Not a full bake yet. Documents the intended artifact contract and validates
# local prerequisites. Real image build lands when Linux+KVM CI (or a documented
# self-hosted path) can produce:
#
#   dist/fc/grain-ubuntu-fc-<arch>.raw
#   dist/fc/grain-ubuntu-fc-<arch>.raw.sha256
#   dist/fc/vmlinux-<arch>
#   dist/fc/vmlinux-<arch>.sha256
#
# Published to GitHub Release tag `fc-latest` (see internal/image fcReleaseBase).
# Catalog IDs: grain-ubuntu-fc (raw rootfs + agent), fc-kernel (vmlinux).
#
# Usage:
#   ./scripts/bake-fc.sh --dry-run     # print plan + check host tools (exit 0)
#   ./scripts/bake-fc.sh --check       # same as dry-run
#   ./scripts/bake-fc.sh --ci          # CI entrypoint (dry-run until bake lands)
#
# Env:
#   ARTIFACT_DIR  default ./dist/fc
#   ARCH          default: uname -m mapped to amd64|arm64
#
set -euo pipefail

ARTIFACT_DIR="${ARTIFACT_DIR:-./dist/fc}"
DRY_RUN=0
CI_MODE=0
CHECK_ONLY=0

for arg in "$@"; do
  case "$arg" in
    --dry-run|--check) DRY_RUN=1; CHECK_ONLY=1 ;;
    --ci) CI_MODE=1; DRY_RUN=1 ;;
    -h|--help)
      sed -n '2,35p' "$0"
      exit 0
      ;;
    *)
      printf 'error: unknown argument %q\n' "$arg" >&2
      exit 2
      ;;
  esac
done

log() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }

map_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo "$m" ;;
  esac
}

ARCH="${ARCH:-$(map_arch)}"
ROOTFS_NAME="grain-ubuntu-fc-${ARCH}.raw"
KERNEL_NAME="vmlinux-${ARCH}"

log "Firecracker bake scaffold (arch=${ARCH})"
log "Planned artifacts under ${ARTIFACT_DIR}/:"
printf '    %s\n' \
  "${ROOTFS_NAME}" \
  "${ROOTFS_NAME}.sha256" \
  "${KERNEL_NAME}" \
  "${KERNEL_NAME}.sha256"
log "Release tag: fc-latest"
log "Catalog: grain-ubuntu-fc (rootfs), fc-kernel (kernel → data_dir/kernels/vmlinux)"
log "Operator today: grain image import <path> --id grain-ubuntu-fc|fc-kernel"

need() {
  if command -v "$1" >/dev/null 2>&1; then
    log "found $1"
  else
    warn "missing $1 (needed for full bake later)"
    return 1
  fi
}

log "Prerequisite scan (soft until bake is implemented)"
miss=0
need firecracker || miss=1
need qemu-img || miss=1
if [[ "$(uname -s)" == Linux ]]; then
  if [[ -e /dev/kvm ]]; then
    log "found /dev/kvm"
  else
    warn "missing /dev/kvm (full bake needs KVM)"
    miss=1
  fi
else
  warn "host is not Linux — Firecracker bake is Linux/KVM-only"
  miss=1
fi

if [[ "$DRY_RUN" -eq 1 || "$CHECK_ONLY" -eq 1 || "$CI_MODE" -eq 1 ]]; then
  log "dry-run / check only — no artifacts written"
  if [[ "$miss" -ne 0 ]]; then
    warn "some prerequisites missing (ok for scaffold; full bake will hard-require them)"
  fi
  log "Next implementation steps:"
  printf '    1) Build Firecracker-capable vmlinux (virtio MMIO, no PCI)\n'
  printf '    2) Build raw rootfs with grain-agent on vsock :7475\n'
  printf '    3) sha256sum → companion sidecars; upload to fc-latest\n'
  printf '    4) Flip catalog LocalOnly=false + set URLs/digests\n'
  exit 0
fi

printf 'error: full bake not implemented yet — run with --dry-run\n' >&2
exit 2
