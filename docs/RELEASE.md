# Release checklist — v0.2.0

Practical gates before tagging **v0.2.0**. Do not start a long golden bake from this document alone; use existing bake/CI workflows when images need refresh.

## Binaries & golden

- [x] **arm64 + amd64 golden** published and pullable on tag **`golden-latest`**
- [ ] Release binaries for macOS/Linux × amd64/arm64 build cleanly from the **v0.2.0** tag (GoReleaser on tag push)

## Community & legal

- [x] **CONTRIBUTING.md**, **SECURITY.md**, and **CODE_OF_CONDUCT.md** present at repo root
- [x] **Repo public** (github.com/cxdy/grain)
- [x] LICENSE remains **Apache-2.0** (do not relicense for this cut)

## Version & changelog

- [x] **CHANGELOG**: **`[0.2.0]`** dated with tray, multi-arch, GPU, overlay net, tooling, coverage, OpenAPI explorer, Field Manual redesign; empty **`[Unreleased]`**
- [x] **SDK versions** set to **0.2.0** (`sdk/ts/package.json`, `sdk/python/pyproject.toml`)
- [x] **OpenAPI** `info.version` **0.2.0** (`api/openapi.yaml` + `docs/assets/openapi.yaml`)
- [ ] Tag **`v0.2.0`** (CLI + agent via GoReleaser)
- [ ] Tag **`sdk-ts-v0.2.0`** and **`sdk-python-v0.2.0`** (or `workflow_dispatch` publish workflows)

## Packages

- [ ] **npm** [`@cxdy/grain@0.2.0`](https://www.npmjs.com/package/@cxdy/grain) from `sdk/ts` (publish-npm.yml / tag `sdk-ts-v0.2.0`)
- [ ] **PyPI** [`grainvm==0.2.0`](https://pypi.org/project/grainvm/) from `sdk/python` (publish-python.yml / tag `sdk-python-v0.2.0`)

## Smoke (human or scripted)

Run on a clean macOS or Linux machine (or fresh PATH without a dirty `~/.grain` if testing install):

- [ ] **`scripts/install.sh`** installs a working `grain` on PATH (latest release)
- [ ] **`grain doctor`** passes (QEMU present)
- [ ] **`grain image pull grain-ubuntu`** succeeds (golden-latest)
- [ ] **`grain act -- -l`** lists workflows (ephemeral sandbox; agent-ready golden preferred)

## Docs accuracy

- [x] **No WSL / Windows host claims** — platforms are macOS + Linux only
- [x] **Remote host** guide accurate: non-loopback API requires `api_token`; never open `0.0.0.0` without a token ([guides/remote-host](https://grainvm.com/guides/remote-host/))
- [x] Site version strings and demo output show **0.2.0**
- [ ] Site/README install and first-sandbox paths match the release

## Explicitly out of scope for v0.2

See CHANGELOG deferred notes under earlier 0.1 lines for historical context. Do not block 0.2.0 on multi-node HA, production Firecracker, or Windows/WSL.

## After tag

- [ ] GitHub Release notes summarize highlights + link grainvm.com
- [ ] Confirm install one-liner pulls **v0.2.0**
- [ ] Confirm npm `@cxdy/grain@0.2.0` and PyPI `grainvm==0.2.0` are live
- [ ] Announce (optional); watch issues for install/doctor/act/k3s regressions
