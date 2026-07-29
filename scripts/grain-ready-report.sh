#!/usr/bin/env bash
# Guest-side helper: report readiness protocol state for grain.
# Usage (inside the sandbox):
#   grain-ready-report running packages "installing git"
#   grain-ready-report ready
#   grain-ready-report failed "" "setup.sh exit 1"
set -euo pipefail
state="${1:-}"
phase="${2:-}"
message="${3:-}"
if [[ -z "$state" ]]; then
  echo "usage: grain-ready-report <pending|running|ready|failed> [phase] [message]" >&2
  exit 2
fi
dir="${GRAIN_READINESS_DIR:-/var/lib/grain/readiness}"
mkdir -p "$dir"
printf '%s\n' "$state" >"$dir/state"
if [[ -n "$phase" ]]; then printf '%s\n' "$phase" >"$dir/phase"; else rm -f "$dir/phase"; fi
if [[ -n "$message" ]]; then printf '%s\n' "$message" >"$dir/message"; else rm -f "$dir/message"; fi
date -u +"%Y-%m-%dT%H:%M:%SZ" >"$dir/updated_at" 2>/dev/null || true
if [[ "$state" == "failed" && -n "$message" ]]; then
  printf '%s\n' "$message" >"$dir/error"
elif [[ "$state" != "failed" ]]; then
  rm -f "$dir/error"
fi
