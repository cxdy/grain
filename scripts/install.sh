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
    *) die "unsupported OS: $u (need darwin or linux; native Windows is not a grain host)" ;;
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

# Default MCP listen (must match internal/config Defaults).
MCP_LISTEN_DEFAULT="127.0.0.1:7476"

grain_config_path() {
  printf '%s\n' "${HOME}/.grain/config.yaml"
}

# Enable mcp.enabled in ~/.grain/config.yaml (create or merge).
enable_mcp_config() {
  local cfg dir
  dir="${HOME}/.grain"
  cfg="$(grain_config_path)"
  mkdir -p "$dir" || die "cannot create ${dir}"

  if [[ ! -f "$cfg" ]]; then
    cat >"$cfg" <<EOF
# written by grain install.sh
mcp:
  enabled: true
  listen: ${MCP_LISTEN_DEFAULT}
EOF
    ok "MCP enabled by default (${cfg})"
    info "grain up will serve MCP at http://${MCP_LISTEN_DEFAULT}/mcp"
    return 0
  fi

  # Existing config: prefer Python for a safe merge when available.
  if command -v python3 >/dev/null 2>&1; then
    if python3 - "$cfg" "$MCP_LISTEN_DEFAULT" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
listen = sys.argv[2]
text = path.read_text(encoding="utf-8")
lines = text.splitlines(keepends=True)
out = []
i = 0
in_mcp = False
saw_mcp = False
saw_enabled = False
while i < len(lines):
    line = lines[i]
    stripped = line.lstrip()
    indent = len(line) - len(stripped)
    if stripped.startswith("mcp:") and (indent == 0 or not in_mcp):
        saw_mcp = True
        in_mcp = True
        out.append(line if line.endswith("\n") else line + "\n")
        i += 1
        # consume mcp block (indented more than mcp key)
        mcp_indent = indent
        block = []
        while i < len(lines):
            l2 = lines[i]
            if l2.strip() == "":
                block.append(l2)
                i += 1
                continue
            ind2 = len(l2) - len(l2.lstrip())
            if ind2 <= mcp_indent and l2.lstrip() and not l2.lstrip().startswith("#"):
                break
            if l2.lstrip().startswith("enabled:"):
                saw_enabled = True
                prefix = l2[: len(l2) - len(l2.lstrip())]
                block.append(f"{prefix}enabled: true\n")
            else:
                block.append(l2 if l2.endswith("\n") else l2 + "\n")
            i += 1
        if not saw_enabled:
            block.insert(0, "  enabled: true\n")
        # ensure listen present
        if not any(b.lstrip().startswith("listen:") for b in block):
            block.append(f"  listen: {listen}\n")
        out.extend(block)
        in_mcp = False
        continue
    out.append(line if line.endswith("\n") else line + "\n")
    i += 1
if not saw_mcp:
    if out and not str(out[-1]).endswith("\n"):
        out[-1] = str(out[-1]) + "\n"
    if out and out[-1].strip() != "":
        out.append("\n")
    out.append("mcp:\n")
    out.append("  enabled: true\n")
    out.append(f"  listen: {listen}\n")
path.write_text("".join(out), encoding="utf-8")
PY
    then
      ok "MCP enabled by default (${cfg})"
      info "grain up will serve MCP at http://${MCP_LISTEN_DEFAULT}/mcp"
      return 0
    fi
    warn "could not merge MCP into ${cfg}; appending section"
  fi

  # Fallback: append if no top-level mcp: key.
  if grep -qE '^mcp:' "$cfg" 2>/dev/null; then
    if grep -qE '^[[:space:]]+enabled:' "$cfg" 2>/dev/null; then
      # Best-effort: flip first enabled under file (may match other keys; rare).
      if sed -i.bak -E 's/^([[:space:]]+enabled:)[[:space:]]*.*/\1 true/' "$cfg" 2>/dev/null; then
        rm -f "${cfg}.bak"
        ok "MCP enabled by default (${cfg})"
        return 0
      fi
    fi
    warn "existing mcp section in ${cfg}; set enabled: true manually"
    print_mcp_enable_later
    return 0
  fi
  {
    printf '\n'
    printf 'mcp:\n'
    printf '  enabled: true\n'
    printf '  listen: %s\n' "$MCP_LISTEN_DEFAULT"
  } >>"$cfg"
  ok "MCP enabled by default (${cfg})"
  info "grain up will serve MCP at http://${MCP_LISTEN_DEFAULT}/mcp"
}

