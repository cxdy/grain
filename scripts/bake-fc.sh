#!/usr/bin/env bash
# bake-fc.sh — Firecracker kernel + raw rootfs bake (Phase 1).
#
# Artifacts (default ARTIFACT_DIR=./dist/fc):
#   grain-ubuntu-fc-<arch>.raw
#   grain-ubuntu-fc-<arch>.raw.sha256
#   vmlinux-<arch>
#   vmlinux-<arch>.sha256
#
# Published (later) to GitHub Release tag `fc-latest` (see internal/image fcReleaseBase).
# Catalog IDs: grain-ubuntu-fc (rootfs), fc-kernel (vmlinux → data_dir/kernels/vmlinux).
#
# Rootfs strategy (v1): start from Firecracker CI Ubuntu squashfs, convert to
# writable ext4, inject a static grain-agent with systemd unit (vsock :7475).
# Kernel strategy (v1): pin a Firecracker CI vmlinux (virtio-mmio, no PCI).
#
# Usage:
#   ./scripts/bake-fc.sh --dry-run
#   ./scripts/bake-fc.sh --kernel              # fetch pinned vmlinux
#   ./scripts/bake-fc.sh --rootfs              # build raw rootfs with agent
#   ./scripts/bake-fc.sh --all                 # kernel + rootfs
#   ./scripts/bake-fc.sh --ci                  # --all for CI (needs Linux)
#
# Env:
#   ARTIFACT_DIR   default ./dist/fc
#   ARCH           amd64|arm64 (default: host)
#   GRAIN_AGENT    path to linux grain-agent (default: build via go)
#   FC_CI_VERSION  Firecracker CI prefix (default: v1.12)
#   FC_KERNEL_VER  kernel artifact name without path (default: vmlinux-6.1.128)
#   ROOTFS_SIZE_MB size of ext4 image (default: 2048)
#
set -euo pipefail

ARTIFACT_DIR="${ARTIFACT_DIR:-./dist/fc}"
DRY_RUN=0
DO_KERNEL=0
DO_ROOTFS=0
CI_MODE=0

for arg in "$@"; do
  case "$arg" in
    --dry-run|--check) DRY_RUN=1 ;;
    --kernel) DO_KERNEL=1 ;;
    --rootfs) DO_ROOTFS=1 ;;
    --all) DO_KERNEL=1; DO_ROOTFS=1 ;;
    --ci) CI_MODE=1; DO_KERNEL=1; DO_ROOTFS=1 ;;
    -h|--help)
      sed -n '2,45p' "$0"
      exit 0
      ;;
    *)
      printf 'error: unknown argument %q\n' "$arg" >&2
      exit 2
      ;;
  esac
done

# Default action when no mode flags: dry-run help (backward compatible scaffold).
if [[ "$DRY_RUN" -eq 0 && "$DO_KERNEL" -eq 0 && "$DO_ROOTFS" -eq 0 && "$CI_MODE" -eq 0 ]]; then
  DRY_RUN=1
fi

# Logs go to stderr so command substitutions (e.g. build_agent) stay clean.
log() { printf '==> %s\n' "$*" >&2; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

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
case "$ARCH" in
  amd64) FC_ARCH=x86_64 ;;
  arm64) FC_ARCH=aarch64 ;;
  *) die "unsupported ARCH=$ARCH (want amd64|arm64)" ;;
esac

FC_CI_VERSION="${FC_CI_VERSION:-v1.12}"
# Pinned CI kernel/rootfs names (override via env when FC republishes).
FC_KERNEL_VER="${FC_KERNEL_VER:-vmlinux-6.1.128}"
FC_UBUNTU_SQFS="${FC_UBUNTU_SQFS:-ubuntu-24.04.squashfs}"
# Keep under GitHub release asset limit (2 GiB exclusive).
ROOTFS_SIZE_MB="${ROOTFS_SIZE_MB:-1536}"
S3_BASE="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${FC_CI_VERSION}/${FC_ARCH}"

ROOTFS_NAME="grain-ubuntu-fc-${ARCH}.raw"
KERNEL_NAME="vmlinux-${ARCH}"

