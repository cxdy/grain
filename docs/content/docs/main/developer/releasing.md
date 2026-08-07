---
title: Releasing
description: How maintainers cut semver tags with svu and GoReleaser.
section: developer
---

grain uses [semantic versioning](https://semver.org/) driven by
[Conventional Commits](https://www.conventionalcommits.org/). Maintainers cut
releases by pushing a version tag; [GoReleaser](https://goreleaser.com/) builds
CLI and guest-agent archives from that tag.

## Prerequisites

- Push access to `cxdy/grain`
- Clean working tree on the commit you intend to ship, CI green
- [`just`](https://github.com/casey/just) and [`svu`](https://github.com/caarlos0/svu):

  ```bash
  brew install just svu
  # or:
  go install github.com/caarlos0/svu/v3@latest
  ```

## How the version is chosen

[`svu next`](https://github.com/caarlos0/svu) inspects git history since the
latest tag:

| Commit shape | Version bump |
|--------------|--------------|
| `fix: …` (no breaking) | **patch** |
| `feat: …` (no breaking) | **minor** |
| `BREAKING CHANGE:` footer, or `type!:` | **major** |
| `chore:`, `docs:`, `ci:`, `test:`, … | often **no bump** (same as current) |

Preview without tagging:

```bash
just version
# or:
svu current
svu next
```

## Cut a release

1. Ensure the commit you want is on `main` (or checkout that SHA).
2. Confirm the next tag:

   ```bash
   just version
   ```

3. Create and push the tag (requires a **clean** working tree):

   ```bash
   just release-tag
   ```

   That recipe:

   - Fetches remote tags so `svu` sees the latest release
   - Runs `svu next` (for example `v0.3.1`)
   - **Updates Hugo docs version metadata** (live content stays
     `docs/content/docs/main/` → `/docs/main/`; sets switcher label to
     `vX.Y.Z (latest)` and lists product SVU tags as GitHub **commit** links —
     no per-release content trees; see issue #88)
   - Creates the git tag on that commit
   - Pushes `HEAD` and the tag to `origin`

4. Watch **Release** (GoReleaser) on the tag — publishes `grain_*.tar.gz`,
   `grain-agent-linux-*.tar.gz`, and `checksums.txt`.

5. Watch **Release Desktop** — matrix builds Wails Desktop and **uploads** to the
   same tag: `Grain_darwin_<arch>.app.tar.gz`, `grain-desktop_linux_<arch>.tar.gz`.
   Waits for the GoReleaser release to exist, then attaches assets. Re-run via
   workflow_dispatch if a leg fails.
   On macOS, if these Actions secrets are set, the app is **Developer ID signed +
   notarized + stapled**: `APPLE_TEAM_ID`, `MACOS_CERTIFICATE` (base64 `.p12`),
   `MACOS_CERTIFICATE_PWD`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID`,
   `APPLE_API_KEY` (base64 `.p8` or PEM). Without secrets, assets are ad-hoc signed.

6. Watch **Pages** — deploys grainvm.com with the new docs label + switcher.

Do **not** create release tags by hand unless fixing a one-off; prefer
`just release-tag` so `svu`, docs version, and commit history stay aligned.

### Docs version only (no tag)

```bash
just docs-version 0.3.1
# review, then commit docs/hugo.toml only (no docs/content/docs/<ver>/ tree)
```

### Safety net

If a tag is cut without `just release-tag`, the **Docs version** GitHub Actions
workflow (on `release: published`) opens a PR that runs the same bump script.

### Manual equivalent

```bash
git fetch --tags
TAG=$(svu next)
VER="${TAG#v}"
./scripts/docs-version-bump.sh "$VER"
git add docs/hugo.toml
git commit -m "docs: publish ${TAG} site version"   # if there are changes
git tag "$TAG"
git push origin HEAD "$TAG"
```

## What the release pipeline publishes

Configuration: [`.goreleaser.yaml`](https://github.com/cxdy/grain/blob/main/.goreleaser.yaml).
Workflow: [`.github/workflows/release.yml`](https://github.com/cxdy/grain/blob/main/.github/workflows/release.yml).

| Artifact | Destination |
|----------|-------------|
| CLI archives `grain_<os>_<arch>.tar.gz` | GitHub Release |
| Guest agent `grain-agent-linux-<arch>.tar.gz` | GitHub Release |
| `checksums.txt` | GitHub Release |

**Golden images** (`grain-ubuntu` on tag `golden-latest`) are **not** part of
GoReleaser — use the bake workflow / local bake scripts.

## Python SDK (`grainvm`) — PyPI Trusted Publishing

The Python client lives in [`sdk/python`](https://github.com/cxdy/grain/tree/main/sdk/python)
and publishes as [`grainvm`](https://pypi.org/project/grainvm/) via
[Trusted Publishing](https://docs.pypi.org/trusted-publishers/) (OIDC). No
long-lived PyPI API token is stored in GitHub secrets.

Import remains `import grain`. Workflow:
[`.github/workflows/publish-python.yml`](https://github.com/cxdy/grain/blob/main/.github/workflows/publish-python.yml).

### One-time setup

1. **GitHub** — create an Actions environment named **`pypi`** on `cxdy/grain`
   (Settings → Environments → New environment). Optional: require reviewers.
2. **PyPI** — create or open project **`grainvm`**, then **Publishing** → add a GitHub publisher:

   | Field | Value |
   |-------|--------|
   | Owner | `cxdy` |
   | Repository | `grain` |
   | Workflow name | `publish-python.yml` |
   | Environment name | `pypi` |

   Direct link (after the project exists): [pypi.org/manage/project/grainvm/settings/publishing/](https://pypi.org/manage/project/grainvm/settings/publishing/)

### Publish a new version

1. Bump `version` in [`sdk/python/pyproject.toml`](https://github.com/cxdy/grain/blob/main/sdk/python/pyproject.toml)
   and update the package README if needed. Merge to `main`.
2. Either:

   ```bash
   # Tag-driven (version must match pyproject, e.g. 0.2.0 → sdk-python-v0.2.0)
   git tag sdk-python-v0.2.0
   git push origin sdk-python-v0.2.0
   ```

   or run **Actions → Publish Python SDK → Run workflow** (`workflow_dispatch`).

3. Confirm the run on GitHub Actions and the new files on
   [pypi.org/project/grainvm](https://pypi.org/project/grainvm/).

## TypeScript SDK (`@cxdy/grain`) — npm Trusted Publishing

The TypeScript client lives in [`sdk/ts`](https://github.com/cxdy/grain/tree/main/sdk/ts)
and publishes as [`@cxdy/grain`](https://www.npmjs.com/package/@cxdy/grain) via
[Trusted Publishing](https://docs.npmjs.com/trusted-publishers/) (OIDC). No
long-lived npm token is stored in GitHub secrets.

Workflow: [`.github/workflows/publish-npm.yml`](https://github.com/cxdy/grain/blob/main/.github/workflows/publish-npm.yml).

Requires **Node ≥ 22.14** and **npm CLI ≥ 11.5.1** on the runner (the workflow
installs a current npm).

### One-time setup

1. **GitHub** — create an Actions environment named **`npm`** on `cxdy/grain`
   (Settings → Environments → New environment). Optional: require reviewers.
2. **npm** — open the package settings → **Trusted Publisher** → GitHub Actions:

   | Field | Value |
   |-------|--------|
   | Organization or user | `cxdy` |
   | Repository | `grain` |
   | Workflow filename | `publish-npm.yml` |
   | Environment name | `npm` |
   | Allowed actions | `npm publish` |

   Package page: [npmjs.com/package/@cxdy/grain](https://www.npmjs.com/package/@cxdy/grain)
   (manage access / settings as owner).

3. Optional hardening after the first successful OIDC publish: package
   **Settings → Publishing access** → require 2FA and disallow long-lived tokens.

### Publish a new version

1. Bump `version` in [`sdk/ts/package.json`](https://github.com/cxdy/grain/blob/main/sdk/ts/package.json)
   (and lockfile if needed). Merge to `main`.
2. Either:

   ```bash
   # Tag-driven (version must match package.json, e.g. 0.2.0 → sdk-ts-v0.2.0)
   git tag sdk-ts-v0.2.0
   git push origin sdk-ts-v0.2.0
   ```

   or run **Actions → Publish TypeScript SDK → Run workflow** (`workflow_dispatch`).

3. Confirm the run on GitHub Actions and the new version on
   [npmjs.com/package/@cxdy/grain](https://www.npmjs.com/package/@cxdy/grain).

## Install after release

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
grain version
```

See also the maintainer checklist in [docs/RELEASE.md](https://github.com/cxdy/grain/blob/main/docs/RELEASE.md).
