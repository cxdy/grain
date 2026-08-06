#!/usr/bin/env bash
# Build Grain Desktop and package a GitHub Release asset for the current host.
#
# macOS → dist/release/Grain_darwin_<arch>.app.tar.gz  (contains Grain.app/)
# Linux → dist/release/grain-desktop_linux_<arch>.tar.gz  (contains grain-desktop)
#
# Usage (from repo root, after deps installed):
#   ./scripts/package-desktop-release.sh
#   DESKTOP_VERSION=0.8.0 ./scripts/package-desktop-release.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch_raw="$(uname -m)"
case "$arch_raw" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *)
    echo "unsupported arch: $arch_raw" >&2
    exit 1
    ;;
esac

version="${DESKTOP_VERSION:-${GITHUB_REF_NAME:-dev}}"
version="${version#v}"

echo "package-desktop-release: os=${os} arch=${arch} version=${version}"

command -v wails >/dev/null 2>&1 || {
  echo "installing wails CLI…"
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  export PATH="${PATH}:$(go env GOPATH)/bin"
}

# Inject product version into wails.json when present (best-effort).
if [[ -f desktop/wails.json ]] && command -v python3 >/dev/null 2>&1; then
  python3 - "$version" <<'PY'
import json, sys
from pathlib import Path
ver = sys.argv[1]
p = Path("desktop/wails.json")
data = json.loads(p.read_text())
info = data.setdefault("info", {})
info["productVersion"] = ver
p.write_text(json.dumps(data, indent=2) + "\n")
print(f"wails.json productVersion={ver}")
PY
fi

just desktop-build

outdir="${ROOT}/dist/release"
mkdir -p "$outdir"
staging="$(mktemp -d "${TMPDIR:-/tmp}/grain-desktop-pkg.XXXXXX")"
cleanup() { rm -rf "$staging"; }
trap cleanup EXIT

if [[ "$os" == "darwin" ]]; then
  app="${ROOT}/desktop/build/bin/Grain.app"
  if [[ ! -d "$app" ]]; then
    # Bare binary fallback — wrap is not a real .app but install path expects .app.
    if [[ -x "${ROOT}/bin/grain-desktop-bin" ]]; then
      echo "warning: no Grain.app; packaging bare binary as grain-desktop_darwin_${arch}.tar.gz" >&2
      mkdir -p "$staging"
      cp -f "${ROOT}/bin/grain-desktop-bin" "$staging/grain-desktop"
      chmod +x "$staging/grain-desktop"
      asset="grain-desktop_darwin_${arch}.tar.gz"
      tar -czf "${outdir}/${asset}" -C "$staging" grain-desktop
      echo "wrote ${outdir}/${asset}"
      ls -la "${outdir}/${asset}"
      exit 0
    fi
    echo "error: desktop/build/bin/Grain.app not found after build" >&2
    exit 1
  fi
  # Tar the .app directory (preserve structure for install.sh).
  cp -R "$app" "$staging/Grain.app"
  asset="Grain_darwin_${arch}.app.tar.gz"
  tar -czf "${outdir}/${asset}" -C "$staging" Grain.app
  echo "wrote ${outdir}/${asset}"
  ls -la "${outdir}/${asset}"
  exit 0
fi

if [[ "$os" == "linux" ]]; then
  bin=""
  for cand in \
    "${ROOT}/bin/grain-desktop-bin" \
    "${ROOT}/desktop/build/bin/Grain" \
    "${ROOT}/desktop/build/bin/grain-desktop"; do
    if [[ -x "$cand" ]]; then
      bin="$cand"
      break
    fi
  done
  if [[ -z "$bin" ]]; then
    echo "error: no Linux desktop binary after build" >&2
    exit 1
  fi
  cp -f "$bin" "$staging/grain-desktop"
  chmod +x "$staging/grain-desktop"
  # Ship a short README for runtime deps.
  cat >"$staging/README-desktop.txt" <<EOF
Grain Desktop ${version} (linux/${arch})

Runtime: WebKitGTK 4.1 (e.g. Debian/Ubuntu: libwebkit2gtk-4.1-0 libgtk-3-0)

  chmod +x grain-desktop
  ./grain-desktop

Requires grain CLI/daemon: grain up
Docs: https://grainvm.com/docs/main/guides/desktop/
EOF
  asset="grain-desktop_linux_${arch}.tar.gz"
  tar -czf "${outdir}/${asset}" -C "$staging" grain-desktop README-desktop.txt
  echo "wrote ${outdir}/${asset}"
  ls -la "${outdir}/${asset}"
  exit 0
fi

echo "unsupported OS: $os" >&2
exit 1
