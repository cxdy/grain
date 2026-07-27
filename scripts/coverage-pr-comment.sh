#!/usr/bin/env bash
# Post (or update) a Cobertura coverage summary as a PR comment.
# Summary is always visible; the full table is collapsed under <details>.
set -euo pipefail

COVERAGE_XML="${COVERAGE_XML:-coverage.xml}"
MIN_COVERAGE="${MIN_COVERAGE:-75}"
MARKER="<!-- grain-coverage-report -->"
REPO="${GITHUB_REPOSITORY:?}"
PR_NUMBER="${PR_NUMBER:?}"
SHA="${GITHUB_SHA:-}"
TOKEN="${GITHUB_TOKEN:?}"

if [[ ! -f "$COVERAGE_XML" ]]; then
  echo "missing $COVERAGE_XML" >&2
  exit 1
fi

BODY="$(python3 - "$COVERAGE_XML" "$MIN_COVERAGE" "$SHA" <<'PY'
import sys
import xml.etree.ElementTree as ET

path, min_cov_s, sha = sys.argv[1], sys.argv[2], sys.argv[3]
min_cov = float(min_cov_s)
root = ET.parse(path).getroot()

rows = []
covered = total = 0
for cls in root.iter("class"):
    filename = cls.attrib.get("filename", "")
    lines = list(cls.iter("line"))
    if not lines:
        continue
    c = sum(1 for ln in lines if int(ln.attrib.get("hits", 0)) > 0)
    t = len(lines)
    covered += c
    total += t
    pct = 100.0 * c / t
    # Compact missing ranges
    miss_nums = [int(ln.attrib.get("number", 0)) for ln in lines if int(ln.attrib.get("hits", 0)) == 0]
    parts = []
    i = 0
    while i < len(miss_nums):
        start = end = miss_nums[i]
        while i + 1 < len(miss_nums) and miss_nums[i + 1] == end + 1:
            i += 1
            end = miss_nums[i]
        parts.append(str(start) if start == end else f"{start}-{end}")
        i += 1
    miss_s = " ".join(parts)
    if len(miss_s) > 90:
        miss_s = miss_s[:87] + "…"
    rows.append((pct, filename, miss_s))

overall = 100.0 * covered / total if total else 0.0
ok = overall + 1e-9 >= min_cov
badge = "✅" if ok else "❌"
rows.sort(key=lambda r: (r[0], r[1]))

out = []
out.append(MARKER)
out.append(f"**Coverage** (lines): `{overall:.0f}%` {badge} — minimum `{min_cov:.0f}%`")
if sha:
    out.append("")
    out.append(f"<sub>commit `{sha[:7]}` · {covered}/{total} lines · cmd/* and tray excluded</sub>")
out.append("")
out.append("<details>")
out.append(f"<summary>Coverage report by file ({overall:.0f}% overall — click to expand)</summary>")
out.append("")
out.append("| File | Coverage | | Missing |")
out.append("| - | :-: | :-: | - |")
out.append(f"| **All files** | `{overall:.0f}%` | {badge} | |")
for pct, filename, miss in rows:
    if pct >= 99.999:
        continue
    m = "✅" if pct + 1e-9 >= min_cov else "❌"
    out.append(f"| `{filename}` | `{pct:.0f}%` | {m} | {miss} |")
out.append("")
out.append("</details>")
print("\n".join(out))
PY
)"

JSON_BODY="$(python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' <<<"$BODY")"

EXISTING="$(curl -fsSL \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/${REPO}/issues/${PR_NUMBER}/comments?per_page=100" \
  | python3 -c "
import json,sys
marker='''${MARKER}'''
for c in json.load(sys.stdin):
    if marker in (c.get('body') or ''):
        print(c['id'])
        break
")"

if [[ -n "$EXISTING" ]]; then
  curl -fsSL -X PATCH \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "Content-Type: application/json" \
    "https://api.github.com/repos/${REPO}/issues/comments/${EXISTING}" \
    -d "{\"body\": ${JSON_BODY}}" >/dev/null
  echo "Updated coverage comment #${EXISTING}"
else
  curl -fsSL -X POST \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: application/vnd.github+json" \
    -H "Content-Type: application/json" \
    "https://api.github.com/repos/${REPO}/issues/${PR_NUMBER}/comments" \
    -d "{\"body\": ${JSON_BODY}}" >/dev/null
  echo "Created coverage comment"
fi
