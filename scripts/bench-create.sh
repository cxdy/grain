#!/usr/bin/env bash
# bench-create.sh — time N grain creates and report p50/p95/avg boot latency.
#
# Prerequisites:
#   grain up
#   grain image pull grain-ubuntu   # or ubuntu-cloud / alpine-cloud
#   make agent-linux                # for --wait agent on non-golden images
#
# Usage:
#   ./scripts/bench-create.sh                 # 5 creates, auto wait
#   ./scripts/bench-create.sh -n 10 -i grain-ubuntu --wait agent
#   N=10 IMAGE=ubuntu-cloud WAIT=ssh ./scripts/bench-create.sh
#
# Env / flags:
#   -n N / N=          number of creates (default 5)
#   -i ID / IMAGE=     catalog image id (default: grain-ubuntu if local, else ubuntu-cloud)
#   --wait MODE        ssh | agent | userdata | auto (default auto)
#   --keep             do not rm each VM after timing (default: rm)
#   GRAIN_BIN=         grain binary (default: grain on PATH, else ./bin/grain)
#
set -euo pipefail

N="${N:-5}"
IMAGE="${IMAGE:-}"
WAIT="${WAIT:-auto}"
KEEP=0
GRAIN_BIN="${GRAIN_BIN:-}"

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \?//'
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n) N="$2"; shift 2 ;;
    -i|--image) IMAGE="$2"; shift 2 ;;
    --wait) WAIT="$2"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    -h|--help) usage 0 ;;
    *)
      echo "unknown arg: $1" >&2
      usage 1
      ;;
  esac
done

if ! [[ "$N" =~ ^[1-9][0-9]*$ ]]; then
  echo "N must be a positive integer (got $N)" >&2
  exit 1
fi

if [[ -n "$GRAIN_BIN" ]]; then
  GRAIN=("$GRAIN_BIN")
elif command -v grain >/dev/null 2>&1; then
  GRAIN=(grain)
elif [[ -x ./bin/grain ]]; then
  GRAIN=(./bin/grain)
else
  echo "grain binary not found (set GRAIN_BIN or install grain)" >&2
  exit 1
fi

# Daemon must be up.
if ! "${GRAIN[@]}" ls >/dev/null 2>&1; then
  echo "grain daemon not reachable — run: grain up" >&2
  exit 1
fi

# Resolve image: prefer grain-ubuntu when local Ready.
if [[ -z "$IMAGE" ]]; then
  if "${GRAIN[@]}" image ls 2>/dev/null | grep -E 'grain-ubuntu' | grep -qiE 'yes|ready|local'; then
    IMAGE=grain-ubuntu
  else
    IMAGE=ubuntu-cloud
  fi
fi

# Ensure image is local (Ready).
if ! "${GRAIN[@]}" image ls 2>/dev/null | grep -E "$IMAGE" | grep -qiE 'yes|ready|local'; then
  echo "image $IMAGE not ready locally — pull first:" >&2
  echo "  grain image pull $IMAGE" >&2
  # Still try a soft check via doctor if available
  if ! "${GRAIN[@]}" image pull "$IMAGE" 2>/dev/null; then
    echo "failed to ensure image $IMAGE" >&2
    exit 1
  fi
fi

# Portable ms timestamp (macOS/Linux).
now_ms() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import time; print(int(time.time()*1000))'
  elif date +%s%3N 2>/dev/null | grep -qv N; then
    date +%s%3N
  else
    # fallback: second resolution * 1000
    echo $(( $(date +%s) * 1000 ))
  fi
}

echo "bench-create: N=$N image=$IMAGE wait=$WAIT grain=${GRAIN[*]}"
echo "---"

times_ms=()
names=()

for i in $(seq 1 "$N"); do
  name="bench-$(date +%s)-$i-$$"
  names+=("$name")
  t0=$(now_ms)
  create_args=(new -n "$name" -i "$IMAGE")
  if [[ -n "$WAIT" && "$WAIT" != "auto" ]]; then
    create_args+=(--wait "$WAIT")
  fi
  if ! "${GRAIN[@]}" "${create_args[@]}" >/dev/null; then
    echo "create #$i failed ($name)" >&2
    # cleanup any partial
    "${GRAIN[@]}" rm "$name" >/dev/null 2>&1 || true
    exit 1
  fi
  t1=$(now_ms)
  elapsed=$((t1 - t0))
  times_ms+=("$elapsed")
  echo "  #$i  ${elapsed}ms  $name"

  if [[ "$KEEP" -eq 0 ]]; then
    "${GRAIN[@]}" rm "$name" >/dev/null 2>&1 || true
  fi
done

# Stats via python for portable percentile math.
export BENCH_TIMES="${times_ms[*]}"
python3 - <<'PY'
import os, statistics

raw = os.environ.get("BENCH_TIMES", "").split()
vals = sorted(int(x) for x in raw if x)
n = len(vals)
if n == 0:
    raise SystemExit("no samples")

def pct(p):
    if n == 1:
        return vals[0]
    # nearest-rank
    k = max(0, min(n - 1, int(round(p / 100.0 * (n - 1)))))
    return vals[k]

avg = sum(vals) / n
p50 = pct(50)
p95 = pct(95)
print("---")
print(f"samples: {n}")
print(f"min:     {min(vals)} ms")
print(f"max:     {max(vals)} ms")
print(f"avg:     {avg:.1f} ms")
print(f"p50:     {p50} ms")
print(f"p95:     {p95} ms")
PY

if [[ "$KEEP" -eq 1 ]]; then
  echo "kept VMs: ${names[*]}"
fi
