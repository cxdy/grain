#!/usr/bin/env bash
# grain install script
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
#
# Installs the grain CLI to /usr/local/bin (if writable) or ~/.local/bin.
# Prefers a GitHub release binary; falls back to `go install` when Go is present.

set -euo pipefail

REPO="cxdy/grain"
BIN_NAME="grain"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
GITHUB_RELEASES="https://github.com/${REPO}/releases"

# --- colors (optional) --------------------------------------------------------
if [[ -t 1 ]]; then
  BOLD=$'\033[1m'
  DIM=$'\033[2m'
  GREEN=$'\033[32m'
  YELLOW=$'\033[33m'
  RED=$'\033[31m'
  RESET=$'\033[0m'
else
  BOLD= DIM= GREEN= YELLOW= RED= RESET=
fi

info()  { printf '%s\n' "${DIM}>${RESET} $*"; }
ok()    { printf '%s\n' "${GREEN}✓${RESET} $*"; }
warn()  { printf '%s\n' "${YELLOW}!${RESET} $*"; }
die()   { printf '%s\n' "${RED}error:${RESET} $*" >&2; exit 1; }

# --- OS / arch ----------------------------------------------------------------
detect_os() {
  local u
  u="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$u" in
    darwin|linux) echo "$u" ;;
    *) die "unsupported OS: $u (need darwin or linux; Windows/WSL are not supported)" ;;
  esac
}

detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "unsupported arch: $m (need amd64 or arm64)" ;;
  esac
}

# --- install dir --------------------------------------------------------------
pick_install_dir() {
  if [[ -n "${GRAIN_INSTALL_DIR:-}" ]]; then
    echo "$GRAIN_INSTALL_DIR"
    return
  fi
  if [[ -w /usr/local/bin ]] || [[ -w /usr/local ]]; then
    echo "/usr/local/bin"
    return
  fi
  # Try with sudo later only if needed; prefer user dir when /usr/local not writable.
  if [[ ! -w /usr/local/bin ]] 2>/dev/null; then
    echo "${HOME}/.local/bin"
    return
  fi
  echo "/usr/local/bin"
}

ensure_dir() {
  local d="$1"
  if [[ -d "$d" ]]; then
    return
  fi
  mkdir -p "$d" || die "cannot create install dir: $d"
}

need_sudo_for() {
  local dest="$1"
  local dir
  dir="$(dirname "$dest")"
  if [[ -w "$dir" ]]; then
    return 1
  fi
  return 0
}

install_file() {
  local src="$1"
  local dest="$2"
  chmod +x "$src"
  if need_sudo_for "$dest"; then
    if command -v sudo >/dev/null 2>&1; then
      info "installing to ${dest} (sudo)"
      sudo install -m 0755 "$src" "$dest"
    else
      die "cannot write ${dest}; re-run with write access or set GRAIN_INSTALL_DIR=~/.local/bin"
    fi
  else
    install -m 0755 "$src" "$dest" 2>/dev/null || cp "$src" "$dest" && chmod 0755 "$dest"
  fi
}

# --- download helpers ---------------------------------------------------------
have_curl()  { command -v curl  >/dev/null 2>&1; }
have_wget()  { command -v wget  >/dev/null 2>&1; }
have_go()    { command -v go    >/dev/null 2>&1; }

download() {
  local url="$1"
  local out="$2"
  if have_curl; then
    curl -fsSL --connect-timeout 15 --max-time 300 -o "$out" "$url"
  elif have_wget; then
    wget -q -O "$out" "$url"
  else
    die "need curl or wget to download release binaries"
  fi
}

# Fetch latest release asset URL by exact asset name.
# GoReleaser assets (preferred):
#   grain_<os>_<arch>.tar.gz
#   grain-agent-linux-<arch>.tar.gz
# Legacy bare binaries (v0.1.0): grain_<os>_<arch>, grain-agent-linux-<arch>
latest_asset_url_named() {
  local asset="$1"
  local json=""

  if have_curl; then
    json="$(curl -fsSL --connect-timeout 10 --max-time 30 \
      -H "Accept: application/vnd.github+json" \
      -H "User-Agent: grain-install" \
      "${GITHUB_API}" 2>/dev/null || true)"
  elif have_wget; then
    json="$(wget -q -O - \
      --header="Accept: application/vnd.github+json" \
      --header="User-Agent: grain-install" \
      "${GITHUB_API}" 2>/dev/null || true)"
  fi

  if [[ -z "$json" ]]; then
    return 1
  fi

  local url=""
  if command -v jq >/dev/null 2>&1; then
    url="$(printf '%s' "$json" | jq -r --arg a "$asset" \
      '.assets[] | select(.name == $a) | .browser_download_url' 2>/dev/null | head -1)"
  elif command -v python3 >/dev/null 2>&1; then
    url="$(printf '%s' "$json" | python3 -c "
import json,sys
data=json.load(sys.stdin)
name=sys.argv[1]
for a in data.get('assets') or []:
    if a.get('name')==name:
        print(a.get('browser_download_url') or '')
        break
" "$asset" 2>/dev/null || true)"
  else
    url="$(printf '%s' "$json" | tr '"' '\n' | grep -E "https://.*/${asset}\$" | head -1 || true)"
  fi

  if [[ -z "$url" || "$url" == "null" ]]; then
    return 1
  fi
  printf '%s' "$url"
}

