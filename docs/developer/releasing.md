---
title: Releasing
description: How maintainers cut semver tags with svu and GoReleaser.
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
   - Runs `svu next` (for example `v0.1.1`)
   - Creates the git tag
   - Pushes `HEAD` and the tag to `origin`

4. Watch **Release** (GoReleaser) on the tag — publishes `grain_*.tar.gz`,
   `grain-agent-linux-*.tar.gz`, and `checksums.txt`.

Do **not** create release tags by hand unless fixing a one-off; prefer
`just release-tag` so `svu` and commit history stay aligned.

### Manual equivalent

```bash
git fetch --tags
TAG=$(svu next)
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

## Install after release

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
grain version
```

See also the maintainer checklist in [docs/RELEASE.md](https://github.com/cxdy/grain/blob/main/docs/RELEASE.md).