print_mcp_enable_later() {
  printf '\n'
  info "MCP was not enabled by default. Turn it on anytime:"
  printf '  grain up --mcp\n'
  printf '  # or in %s:\n' "$(grain_config_path)"
  printf '  # mcp:\n'
  printf '  #   enabled: true\n'
  printf '  #   listen: %s\n' "$MCP_LISTEN_DEFAULT"
  printf '  # docs: https://grainvm.com/guides/mcp/\n'
}

# Prompt (or honor GRAIN_ENABLE_MCP) for default MCP on grain up.
# Skip entirely when ~/.grain/config.yaml already exists (updates / reinstalls).
maybe_configure_mcp() {
  local cfg reply=""
  cfg="$(grain_config_path)"

  # Existing config ⇒ user already set up grain; never prompt or rewrite MCP.
  if [[ -f "$cfg" ]]; then
    return 0
  fi

  # Non-interactive override for CI / curl automation.
  case "${GRAIN_ENABLE_MCP:-}" in
    1|true|yes|y|Y)
      enable_mcp_config
      return 0
      ;;
    0|false|no|n|N)
      print_mcp_enable_later
      return 0
      ;;
  esac

  printf '\n'
  printf '%s\n' "${BOLD}MCP (Model Context Protocol)${RESET}"
  printf '  Expose sandboxes to coding agents (Claude Code, Codex, …).\n'
  printf '  When enabled, grain up serves MCP at http://%s/mcp\n' "$MCP_LISTEN_DEFAULT"

  # curl|bash has no stdin for answers — use the controlling terminal when present.
  if [[ -r /dev/tty ]]; then
    printf '  Enable MCP by default on grain up? [y/N] ' >/dev/tty
    # shellcheck disable=SC2162
    read -r reply </dev/tty || reply=""
  elif [[ -t 0 ]]; then
    printf '  Enable MCP by default on grain up? [y/N] '
    # shellcheck disable=SC2162
    read -r reply || reply=""
  else
    info "non-interactive install — skipping MCP prompt (set GRAIN_ENABLE_MCP=1 to enable)"
    print_mcp_enable_later
    return 0
  fi

  case "$(printf '%s' "$reply" | tr '[:upper:]' '[:lower:]')" in
    y|yes)
      enable_mcp_config
      ;;
    *)
      print_mcp_enable_later
      ;;
  esac
}

# Short post-install summary (no "Next steps" wall — updates and reinstalls stay quiet).
print_install_summary() {
  local dest_dir="$1"
  local dest="${dest_dir}/${BIN_NAME}"
  local ver=""

  # Prefer newly installed binary for version string.
  if [[ -x "$dest" ]]; then
    ver="$("$dest" version 2>/dev/null | head -1 | tr -d '\r' || true)"
  elif command -v grain >/dev/null 2>&1; then
    ver="$(grain version 2>/dev/null | head -1 | tr -d '\r' || true)"
  fi
  # "grain version" may print "grain 0.6.1" or just "0.6.1" / "v0.6.1".
  if [[ -n "$ver" ]]; then
    ver="$(printf '%s' "$ver" | sed -E 's/^grain[[:space:]]+//I; s/^v//')"
    printf '\n'
    printf '%s\n' "${BOLD}grain ${ver} installed${RESET}"
  else
    printf '\n'
    printf '%s\n' "${BOLD}grain installed${RESET}"
  fi

  if command -v grain >/dev/null 2>&1; then
    ok "grain is on PATH: $(command -v grain)"
  elif [[ -x "$dest" ]]; then
    # Install dir may not be on PATH yet for this shell.
    ok "installed ${dest}"
    warn "grain is not on PATH yet — add: export PATH=\"${dest_dir}:\$PATH\""
  else
    warn "grain is not on PATH yet"
    printf '  add to your shell profile:\n'
    printf '    export PATH="%s:$PATH"\n' "$dest_dir"
    printf '  binary: %s\n' "$dest"
  fi

  # First-time only (no config yet): optional MCP prompt.
  maybe_configure_mcp
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
    print_install_summary "$dest_dir"
    return 0
  fi

  warn "release install unavailable — trying go install fallback"
  if install_from_go "$dest_dir"; then
    print_install_summary "$dest_dir"
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
