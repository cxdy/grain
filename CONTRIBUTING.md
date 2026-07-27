# Contributing to grain

Thanks for helping. grain is free and open source ([Apache-2.0](LICENSE)). Docs live at **[grainvm.com](https://grainvm.com)**; this repo’s `docs/` is the Jekyll source.

Also read [SECURITY.md](SECURITY.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Platforms

**Supported:** macOS and Linux (amd64 / arm64).

**Not supported:** Windows and WSL / WSL2. The installer rejects them; nested QEMU inside WSL is not a supported configuration. From a Windows machine, use the remote CLI/SDK against a Linux or macOS host if needed ([remote host guide](https://grainvm.com/guides/remote-host/)).

## Prerequisites

- **Go 1.23+**
- **QEMU** for real VMs (`brew install qemu` on macOS; `qemu-system` + `qemu-img` on Linux)
- **make**

```bash
make test          # unit tests (mock hypervisor — no QEMU required)
make smoke-api     # CLI + daemon e2e without QEMU
make build
./bin/grain doctor # dependency check (QEMU, image, optional agent binary)
```

Unit tests use a **mock hypervisor**. Live QEMU is optional for day-to-day development; run it when you change boot, networking, or guest paths.

### Guest agent binary

Before deploying the agent to a non-golden image, or before `grain act` on images without a baked-in agent, build Linux agent binaries:

```bash
make build agent-linux
# produces bin/grain-agent-linux-amd64 and bin/grain-agent-linux-arm64
```

Prefer `grain image pull grain-ubuntu` (golden from tag `golden-latest`) so creates skip SSH agent deploy and default to `--wait agent`.

## Coding norms

- Keep the **CLI short** (`new`, `sh`, `x`, `rm`, …). Prefer flags over subcommand sprawl.
- **Product copy:** no competitor name-calling. Describe what grain does; do not trash other tools in UI strings, CLI help, or user-facing docs.
- **SDKs stay thin HTTP clients** — no imports of `internal/`:
  - Go: [`client/`](client/)
  - TypeScript: [`sdk/ts`](sdk/ts) (`@cxdy/grain`)
  - Python: [`sdk/python`](sdk/python) (`cxdy-grain`)
- Match existing style: concise comments, table-driven tests, clear error messages.

## Pull requests

- Include **tests** for behavior changes (`make test`; `make smoke-api` when touching CLI/daemon glue).
- Update **docs** (README and/or `docs/`) when the change is user-facing.
- Keep PRs focused; note risk (hypervisor, API auth, install script) in the description.
- Do not commit large binaries or accidental golden bake artifacts under `dist/` unless that is the intentional subject of the PR.

## Local workflow

```bash
make test && make build
./bin/grain doctor
./bin/grain up
./bin/grain image pull grain-ubuntu   # once
./bin/grain new && ./bin/grain sh
./bin/grain down
```

Troubleshooting: [guides/troubleshooting](https://grainvm.com/guides/troubleshooting/). Security model: [explain/security](https://grainvm.com/explain/security/).

## Release

Maintainer checklist: [docs/RELEASE.md](docs/RELEASE.md).

GitHub Releases for `v*` tags are produced by **GoReleaser** (`.goreleaser.yaml`, grex-style workflow). Snapshot locally without publishing:

```bash
go install github.com/goreleaser/goreleaser/v2@latest
make release-build   # dist/*.tar.gz + checksums.txt
```

Golden images (`golden-latest`) use the separate bake workflow, not GoReleaser.

## License

By contributing, you agree that your contributions are licensed under the Apache License 2.0.
