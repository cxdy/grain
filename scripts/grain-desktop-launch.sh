#!/usr/bin/env bash
# Launcher for Grain Desktop — opens the .app on macOS (bare GUI binaries are often
# SIGKILL'd under iCloud Documents / Gatekeeper when not packaged).
#
# Install as bin/grain-desktop only. Never bin/Grain: on case-insensitive
# macOS volumes that path collides with the CLI binary bin/grain.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/desktop/build/bin/Grain.app"
if [[ -d "$APP" ]] && command -v open >/dev/null 2>&1; then
  open "$APP" "$@"
  exit 0
fi
# Linux (and macOS bare-binary fallback)
for cand in \
  "$ROOT/bin/grain-desktop-bin" \
  "$ROOT/desktop/build/bin/Grain" \
  "$ROOT/desktop/build/bin/grain-desktop"; do
  if [[ -x "$cand" ]]; then
    exec "$cand" "$@"
  fi
done
echo "Grain Desktop not found — run: just desktop-build" >&2
exit 1
