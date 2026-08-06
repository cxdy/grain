#!/usr/bin/env bash
# Update the Hugo docs version switcher for a product release WITHOUT copying
# per-release content trees (see GitHub issue #88).
#
# Live docs are a single tree: docs/content/docs/main/ → site path /docs/main/.
# Historical product SVU tags (vX.Y.Z only; not fc/golden/sdk/guest-agent tags)
# appear in the switcher as GitHub commit links (source at that tag).
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

# Refuse to reintroduce per-release content trees.
if [[ -d "docs/content/docs/${VER}" ]]; then
  echo "error: found legacy tree docs/content/docs/${VER} — remove it (issue #88: no per-release content copies)" >&2
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
    return [(v, found[v][0], found[v][1]) for v in ordered]


# Patch default docs labels (path slug stays main).
text, n1 = re.subn(
    r'(docsVersion\s*=\s*")[^"]*(")',
    r'\g<1>main\2',
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
    # Prefer live_commit for this version.
    tags = [(v, n, live_commit if v == ver else c) for v, n, c in tags]


def fmt_live(label: str, commit: str) -> str:
    lines = [
        "  [[params.docsVersions]]\n",
        '    version = "main"\n',
        f'    label = "{label}"\n',
        '    path = "/docs/main/"\n',
        "    live = true\n",
    ]
    if commit:
        lines.append(f'    commit = "{commit}"\n')
    return "".join(lines)


def fmt_external(version: str, commit: str, tag_name: str) -> str:
    ref = commit or tag_name
    path = f"{github}/tree/{ref}"
    lines = [
        "  [[params.docsVersions]]\n",
        f'    version = "{version}"\n',
        f'    label = "v{version}"\n',
        f'    path = "{path}"\n',
        "    external = true\n",
    ]
    if commit:
        lines.append(f'    commit = "{commit}"\n')
    return "".join(lines)


parts = [fmt_live(f"v{ver} (latest)", live_commit)]
for v, name, commit in tags:
    # Historical rows for all product tags (including current) as commit view.
    parts.append(fmt_external(v, commit, name))
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
print(f"updated {hugo}: live=/docs/main/ label=v{ver}, {len(tags)} product tag(s) as commit links")
PY

echo "docs-version-bump: done (live docs = main, latest label v${VER})"