# Extract a single binary from a .tar.gz (GoReleaser archive) into dest path.
extract_binary_from_tarball() {
  local tarball="$1"
  local member="$2" # e.g. grain or grain-agent
  local dest="$3"
  local tmpdir
  tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t grain.XXXXXX)"
  # shellcheck disable=SC2064
  trap "rm -rf '$tmpdir'" RETURN
  if ! tar -xzf "$tarball" -C "$tmpdir" 2>/dev/null; then
    return 1
  fi
  local found=""
  # Prefer exact member name at root, else find by basename.
  if [[ -f "${tmpdir}/${member}" ]]; then
    found="${tmpdir}/${member}"
  else
    found="$(find "$tmpdir" -type f -name "$member" 2>/dev/null | head -1 || true)"
  fi
  if [[ -z "$found" || ! -f "$found" ]]; then
    return 1
  fi
  install_file "$found" "$dest"
  return 0
}

looks_like_binary_or_archive() {
  local f="$1"
  [[ -s "$f" ]] || return 1
  if head -c 100 "$f" | grep -qi '<html\|<!doctype'; then
    return 1
  fi
  return 0
}

latest_asset_url() {
  local os="$1" arch="$2"
  # Prefer GoReleaser tarball, then bare binary (older releases).
  if latest_asset_url_named "grain_${os}_${arch}.tar.gz"; then
    return 0
  fi
  latest_asset_url_named "grain_${os}_${arch}"
}

# --- install paths ------------------------------------------------------------
install_from_release() {
  local os="$1" arch="$2" dest_dir="$3"
  local url name
  info "looking up latest GitHub release for ${os}/${arch}…"
  if ! url="$(latest_asset_url "$os" "$arch")"; then
    warn "no release binary found for grain_${os}_${arch}(.tar.gz)"
    return 1
  fi
  name="$(basename "$url")"
  info "downloading ${url}"
  local tmp
  tmp="$(mktemp -t grain.XXXXXX 2>/dev/null || mktemp)"
  # shellcheck disable=SC2064
  trap "rm -f '$tmp'" RETURN
  if ! download "$url" "$tmp"; then
    warn "download failed"
    return 1
  fi
  if ! looks_like_binary_or_archive "$tmp"; then
    warn "download looks empty or like HTML, not a release asset"
    return 1
  fi
  ensure_dir "$dest_dir"
  local dest="${dest_dir}/${BIN_NAME}"
  case "$name" in
    *.tar.gz|*.tgz)
      if ! extract_binary_from_tarball "$tmp" "grain" "$dest"; then
        warn "failed to extract grain from ${name}"
        return 1
      fi
      ;;
    *)
      install_file "$tmp" "$dest"
      ;;
  esac
  ok "installed ${dest}"
  return 0
}

# Install guest agent binary for SSH deploy into VMs (linux arch matching host).
install_agent_from_release() {
  local arch="$1"
  local asset_tar="grain-agent-linux-${arch}.tar.gz"
  local asset_bin="grain-agent-linux-${arch}"
  local agent_dir="${HOME}/.grain/agent"
  local url name
  info "looking up guest agent for ${arch}…"
  if url="$(latest_asset_url_named "$asset_tar")"; then
    :
  elif url="$(latest_asset_url_named "$asset_bin")"; then
    :
  else
    warn "no release asset ${asset_tar} or ${asset_bin} (run just agent-linux for local dev)"
    return 1
  fi
  name="$(basename "$url")"
  info "downloading ${url}"
  local tmp
  tmp="$(mktemp -t grain-agent.XXXXXX 2>/dev/null || mktemp)"
  # shellcheck disable=SC2064
  trap "rm -f '$tmp'" RETURN
  if ! download "$url" "$tmp"; then
    warn "agent download failed"
    return 1
  fi
  if ! looks_like_binary_or_archive "$tmp"; then
    warn "agent download empty or invalid"
    return 1
  fi
  ensure_dir "$agent_dir"
  local dest="${agent_dir}/${asset_bin}"
  case "$name" in
    *.tar.gz|*.tgz)
      # Archive contains binary "grain-agent"; install as grain-agent-linux-$arch.
      if ! extract_binary_from_tarball "$tmp" "grain-agent" "$dest"; then
        warn "failed to extract grain-agent from ${name}"
        return 1
      fi
      ;;
    *)
      install_file "$tmp" "$dest"
      ;;
  esac
  ok "installed guest agent ${dest}"
  return 0
}

