#!/usr/bin/env bash
# Launcher for Grain Desktop — opens the .app (bare GUI binaries are often
# SIGKILL'd under iCloud Documents / Gatekeeper when not packaged).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/desktop/build/bin/Grain.app"
if [[ -d "$APP" ]]; then
  open "$APP" "$@"
  exit 0
fi
echo "Grain.app not found at $APP — run: just desktop-build" >&2
exit 1
