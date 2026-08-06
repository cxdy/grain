#!/usr/bin/env bash
# Launcher for Grain Desktop — opens the .app (bare GUI binaries are often
# SIGKILL'd under iCloud Documents / Gatekeeper when not packaged).
#
# Install as bin/grain-desktop only. Never bin/Grain: on case-insensitive
# macOS volumes that path collides with the CLI binary bin/grain.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/desktop/build/bin/Grain.app"
if [[ -d "$APP" ]]; then
  open "$APP" "$@"
  exit 0
fi
# Fallback bare binary (no Dock icon / may SIGKILL under Documents)
if [[ -x "$ROOT/bin/grain-desktop-bin" ]]; then
  exec "$ROOT/bin/grain-desktop-bin" "$@"
fi
echo "Grain.app not found at $APP — run: just desktop-build" >&2
exit 1
