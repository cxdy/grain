# Contributing to grain

Thanks for helping. grain is free and open source ([Apache-2.0](LICENSE)). Docs live at **[grainvm.com](https://grainvm.com)**; this repo’s `docs/` is the Jekyll source.

Also read [SECURITY.md](SECURITY.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Platforms

**Supported:** macOS and Linux (amd64 / arm64).

**Not supported:** Windows and WSL / WSL2. The installer rejects them; nested QEMU inside WSL is not a supported configuration. From a Windows machine, use the remote CLI/SDK against a Linux or macOS host if needed ([remote host guide](https://grainvm.com/guides/remote-host/)).

## Prerequisites

- **Go 1.25+** (see [`.tool-versions`](.tool-versions); [mise](https://mise.jdx.dev/) recommended)
- **[just](https://github.com/casey/just)**
- **QEMU** for real VMs (`brew install qemu` on macOS; `qemu-system` + `qemu-img` on Linux)
- Optional: **pre-commit**, **golangci-lint** (also listed in `.tool-versions`)

```bash
just init          # mise install (if available) + pre-commit hooks
just test          # unit tests (mock hypervisor — no QEMU required)
just smoke-api     # CLI + daemon e2e without QEMU
just build
just lint          # golangci-lint
./bin/grain doctor # dependency check (QEMU, image, optional agent binary)
```

Unit tests use a **mock hypervisor**. Live QEMU is optional for day-to-day development; run it when you change boot, networking, or guest paths.

Commits should follow [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, …). With hooks installed, `commit-msg` is checked via commitizen.

### Guest agent binary

Before deploying the agent to a non-golden image, or before `grain act` on images without a baked-in agent, build Linux agent binaries:

```bash
just build && just agent-linux
# produces bin/grain-agent-linux-amd64 and bin/grain-agent-linux-arm64
```

Prefer `grain image pull grain-ubuntu` (golden from tag `golden-latest`) so creates skip SSH agent deploy and default to `--wait agent`.

## Coding norms

- Keep the **CLI short** (`new`, `sh`, `x`, `rm`, …). Prefer flags over subcommand sprawl.
- **Product copy:** no competitor name-calling. Describe what grain does; do not trash other tools in UI strings, CLI help, or user-facing docs.
- **SDKs stay thin HTTP clients** — no imports of `internal/`:
  - Go: [`client/`](client/)
  - TypeScript: [`sdk/ts`](sdk/ts) (`@cxdy/grain`)
  - Python: [`sdk/python`](sdk/python) (`grainvm`, `import grain`)
- Match existing style: concise comments, table-driven tests, clear error messages.

## Pull requests

- Include **tests** for behavior changes (`just test`; `just smoke-api` when touching CLI/daemon glue).
- Update **docs** (README and/or `docs/`) when the change is user-facing.
- Keep PRs focused; note risk (hypervisor, API auth, install script) in the description.
- Do not commit large binaries or accidental golden bake artifacts under `dist/` unless that is the intentional subject of the PR.

## Local workflow

```bash
just test && just build
./bin/grain doctor
./bin/grain up
./bin/grain image pull grain-ubuntu   # once
./bin/grain new && ./bin/grain sh
./bin/grain down
```

Troubleshooting: [guides/troubleshooting](https://grainvm.com/guides/troubleshooting/). Security model: [explain/security](https://grainvm.com/explain/security/).

## Release

Maintainer checklist: [docs/RELEASE.md](docs/RELEASE.md).  
How-to: [docs/developer/releasing.md](docs/developer/releasing.md).

```bash
brew install just svu
just version          # preview: svu current / svu next
just release-tag      # create + push next semver tag (triggers GoReleaser)
just release-build    # snapshot tarballs under dist/ (no publish)
```

Golden images (`golden-latest`) use the separate bake workflow, not GoReleaser.

## License

By contributing, you agree that your contributions are licensed under the Apache License 2.0.