install_from_go() {
  local dest_dir="$1"
  if ! have_go; then
    return 1
  fi
  info "installing via go install github.com/${REPO}/cmd/grain@latest"
  # go install puts the binary in GOBIN or GOPATH/bin
  local gobin
  gobin="$(go env GOBIN 2>/dev/null || true)"
  if [[ -z "$gobin" ]]; then
    gobin="$(go env GOPATH 2>/dev/null)/bin"
  fi
  GO111MODULE=on go install "github.com/${REPO}/cmd/grain@latest"
  local src="${gobin}/${BIN_NAME}"
  if [[ ! -x "$src" ]]; then
    warn "go install finished but ${src} not found"
    return 1
  fi
  # If go already installed into our preferred dir, done.
  if [[ "$(cd "$(dirname "$src")" && pwd)" == "$(cd "$dest_dir" 2>/dev/null && pwd)" ]]; then
    ok "installed ${src}"
    return 0
  fi
  ensure_dir "$dest_dir"
  local dest="${dest_dir}/${BIN_NAME}"
  # Only copy if dest differs
  if [[ "$src" != "$dest" ]]; then
    install_file "$src" "$dest"
    ok "installed ${dest} (from go install)"
  else
    ok "installed ${src}"
  fi
  return 0
}

print_next_steps() {
  local dest_dir="$1"
  local dest="${dest_dir}/${BIN_NAME}"
  printf '\n'
  printf '%s\n' "${BOLD}grain installed${RESET}"
  if command -v grain >/dev/null 2>&1; then
    ok "grain is on PATH: $(command -v grain)"
    grain version 2>/dev/null || true
  else
    warn "grain is not on PATH yet"
    printf '  add to your shell profile:\n'
    printf '    export PATH="%s:$PATH"\n' "$dest_dir"
    printf '  binary: %s\n' "$dest"
  fi
  printf '\n'
  printf '%s\n' "${BOLD}Next steps${RESET}"
  printf '  1. Install QEMU (required for real VMs):\n'
  case "$(detect_os)" in
    darwin)
      printf '       brew install qemu\n'
      printf '       # ensure Homebrew is on PATH (Apple Silicon often needs):\n'
      printf '       #   eval "$(/opt/homebrew/bin/brew shellenv)"\n'
      ;;
    linux)
      if command -v apt-get >/dev/null 2>&1; then
        printf '       sudo apt-get install -y qemu-system qemu-utils\n'
      elif command -v dnf >/dev/null 2>&1; then
        printf '       sudo dnf install -y qemu-system-x86 qemu-img\n'
      else
        printf '       install qemu-system and qemu-img for your distro\n'
      fi
      ;;
  esac
  printf '  2. Verify dependencies:\n'
  printf '       grain doctor\n'
  printf '  3. Start the daemon and create a sandbox:\n'
  printf '       grain up\n'
  printf '       grain image pull grain-ubuntu   # agent-ready golden image\n'
  printf '       grain new && grain sh\n'
  printf '  4. Optional workloads:\n'
  printf '       grain act -- -l                 # GitHub Actions via act in a microVM\n'
  printf '       grain new --preset k3s -n lab -p\n'
  printf '\n'
  printf 'Guest agent for non-golden images is under ~/.grain/agent/\n'
  printf 'Docs:    https://grainvm.com\n'
  printf 'act:     https://grainvm.com/guides/recipes/act/\n'
  printf 'k3s:     https://grainvm.com/guides/recipes/k3s/\n'
  printf 'Uninstall: grain uninstall   # or: grain uninstall --purge -y\n'
}

# --- main ---------------------------------------------------------------------
main() {
  printf '%s\n' "${BOLD}grain installer${RESET}"
  local os arch dest_dir
  os="$(detect_os)"
  arch="$(detect_arch)"
  dest_dir="$(pick_install_dir)"
  info "os=${os} arch=${arch} install_dir=${dest_dir}"

  if install_from_release "$os" "$arch" "$dest_dir"; then
    install_agent_from_release "$arch" || true
    print_next_steps "$dest_dir"
    return 0
  fi

  warn "release install unavailable — trying go install fallback"
  if install_from_go "$dest_dir"; then
    print_next_steps "$dest_dir"
    return 0
  fi

  cat >&2 <<EOF
${RED}error:${RESET} could not install grain.

Options:
  1. Download a binary from ${GITHUB_RELEASES}
     (look for grain_${os}_${arch}), chmod +x, move to PATH
  2. Install Go 1.23+ and run:
       go install github.com/${REPO}/cmd/grain@latest
  3. Build from source:
       git clone https://github.com/${REPO}.git && cd grain && just build
EOF
  exit 1
}

main "$@"
