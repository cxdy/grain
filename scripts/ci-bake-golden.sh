#!/usr/bin/env bash
# ci-bake-golden.sh — attempt a real headless golden bake for CI / local automation.
#
# Builds grain + agent-linux, runs bake-golden.sh --ci with a long timeout budget,
# and leaves importable qcow2 + sha256 under ARTIFACT_DIR (default ./dist/golden).
#
# Intended for:
#   - GitHub Actions (.github/workflows/bake-golden.yml) on ubuntu-latest (amd64)
#   - Self-hosted runners with /dev/kvm (recommended)
#   - Manual: ./scripts/ci-bake-golden.sh
#
# KVM note: grain QEMU uses -cpu host, which needs KVM (Linux) or HVF (macOS).
# Standard GitHub-hosted runners often lack /dev/kvm; the bake may fail or be
# extremely slow under pure TCG. Prefer self-hosted + KVM, or bake on a laptop
# and attach the artifact to a release / import via grain image import.
#
# Env:
#   ARTIFACT_DIR     default ./dist/golden
#   GRAIN_DATA_DIR   optional isolated data dir (else bake-golden --ci picks one)
#   CI_READY_TIMEOUT default 15m
#   SKIP_BUILD=1     skip make build agent-linux
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ARTIFACT_DIR="${ARTIFACT_DIR:-${ROOT}/dist/golden}"
export ARTIFACT_DIR
export CI_READY_TIMEOUT="${CI_READY_TIMEOUT:-15m}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
warn() { printf 'warning: %s\n' "$*" >&2; }

log "ci-bake-golden: root=$ROOT artifact_dir=$ARTIFACT_DIR"

# --- doctor: host tools ---
if ! command -v go >/dev/null 2>&1; then
  die "go not found (setup-go or install Go)"
fi
if ! command -v qemu-img >/dev/null 2>&1; then
  die "qemu-img not found — install qemu-utils (apt) or qemu (brew)"
fi

ARCH="$(go env GOARCH)"
case "$ARCH" in
  amd64) QEMU_SYSTEM=qemu-system-x86_64 ;;
  arm64) QEMU_SYSTEM=qemu-system-aarch64 ;;
  *) QEMU_SYSTEM="qemu-system-${ARCH}" ;;
esac
if ! command -v "$QEMU_SYSTEM" >/dev/null 2>&1; then
  die "$QEMU_SYSTEM not found — install qemu-system-x86 (amd64) or qemu-system-arm (arm64)"
fi

if [[ "$(uname -s)" == "Linux" ]]; then
  if [[ ! -e /dev/kvm ]]; then
    warn "============================================================"
    warn " /dev/kvm missing — real bake likely to fail or take forever"
    warn " grain uses -cpu host (needs KVM). Options:"
    warn "   1) self-hosted runner with KVM"
    warn "   2) bake locally: make build agent-linux && ./scripts/bake-golden.sh --ci"
    warn "   3) download a prior Actions artifact and grain image import"
    warn "============================================================"
  elif [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
    warn "/dev/kvm not rw — add user to kvm group or run with appropriate perms"
  else
    log "KVM ok (/dev/kvm)"
  fi
fi

# --- build ---
if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
  log "make build agent-linux"
  make build agent-linux
else
  log "SKIP_BUILD=1 — using existing bin/"
fi
[[ -x ./bin/grain ]] || die "missing ./bin/grain"
[[ -f "./bin/grain-agent-linux-${ARCH}" ]] || die "missing ./bin/grain-agent-linux-${ARCH}"

export GRAIN_BIN="${GRAIN_BIN:-$ROOT/bin/grain}"
mkdir -p "$ARTIFACT_DIR"

log "running scripts/bake-golden.sh --ci"
# Real bake — long wall clock on TCG; workflow timeout-minutes should be ≥ 45.
exec ./scripts/bake-golden.sh --ci
