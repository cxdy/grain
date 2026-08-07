#!/usr/bin/env bash
# Materialize versioned docs trees at *build time* from product release tags.
#
# Issue #88: do NOT commit per-release copies under docs/content/docs/0.x.y/.
# Pages/CI (and local `just docs` if desired) run this before `hugo` so the site
# still has /docs/0.8.0/, /docs/0.7.0/, … while git only stores docs/main/.
#
# Usage (repo root, fetch-depth: 0 required):
#   ./scripts/docs-materialize-versions.sh
#   DOCS_VERSIONS_MAX=8 ./scripts/docs-materialize-versions.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CONTENT_DOCS="docs/content/docs"
MAIN_SRC="${CONTENT_DOCS}/main"
MAX="${DOCS_VERSIONS_MAX:-12}"

if [[ ! -d "$MAIN_SRC" ]]; then
  echo "error: missing ${MAIN_SRC}" >&2
  exit 1
fi

# Product SVU tags only (mirror internal/docsver + docs-version-bump).
is_product_tag() {
  local name="$1" lower
  lower="$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    fc-*|golden-*|sdk-*|*guest-agent*|agent-*) return 1 ;;
  esac
  [[ "$name" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]
}

normalize() {
  local t="$1"
  t="${t#v}"
  t="${t#V}"
  printf '%s' "$t"
}

# List product tags newest-first.
mapfile -t ALL_TAGS < <(git tag -l 'v*' 2>/dev/null | sort -V -r || true)
PRODUCT_TAGS=()
for t in "${ALL_TAGS[@]}"; do
  is_product_tag "$t" || continue
  PRODUCT_TAGS+=("$t")
done

if [[ ${#PRODUCT_TAGS[@]} -eq 0 ]]; then
  echo "docs-materialize: no product tags; leaving only main"
  exit 0
fi

# Cap how many historical trees we build (keeps Hugo/CI fast).
if [[ ${#PRODUCT_TAGS[@]} -gt "$MAX" ]]; then
  PRODUCT_TAGS=("${PRODUCT_TAGS[@]:0:$MAX}")
fi

echo "docs-materialize: materializing ${#PRODUCT_TAGS[@]} version tree(s) from tags"

# Remove any previously materialized version dirs (keep main + _index).
shopt -s nullglob
for d in "${CONTENT_DOCS}"/*/; do
  base="$(basename "$d")"
  if [[ "$base" == "main" ]]; then
    continue
  fi
  if [[ "$base" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    rm -rf "$d"
  fi
done
shopt -u nullglob

materialize_tag() {
  local tag="$1"
  local ver
  ver="$(normalize "$tag")"
  local dest="${CONTENT_DOCS}/${ver}"
  local tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/docs-mat.XXXXXX")"

  # Prefer single-tree layout at tag (post-#88).
  if git cat-file -e "${tag}:docs/content/docs/main/_index.md" 2>/dev/null; then
    git archive "$tag" docs/content/docs/main | tar -x -C "$tmp"
    mkdir -p "$dest"
    # archive extracts full path under tmp
    if [[ -d "${tmp}/docs/content/docs/main" ]]; then
      cp -R "${tmp}/docs/content/docs/main/." "$dest/"
    else
      echo "warn: unexpected archive layout for ${tag}" >&2
      rm -rf "$tmp"
      return 1
    fi
  elif git cat-file -e "${tag}:docs/content/docs/${ver}/_index.md" 2>/dev/null; then
    # Pre-#88 snapshot trees at that tag.
    git archive "$tag" "docs/content/docs/${ver}" | tar -x -C "$tmp"
    mkdir -p "$dest"
    cp -R "${tmp}/docs/content/docs/${ver}/." "$dest/"
  else
    echo "warn: no docs tree at ${tag}; skip ${ver}" >&2
    rm -rf "$tmp"
    return 1
  fi
  rm -rf "$tmp"

  # Point absolute docs links at this version slug.
  # shellcheck disable=SC2038
  find "$dest" -type f -name '*.md' -print0 2>/dev/null \
    | xargs -0 sed -i.bak \
      -e "s|/docs/main/|/docs/${ver}/|g" \
      -e "s|grainvm.com/docs/main|grainvm.com/docs/${ver}|g" \
      2>/dev/null || true
  find "$dest" -type f -name '*.md.bak' -delete 2>/dev/null || true

  # Version landing title (best-effort).
  if [[ -f "${dest}/_index.md" ]]; then
    # Prepend/replace front matter title lightly — only if description is generic.
    :
  fi

  echo "  ok ${ver} ← ${tag}"
  return 0
}

for tag in "${PRODUCT_TAGS[@]}"; do
  materialize_tag "$tag" || true
done

echo "docs-materialize: done (git still has only main; version dirs are build artifacts)"