need() {
  command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

write_sidecar() {
  local f="$1"
  local sum
  sum="$(sha256_file "$f")"
  # sha256sum-compatible sidecar
  printf '%s  %s\n' "$sum" "$(basename "$f")" >"${f}.sha256"
  log "sha256 ${f}.sha256 → $sum"
}

log "Firecracker bake (arch=${ARCH} fc_arch=${FC_ARCH} ci=${FC_CI_VERSION})"
log "Artifacts → ${ARTIFACT_DIR}/"
printf '    %s\n' "${ROOTFS_NAME}" "${ROOTFS_NAME}.sha256" "${KERNEL_NAME}" "${KERNEL_NAME}.sha256"

if [[ "$DRY_RUN" -eq 1 && "$DO_KERNEL" -eq 0 && "$DO_ROOTFS" -eq 0 ]]; then
  log "dry-run only — no downloads"
  log "Kernel URL:  ${S3_BASE}/${FC_KERNEL_VER}"
  log "Rootfs URL:  ${S3_BASE}/${FC_UBUNTU_SQFS}"
  log "Run: ./scripts/bake-fc.sh --all   # on Linux with KVM tools"
  exit 0
fi

if [[ "$(uname -s)" != Linux ]]; then
  die "bake requires Linux (current: $(uname -s))"
fi

need curl
need qemu-img
need mkfs.ext4
need unsquashfs
need mount
need sha256sum || need shasum

mkdir -p "$ARTIFACT_DIR"
WORKDIR="${WORKDIR:-$(mktemp -d -t grain-fc-bake.XXXXXX)}"
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

fetch() {
  local url="$1" dest="$2"
  if [[ -f "$dest" && -s "$dest" ]]; then
    log "reuse $dest"
    return 0
  fi
  log "download $url"
  curl -fsSL --retry 3 -o "${dest}.partial" "$url"
  mv "${dest}.partial" "$dest"
}

build_agent() {
  if [[ -n "${GRAIN_AGENT:-}" && -x "$GRAIN_AGENT" ]]; then
    echo "$GRAIN_AGENT"
    return 0
  fi
  need go
  local out="$WORKDIR/grain-agent"
  local repo_root
  repo_root="$(cd "$(dirname "$0")/.." && pwd)"
  log "build static grain-agent (CGO_ENABLED=0)"
  (cd "$repo_root" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o "$out" ./cmd/grain-agent)
  echo "$out"
}

bake_kernel() {
  local dest="${ARTIFACT_DIR}/${KERNEL_NAME}"
  local url="${S3_BASE}/${FC_KERNEL_VER}"
  fetch "$url" "$WORKDIR/${FC_KERNEL_VER}"
  cp -f "$WORKDIR/${FC_KERNEL_VER}" "$dest"
  write_sidecar "$dest"
  log "kernel ready: $dest"
}

bake_rootfs() {
  local dest="${ARTIFACT_DIR}/${ROOTFS_NAME}"
  local sqfs="$WORKDIR/${FC_UBUNTU_SQFS}"
  local agent
  agent="$(build_agent)"
  file "$agent" | grep -qi 'statically linked\|static' || warn "grain-agent may not be static (prefer CGO_ENABLED=0)"

  fetch "${S3_BASE}/${FC_UBUNTU_SQFS}" "$sqfs"

  local sqmnt="$WORKDIR/sqmnt"
  local root="$WORKDIR/root"
  mkdir -p "$sqmnt" "$root"
  log "unsquashfs → root tree"
  # Prefer unsquashfs over mount (no root required for extract).
  unsquashfs -f -d "$root" "$sqfs" >/dev/null

  log "inject grain-agent + systemd unit"
  mkdir -p "$root/usr/local/bin" "$root/etc/systemd/system/multi-user.target.wants"
  install -m 0755 "$agent" "$root/usr/local/bin/grain-agent"

  cat >"$root/etc/systemd/system/grain-agent.service" <<'EOF'
[Unit]
Description=grain-agent (Firecracker vsock / TCP :7475)
After=local-fs.target
DefaultDependencies=yes

[Service]
Type=simple
ExecStart=/usr/local/bin/grain-agent -listen :7475
Restart=always
RestartSec=1
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
EOF
  ln -sfn /etc/systemd/system/grain-agent.service \
    "$root/etc/systemd/system/multi-user.target.wants/grain-agent.service"

  # Provenance for operators / doctor.
  mkdir -p "$root/etc/grain"
  cat >"$root/etc/grain/image" <<EOF
id=grain-ubuntu-fc
arch=${ARCH}
base=${FC_CI_VERSION}/${FC_ARCH}/${FC_UBUNTU_SQFS}
agent=static
baked_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

  log "mkfs.ext4 ${ROOTFS_SIZE_MB}MiB image"
  local img="$WORKDIR/rootfs.raw"
  # sparse-friendly
  truncate -s "${ROOTFS_SIZE_MB}M" "$img"
  mkfs.ext4 -F -L grain-root -d "$root" "$img" >/dev/null

  cp -f "$img" "$dest"
  write_sidecar "$dest"
  log "rootfs ready: $dest ($(du -h "$dest" | awk '{print $1}'))"
}

if [[ "$DO_KERNEL" -eq 1 ]]; then
  bake_kernel
fi
if [[ "$DO_ROOTFS" -eq 1 ]]; then
  bake_rootfs
fi

log "done"
ls -lh "${ARTIFACT_DIR}/${KERNEL_NAME}" "${ARTIFACT_DIR}/${ROOTFS_NAME}" 2>/dev/null || true
ls -lh "${ARTIFACT_DIR}"/*.sha256 2>/dev/null || true
