#!/usr/bin/env bash
# Snapshot docs/content/docs/main → docs/content/docs/<version> and make that
# version the site default / switcher "latest" in docs/hugo.toml.
#
# Each version entry records the git commit for tag v<version> (or HEAD if the
# tag is not present yet) so the site can link "source at this version" without
# relying forever on full duplicated content trees as the only model.
#
# Usage:
#   ./scripts/docs-version-bump.sh 0.3.1
#   ./scripts/docs-version-bump.sh v0.3.1
#   just docs-version 0.3.1
#
# Idempotent: re-running for the same version refreshes the tree from main
# and rewrites hugo.toml. Older semver trees under docs/content/docs/ stay
# listed in the switcher (newest first).
#
# Optional (future): drop content trees and build from `git archive` / checkout
# of the recorded commit at publish time — commit metadata is already here.
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
DST="docs/content/docs/${VER}"
HUGO="docs/hugo.toml"

if [[ ! -d "$SRC" ]]; then
  echo "error: missing $SRC (Hugo docs main tree)" >&2
  exit 1
fi
if [[ ! -f "$HUGO" ]]; then
  echo "error: missing $HUGO" >&2
  exit 1
fi

echo "docs-version-bump: publishing docs version ${VER}"

rm -rf "$DST"
cp -R "$SRC" "$DST"

# Rewrite absolute versioned links inside the snapshot to this release.
while IFS= read -r -d '' f; do
  sed -i.bak \
    -e "s|/docs/main/|/docs/${VER}/|g" \
    -e "s|/docs/0\\.2\\.2/|/docs/${VER}/|g" \
    -e "s|grainvm.com/docs/main|grainvm.com/docs/${VER}|g" \
    -e "s|grainvm.com/docs/0\\.2\\.2|grainvm.com/docs/${VER}|g" \
    "$f"
  rm -f "${f}.bak"
done < <(find "$DST" -type f -name '*.md' -print0)

# Version landing page (overwrite main-branch wording).
cat >"${DST}/_index.md" <<EOF
---
title: Documentation
description: grain ${VER} documentation.
version: "${VER}"
---

Welcome to the grain **v${VER}** docs. Use the sidebar to browse Learn, MCP, Guides, Reference, and Explain pages.
EOF

# Update hugo.toml defaults + switcher list.
python3 - "$VER" "$HUGO" <<'PY'
import re
import sys
from pathlib import Path

ver = sys.argv[1]
hugo = Path(sys.argv[2])
text = hugo.read_text()
repo = hugo.resolve().parent.parent
content_docs = repo / "docs" / "content" / "docs"

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

# Discover historical versions from content trees (semver dir names).
semver_dirs = []
if content_docs.is_dir():
    for p in content_docs.iterdir():
        if p.is_dir() and re.fullmatch(r"\d+\.\d+\.\d+", p.name):
            semver_dirs.append(p.name)

others = sorted(
    (v for v in semver_dirs if v != ver),
    key=lambda s: tuple(int(x) for x in s.split(".")),
    reverse=True,
)
ordered = [ver] + others


def git_commit_for(version: str) -> str:
    """Resolve full SHA for tag vX.Y.Z, tag X.Y.Z, branch, or HEAD."""
    import subprocess

    candidates = [f"v{version}", version, "HEAD"]
    if version == "main":
        candidates = ["main", "HEAD"]
    for ref in candidates:
        try:
            out = subprocess.check_output(
                ["git", "rev-parse", ref],
                cwd=str(repo),
                stderr=subprocess.DEVNULL,
                text=True,
            ).strip()
            if out:
                return out
        except (subprocess.CalledProcessError, FileNotFoundError):
            continue
    return ""


def fmt_block(version: str, label: str, path_s: str, commit: str) -> str:
    lines = [
        "  [[params.docsVersions]]\n",
        f'    version = "{version}"\n',
        f'    label = "{label}"\n',
        f'    path = "{path_s}"\n',
    ]
    if commit:
        lines.append(f'    commit = "{commit}"\n')
    return "".join(lines)


parts = []
default_commit = git_commit_for(ver)
for i, v in enumerate(ordered):
    label = f"v{v} (latest)" if i == 0 else f"v{v}"
    parts.append(fmt_block(v, label, f"/docs/{v}/", git_commit_for(v)))
parts.append(fmt_block("main", "main (bleeding edge)", "/docs/main/", git_commit_for("main")))
new_block = "".join(parts)

# Default commit for marketing pages (matches latest released docs).
text, n3 = re.subn(
    r'(docsVersionCommit\s*=\s*")[^"]*(")',
    rf"\g<1>{default_commit}\2",
    text,
    count=1,
)
if n3 == 0 and default_commit:
    # Insert after docsVersionLabel if missing.
    text, n3 = re.subn(
        r'(docsVersionLabel\s*=\s*"[^"]*"\n)',
        rf'\g<1>  docsVersionCommit = "{default_commit}"\n',
        text,
        count=1,
    )

# Each block: optional indent + [[params.docsVersions]] + key = value lines only.
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
print(f"updated {hugo}: docsVersion={ver}, {len(ordered)} release(s) + main")
PY

echo "docs-version-bump: wrote ${DST} and updated ${HUGO}"
echo "docs-version-bump: done (v${VER})"
