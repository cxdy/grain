#!/usr/bin/env bash
# bench-fc.sh — Firecracker create --wait agent p50/p95 on a named SKU.
#
# Prerequisites (Linux + KVM):
#   hypervisor: firecracker in ~/.grain/config.yaml
#   grain up
#   grain image pull fc-kernel && grain image pull grain-ubuntu-fc
#   firecracker on PATH; /dev/kvm RDWR
#
# Usage:
#   ./scripts/bench-fc.sh              # N=5 (default)
#   ./scripts/bench-fc.sh -n 10
#   N=8 ./scripts/bench-fc.sh
#
# Env:
#   GRAIN_BIN, N, SKU_LABEL (default: auto from uname/cpu)
#
# Writes human-readable stats to stdout. Redirect to a file for records.
#
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
N="${N:-5}"
GRAIN_BIN="${GRAIN_BIN:-grain}"
IMAGE="${IMAGE:-grain-ubuntu-fc}"
WAIT="${WAIT:-agent}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n) N="$2"; shift 2 ;;
    -i|--image) IMAGE="$2"; shift 2 ;;
    --wait) WAIT="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,22p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      exit 1
      ;;
  esac
done

if [[ "$(uname -s)" != Linux ]]; then
  echo "bench-fc requires Linux" >&2
  exit 1
fi
if [[ ! -e /dev/kvm ]]; then
  echo "missing /dev/kvm" >&2
  exit 1
fi

SKU_LABEL="${SKU_LABEL:-}"
if [[ -z "$SKU_LABEL" ]]; then
  arch="$(uname -m)"
  # Best-effort instance metadata (AWS) else generic nested-virt label.
  itype="$(curl -fsS --connect-timeout 1 --max-time 2 \
    http://169.254.169.254/latest/meta-data/instance-type 2>/dev/null || true)"
  if [[ -n "$itype" ]]; then
    SKU_LABEL="AWS ${itype} nested-virt ${arch}"
  else
    SKU_LABEL="Linux nested-or-bare KVM ${arch}"
  fi
fi

echo "bench-fc: reference_sku=${SKU_LABEL}"
echo "bench-fc: image=${IMAGE} wait=${WAIT} N=${N}"
echo "bench-fc: note=Firecracker agent path (vsock UDS CONNECT); not QEMU hostfwd"
echo "---"

export N IMAGE WAIT GRAIN_BIN
# Reuse portable create loop from bench-create.sh
exec "$ROOT/scripts/bench-create.sh" -n "$N" -i "$IMAGE" --wait "$WAIT"
