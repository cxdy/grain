#!/usr/bin/env bash
# Update Hugo docs version switcher for a product release WITHOUT committing
# per-release content trees (issue #88).
#
# Git keeps a single source tree: docs/content/docs/main/.
# Switcher entries use on-site paths /docs/X.Y.Z/ (and /docs/main/ edge).
# CI runs scripts/docs-materialize-versions.sh before hugo to extract each
# tag's docs into those paths at build time; commit SHAs still show in the UI.
#
# Usage:
#   ./scripts/docs-version-bump.sh 0.3.1
#   ./scripts/docs-version-bump.sh v0.3.1
#   just docs-version 0.3.1
#
# Idempotent: re-running for the same version rewrites hugo.toml metadata only.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ $# -lt 1 || -z "${1:-}" ]]; then
  echo "usage: $0 <version>" >&2
  echo "  example: $0 0.3.1   or   $0 v0.3.1" >&2
  exit 2
fi

RAW="$1"
VER="${RAW#v}"
if [[ ! "$VER" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]; then
  echo "error: invalid version $RAW (want semver like 0.3.1)" >&2
  exit 2
fi

SRC="docs/content/docs/main"
HUGO="docs/hugo.toml"

if [[ ! -d "$SRC" ]]; then
  echo "error: missing $SRC (single live Hugo docs tree)" >&2
  exit 1
fi
if [[ ! -f "$HUGO" ]]; then
  echo "error: missing $HUGO" >&2
  exit 1
fi

# Refuse to *commit* per-release trees (build-time materialize dirs are gitignored).
if git ls-files --error-unmatch "docs/content/docs/${VER}" >/dev/null 2>&1 \
  || git ls-files "docs/content/docs/${VER}" | grep -q .; then
  echo "error: tracked per-release tree docs/content/docs/${VER} — remove from git (issue #88)" >&2
  exit 1
fi

echo "docs-version-bump: set live docs label to v${VER} (content stays ${SRC})"

python3 - "$VER" "$HUGO" <<'PY'
import re
import subprocess
import sys
from pathlib import Path

ver = sys.argv[1]
hugo = Path(sys.argv[2])
text = hugo.read_text()
repo = hugo.resolve().parent.parent
github = "https://github.com/cxdy/grain"

# Product SVU tags only (mirror internal/docsver.IsProductSVUTag).
_product = re.compile(r"(?i)^v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)$")


def is_product_svu(name: str) -> bool:
    name = (name or "").strip()
    if not name:
        return False
    lower = name.lower()
    if (
        lower.startswith("fc-")
        or lower.startswith("golden-")
        or lower.startswith("sdk-")
        or "guest-agent" in lower
        or "qemu" in lower
        or lower.startswith("agent-")
    ):
        return False
    return bool(_product.match(name))


def normalize(tag: str) -> str:
    tag = tag.strip()
    if tag.lower().startswith("v") and is_product_svu(tag):
        return tag[1:]
    return tag


def git_rev(ref: str) -> str:
    try:
        return subprocess.check_output(
            ["git", "rev-parse", ref],
            cwd=str(repo),
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return ""


def list_product_tags() -> list[tuple[str, str, str]]:
    """Return (version, tag_name, commit) newest-first."""
    try:
        out = subprocess.check_output(
            ["git", "tag", "-l"],
            cwd=str(repo),
            text=True,
        )
    except (subprocess.CalledProcessError, FileNotFoundError):
        out = ""
    found: dict[str, tuple[str, str]] = {}
    for line in out.splitlines():
        name = line.strip()
        if not is_product_svu(name):
            continue
        v = normalize(name)
        commit = git_rev(name)
        # Prefer v-prefixed tag name when both exist.
        if v not in found or name.startswith("v") or name.startswith("V"):
            found[v] = (name, commit)

    def ver_key(s: str):
        core = s.split("-", 1)[0].split("+", 1)[0]
        parts = []
        for p in core.split("."):
            n = 0
            for c in p:
                if c.isdigit():
                    n = n * 10 + int(c)
                else:
                    break
            parts.append(n)
        while len(parts) < 3:
            parts.append(0)
        return tuple(parts[:3])

    ordered = sorted(found.keys(), key=ver_key, reverse=True)
    out = []
    for v in ordered:
        name, commit = found[v]
        # Only list tags that have a docs tree (materialize will extract it).
        has_main = (
            subprocess.call(
                ["git", "cat-file", "-e", f"{name}:docs/content/docs/main/_index.md"],
                cwd=str(repo),
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            == 0
        )
        has_ver = (
            subprocess.call(
                ["git", "cat-file", "-e", f"{name}:docs/content/docs/{v}/_index.md"],
                cwd=str(repo),
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            == 0
        )
        if has_main or has_ver:
            out.append((v, name, commit))
    return out


# Default site version = latest product release slug (on-site /docs/X.Y.Z/).
# Content still lives only as docs/main/ in git; CI materializes version dirs.
text, n1 = re.subn(
    r'(docsVersion\s*=\s*")[^"]*(")',
    rf"\g<1>{ver}\2",
    text,
    count=1,
)
text, n2 = re.subn(
    r'(docsVersionLabel\s*=\s*")[^"]*(")',
    rf"\g<1>v{ver}\2",
    text,
    count=1,
)
if n1 != 1 or n2 != 1:
    raise SystemExit(f"failed to patch docsVersion fields (n1={n1} n2={n2})")

live_commit = git_rev(f"v{ver}") or git_rev(ver) or git_rev("HEAD")
text, n3 = re.subn(
    r'(docsVersionCommit\s*=\s*")[^"]*(")',
    rf"\g<1>{live_commit}\2",
    text,
    count=1,
)
if n3 == 0 and live_commit:
    text, n3 = re.subn(
        r'(docsVersionLabel\s*=\s*"[^"]*"\n)',
        rf'\g<1>  docsVersionCommit = "{live_commit}"\n',
        text,
        count=1,
    )

tags = list_product_tags()
# Ensure current version appears even if tag not created yet.
if not any(v == ver for v, _, _ in tags):
    tags = [(ver, f"v{ver}", live_commit)] + tags
else:
    tags = [(v, n, live_commit if v == ver else c) for v, n, c in tags]


def fmt_version(version: str, label: str, commit: str, *, live: bool = False) -> str:
    """On-site path /docs/<ver>/ — materialize script fills content at build time."""
    lines = [
        "  [[params.docsVersions]]\n",
        f'    version = "{version}"\n',
        f'    label = "{label}"\n',
        f'    path = "/docs/{version}/"\n',
    ]
    if live:
        lines.append("    live = true\n")
    if commit:
        lines.append(f'    commit = "{commit}"\n')
    return "".join(lines)


parts = []
# Newest first; first entry is latest release (live).
for i, (v, name, commit) in enumerate(tags):
    label = f"v{v} (latest)" if i == 0 else f"v{v}"
    parts.append(fmt_version(v, label, commit, live=(i == 0)))
# Optional edge tree always present in git.
parts.append(
    fmt_version(
        "main",
        "main (edge)",
        git_rev("HEAD") or live_commit,
        live=False,
    )
)
new_block = "".join(parts)

block_re = re.compile(
    r"^[ \t]*\[\[params\.docsVersions\]\][ \t]*\n"
    r"(?:[ \t]+[a-zA-Z_][a-zA-Z0-9_]*[ \t]*=[ \t]*[^\n]*\n)*",
    re.MULTILINE,
)
matches = list(block_re.finditer(text))
if not matches:
    raise SystemExit("no [[params.docsVersions]] blocks found in hugo.toml")
region_start = matches[0].start()
region_end = matches[-1].end()
if region_end < len(text) and text[region_end] == "\n":
    region_end += 1

text = text[:region_start] + new_block + "\n" + text[region_end:]
hugo.write_text(text)
print(f"updated {hugo}: docsVersion={ver} on-site paths, {len(tags)} version(s) + main edge")
PY

echo "docs-version-bump: done (latest /docs/${VER}/; content source remains docs/main/ in git)"
