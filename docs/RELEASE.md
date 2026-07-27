# Release checklist — v0.1.0

Practical gates before tagging **v0.1.0**. Do not start a long golden bake from this document alone; use existing bake/CI workflows when images need refresh.

## Binaries & golden

- [x] **arm64 + amd64 golden** published and pullable on tag **`golden-latest`**
- [ ] Release binaries for macOS/Linux × amd64/arm64 build cleanly from the release tag

## Community & legal

- [x] **CONTRIBUTING.md**, **SECURITY.md**, and **CODE_OF_CONDUCT.md** present at repo root
- [ ] **Repo public** (github.com/cxdy/grain)
- [x] LICENSE remains **Apache-2.0** (do not relicense for this cut)

## Version & changelog

- [x] **CHANGELOG**: move `[Unreleased]` notes into **`[0.1.0]`** with date; leave a fresh Unreleased section
- [x] **Version pins** at **0.1.0** (not `0.1.0-dev`):
  - `Makefile` `VERSION`
  - `cmd/grain` / `cmd/grain-agent` version ldflags as used by `make build`
- [ ] Tag **`v0.1.0`** and attach release binaries (and notes pointing at CHANGELOG + docs)

## Packages

- [ ] **npm** `@cxdy/grain` published from `sdk/ts` (version aligned)
- [ ] **PyPI** `cxdy-grain` published from `sdk/python` (version aligned)

## Smoke (human or scripted)

Run on a clean macOS or Linux machine (or fresh PATH without a dirty `~/.grain` if testing install):

- [ ] **`scripts/install.sh`** installs a working `grain` on PATH
- [ ] **`grain doctor`** passes (QEMU present)
- [ ] **`grain image pull grain-ubuntu`** succeeds (golden-latest)
- [ ] **`grain act -- -l`** lists workflows (ephemeral sandbox; agent-ready golden preferred)

## Docs accuracy

- [ ] **No WSL / Windows host claims** — platforms are macOS + Linux only
- [ ] **Remote host** guide accurate: non-loopback API requires `api_token`; never open `0.0.0.0` without a token ([guides/remote-host](https://grainvm.com/guides/remote-host/))
- [ ] Site/README install and first-sandbox paths match the release

## Explicitly out of scope for v0.1

See CHANGELOG “Explicitly deferred”. Do not block 0.1.0 on tray apps, multi-node HA, production Firecracker, or Windows/WSL.

## After tag

- [ ] GitHub Release notes summarize highlights + link grainvm.com
- [ ] Confirm install one-liner pulls the new tag
- [ ] Optional: announce; watch issues for install/doctor regressions
