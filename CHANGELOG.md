# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- **Image pull fail-closed digests** — production catalog pulls refuse install when neither a pinned `SHA256` nor a companion `.sha256` sidecar is available (`grain-ubuntu` sidecar required; no silent skip). `ubuntu-cloud` digests refreshed to current noble minimal SHA256SUMS; `alpine-cloud` pins SHA-256 of the published qcow2 (Alpine ships `.sha512`/`.asc` only). Spec field `AllowUnverified` is for tests/dev only.
- **Remote API transport guidance** — CLI prints a one-time stderr warning when dialing a non-loopback cleartext `http://` API URL (Bearer tokens are sniffable); silence with `GRAIN_INSECURE_HTTP=1`. Daemon non-loopback bind warning mentions cleartext HTTP. Docs (`SECURITY.md`, remote-lab, config) recommend SSH tunnel to `127.0.0.1` or HTTPS reverse proxy. `https://` API URLs use the default TLS client.


### Added

- **`grain clone SRC DST` / `grain new --clone SRC`** — offline clone of a **stopped persistent** VM: copy root disk + meta under a new name (left stopped; SSH/agent and hostfwd host ports allocated on next start). API: `POST /vms/{name}/clone` with body `{"name":"dst"}`. Refuses running/paused and ephemeral VMs. Limitations: qcow2 overlays keep their backing chain; guest hostname may still match the source; live SSH forwards are not copied.
- **`grain fwd tunnel [name]`** — print ready-to-run `ssh -N -L HOSTPORT:127.0.0.1:HOSTPORT` lines for a VM's published SLIRP and live host ports (daemon host loopback). Flags: `--host`, `--user`, `--json`; default host from `GRAIN_SSH_HOST` or `USER@HOST` placeholder. See [Remote lab](https://grainvm.com/guides/remote-lab/).
- **Builtin profile `remote-coding`** — durable remote lab defaults (`persistent`, 4 CPU / 8192 MiB / 32 GiB, `grain-ubuntu`) without editing config; user `profiles:` with the same name override. CLI root help lists `sync`, `agent deploy`, and the profile.
- **`grain sync push | pull`** — unidirectional incremental host↔guest directory sync via the guest agent (local dial + remote `GRAIN_API` proxy). Host-side baselines under `data_dir/sync/`; `--delete` / `--dry-run` / `--force` / ignore flags; exit `2` on conflicts with zero applies. MCP: `grain_sync_push` / `grain_sync_pull`.
- **`grain agent deploy [name]`** — SCP/install or refresh `grain-agent` in a running guest over SSH. Local CLI SCPs directly; remote CLI (`GRAIN_API`) calls `POST /vms/{name}/agent/deploy` so deploy runs on the daemon host (agent binary must exist there). Docs cover reverse `cp`, remote tunnel + `-p` labs, and agent refresh.
- **Sandbox recipes** — portable YAML (`apiVersion: grain/v1`, `kind: Sandbox`) for create options + ordered bootstrap steps that implement the readiness protocol. CLI: `grain new --recipe file.yaml`, `grain recipe validate|show`. Examples: `examples/recipes/`. Docs: [Sandbox recipes](https://grainvm.com/docs/main/get-started/recipe/).
- **Host clipboard via OSC 52 on `grain sh`** — intercept OSC 52 (and tmux DCS-wrapped) sequences from the guest PTY and copy to the local clipboard (`pbcopy` / `wl-copy` / `xclip` / `xsel`) so tools like Grok Build can copy on highlight inside a sandbox. Disable with `GRAIN_OSC52_CLIPBOARD=0`.
- **`grain sh` forwards host terminal env** — `TERM` / `TERM_PROGRAM` / `COLORTERM` (and locale) into the guest PTY so TUIs can negotiate keyboard protocols the same as local sessions (Shift+Enter newlines in Grok Build, etc.).
- **Guest readiness protocol** — custom images/bootstrap write `/var/lib/grain/readiness/*`; agent `GET /health` includes `readiness` and `GET /readiness`; create wait mode `bootstrap` polls until `state=ready` (fails on `failed`/timeout, VM left running). CLI: `grain status`, create progress surfaces guest messages. Docs: [Readiness protocol](https://grainvm.com/docs/0.3.0/explain/readiness/). Helper: `scripts/grain-ready-report.sh`.

### Fixed

- **Firecracker doctor** — when `hypervisor: firecracker`, `grain doctor` hard-fails if `/dev/kvm` is missing or not RDWR-accessible (plus a soft nested-virt CPU flag hint).
- **Firecracker start errors** — if Firecracker dies right after opening its API socket (typical missing-KVM path), create returns `firecracker exited immediately` with the log tail and a KVM hint, instead of a misleading later `vsock … unreachable` agent wait error.
- **MCP in the main binary** — `grain up --mcp` / config `mcp.enabled` + `mcp.listen` (default `127.0.0.1:7476/mcp` Streamable HTTP); `grain mcp` for stdio IDE hosts. Guide: [MCP server](https://grainvm.com/guides/mcp/).
- **Install script MCP prompt** — asks whether to enable MCP by default in `~/.grain/config.yaml` (`GRAIN_ENABLE_MCP=1|0` for non-interactive); declining prints `grain up --mcp` and config snippets.
- **Expanded MCP tools** — streaming `grain_exec` (timeout, progress); write/read file + tar; fs readdir/stat/mkdir/rm; agent_health, logs, stats; workspace_sandbox helper; live port forwards; image list/pull; `grain_act` (GitHub Actions via act); `grain_k3s` lab; create defaults `grain-ubuntu` + `wait=agent`; idempotent delete.

- **`grain update`** — check GitHub Releases for a newer CLI and re-run the install script (`--check` report-only with exit 1 when outdated; `--force` reinstalls even when current).
- **Upgrade notices** — most commands may print a one-line stderr hint when a newer release is known (24h cache under `~/.grain/cache/`). Disable with `check_updates: false`, `GRAIN_CHECK_UPDATES=0`, or `GRAIN_NO_UPDATE_CHECK=1`.

### Removed

- **`grain tray`** — menu bar / system tray helper. It required a CGO-enabled build (not available in portable release archives) or a separate binary; status and lifecycle stay on the CLI and API.

## [0.2.2] - 2026-07-27

### Added

- **`grain uninstall`** — stop the local daemon, remove the CLI binary, and optionally purge the data directory with `--purge` (`-y` skips the prompt). Default keeps VMs and images; always drops the installer agent cache and runtime socket/pid files.

### Fixed

- **`grain up`** — detects a healthy existing daemon (`already up`) instead of spawning a second process that races a leftover foreground instance. Dead pid / orphan socket are cleaned before start; background start waits for healthz or child exit. Setsid so shell Ctrl+C does not signal the daemon.
- **`grain down`** — cleans stale runtime files and SIGKILLs a stuck daemon when needed.
- **Coverage comment workflow** — runs only for fork PRs (same-repo PRs already post coverage from `ci.yml`).

### Changed

- Broader unit-test coverage and consolidated `*_test.go` names (CI Cobertura threshold unchanged).

## [0.2.1] - 2026-07-27

### Fixed

- **`grain new` create wait** — for agent-ready images (`grain-ubuntu` / HasAgent), wait the full readiness budget for the guest agent instead of falling back to SSH after ~45s. SSH deploy is still used when needed, but agent health is raced so a late-up agent finishes create without hanging on SSH.
- **Ctrl+C during create** — no longer marks a live VM as `status=error`. The guest stays **running** so `grain sh` / `grain ls` work. `grain ls` also reconciles stale `error`/`creating` to `running` when the hypervisor process is still alive.

## [0.2.0] - 2026-07-27

### Added

- **`grain tray`** — macOS menu bar / Linux system tray status helper.
- **Guest arch** — `grain new --arch arm64|amd64` (and config `guest_arch`). Cross-arch on Apple Silicon uses QEMU TCG for x86_64 guests.
- **Virtio GPU** — `grain new --gpu virtio` / config `gpu: virtio` adds `virtio-gpu-pci`.
- **Overlay network** — `grain new --network overlay` / config `network: overlay` shares an L2 multicast segment between VMs (plus SLIRP for hostfwd).
- Guides: tray, multi-arch, GPU, overlay networking. Product surface page cleaned up (no competitor comparison / deferred-feature table).
- Dev tooling: mise (`.tool-versions`), pre-commit, golangci-lint, markdownlint-cli2, commitizen; `just init` / `just lint` / `just pre-commit`.
- CI coverage: `just coverage` writes Cobertura `coverage.xml`; PR comment posts a compact summary with the full per-file table under a collapsible details block (75% line minimum; cmd/tray excluded).
- Broad unit-test expansion across client, API, manager, hypervisor, agent, CLI helpers, and related packages.
- Interactive **OpenAPI explorer** on the docs site (`/reference/openapi/`).
- **Field Manual** docs site redesign (layout, navigation, and visual system).

### Changed

- **Python SDK** PyPI name is **`grainvm`** (`pip install grainvm`; import still `import grain`). Previous name `cxdy-grain` is not used for new publishes.
- TypeScript SDK **`@cxdy/grain`** and Python SDK **`grainvm`** published as **0.2.0** (aligned with the CLI tag).
- Docs site visual refresh (typography and cool ink/mint/steel palette); OpenAPI explorer readable in light mode.

### Fixed

- Release builds no longer fail on the tray dependency when `CGO_ENABLED=0` (tray is a CGO-only build path).
- OpenAPI YAML descriptions quoted so Swagger UI can parse the published spec.

## [0.1.4] - 2026-07-27

### Fixed

- **`grain act`** no longer dies on first job run waiting for act’s interactive Large/Medium/Micro image prompt. The act preset and `grain act` seed a non-interactive `actrc` (medium-class `catthehacker/ubuntu` images).

## [0.1.3] - 2026-07-27

### Fixed

- **Daemon start** fails fast if the TCP API address is already bound (no half-up process that only holds the unix socket).

## [0.1.2] - 2026-07-27

### Fixed

- **Install script** next-step links pointed at a non-existent `docs/recipes` path; they now open grainvm.com (docs, act, k3s).
- **QEMU discovery** — doctor and the QEMU runtime resolve `qemu-system-*` / `qemu-img` from Homebrew locations (`/opt/homebrew/bin`, `/usr/local/bin`) when those dirs are not on `PATH`.

### Changed

- Installer “next steps” mention `grain act` and the k3s preset.
- GitHub **issue templates** (bug / feature) and **CODEOWNERS**.

## [0.1.1] - 2026-07-27

### Added

- Published SDKs: npm `@cxdy/grain`, PyPI `cxdy-grain`; Go client via `github.com/cxdy/grain/client`.
- Trusted-publishing workflows for npm and PyPI.
- Product docs: quick start, act/k3s homepage surfaces, multi-scenario demo.

### Changed

- GoReleaser archives (`.tar.gz`) as primary install assets; install script supports them.

## [0.1.0] - 2026-07-27

First public release: local Linux microVM control plane for macOS and Linux.

### Added

- **Community docs** — [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md); site page [get-started/contributing](https://grainvm.com/get-started/contributing/); maintainer [docs/RELEASE.md](docs/RELEASE.md) (v0.1.0 checklist).
- **`grain act`** — run [nektos/act](https://github.com/nektos/act) inside an ephemeral microVM (`--preset act`: Docker Engine + act); mounts the project at `/work`, waits for docker/act, streams the run, deletes the sandbox unless `--keep`. Recipe: [guides/recipes/act](https://grainvm.com/guides/recipes/act/).
- **Python client SDK** — `sdk/python` (`cxdy-grain`, `import grain`); stdlib-only TCP/Unix socket client with create/stream, exec, lifecycle, forwards, and guest fs/cp. Docs: [reference/python-sdk](https://grainvm.com/reference/python-sdk/).
- **Guest agent (`grain-agent`)** — in-guest HTTP server for health, streaming/buffered exec, interactive shell (PTY), file copy, filesystem ops, stats, and secret materialization; deploy over SSH when missing; optional vsock transport with TCP hostfwd fallback.
- **Create wait modes** — `auto` (default: agent when image HasAgent, else ssh), `ssh`, `agent`, `userdata`.
- **Golden image `grain-ubuntu`** — bake scripts, CI bake workflow, pull from GitHub Release tag `golden-latest` with companion `.sha256` sidecars (arm64 + amd64); minimal cloud-init seed for agent-ready clones; auto default when local Ready.
- **Multi-distro catalog** — `ubuntu-cloud` (Ubuntu 24.04 minimal) and `alpine-cloud` (Alpine generic UEFI + cloud-init; SSH user `alpine`).
- **Host egress proxy** — default-deny allowlist (`proxy up/down/allow/deny/ls/client`) with optional Authorization inject from host secrets; `new --proxy` for guest `HTTPS_PROXY`.
- **Secrets store** — host-side `secret ls|set|rm|inject` under `~/.grain/secrets`; agent materialize into the guest.
- **Guest stats** — `grain stats` / agent `GET /stats` (uptime, mem, load).
- **Suspend / restore** — stop QEMU and free RAM for persistent VMs; restore from disk or savevm snapshot when available.
- **Pause / resume** — QMP freeze/unfreeze guest vCPUs without tearing down the process.
- **Live port forwards** — `fwd ls|add|rm` plus create-time `--publish`.
- **Virtio-9p mounts** — `-v HOST:GUEST` (and virtiofs on Linux when virtiofsd is available).
- **Profiles & presets** — named create profiles in config; embedded `docker`, `k3s`, and `act` userdata presets.
- **OpenAPI + Go client SDK** — `api/openapi.yaml`, optional Bearer `api_token` / `GRAIN_TOKEN`, public `github.com/cxdy/grain/client`.
- **TypeScript client SDK** — `sdk/ts` (`@cxdy/grain`) thin fetch client for Node automation.
- **Experimental Firecracker** backend on Linux (raw rootfs, vsock agent, known limits documented).
- **Boot benchmark** — `scripts/bench-create.sh` times N creates and prints p50/p95/avg.
- Install script, recipes (coding-agent, k3s, docker-socket, ci-ephemeral, act), grainvm.com Diátaxis site, and feature docs (agent, images, proxy, firecracker, mounts, networking, profiles).
- **Remote sandbox host guide** — run grain as a systemd service for a team box (API token, caps, SSH tunnels, SDKs); example unit under `deploy/systemd/`.
- **Remote CLI** — `GRAIN_API` / `--api` / config `api_url` dial a remote daemon over HTTP; agent shell/exec/cp/fs proxy through the API; non-loopback API bind requires `api_token` (daemon refuses insecure open binds).

### Changed

- Default image selection prefers local **`grain-ubuntu`** when Ready (`image: auto`).
- Create readiness auto-selects **agent wait** for golden / HasAgent images.
- Release binaries can ship the guest agent for multi-arch deploys.
- **WaitAgent** short-probes then SSH-deploys instead of spending the full create timeout probing a missing agent (fixes `grain act` / non-golden `--wait agent` hangs).

### Fixed

- QEMU **OVMF** for amd64 cloud images; CI bake installs OVMF + ISO tools.
- Agent transport **auto** requires a *writable* `/dev/vhost-vsock` (fixes GitHub Actions golden bake Permission denied).
- Golden bake prep commands run entirely in-guest via `sh -c`.

### Explicitly deferred (out of scope for v0.1)

- macOS menu-bar tray app, Rosetta x86_64 guests, GPU/PCI passthrough
- Full HA multi-node cluster networking (single-node k3s preset only)
- Production-hardened Firecracker (still experimental)
- Measured sub-second marketing claims without hardware bench (use `scripts/bench-create.sh`)
- Windows host and **WSL / WSL2** (macOS + Linux only; use remote API/SDK from Windows if needed)

### Notes

- Platforms: **macOS and Linux** only (amd64 / arm64). Golden images: `golden-latest` on GitHub Releases.
- Install: `curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash` (or release assets for `v0.1.0`).

[Unreleased]: https://github.com/cxdy/grain/compare/v0.2.2...HEAD
[0.2.2]: https://github.com/cxdy/grain/releases/tag/v0.2.2
[0.2.1]: https://github.com/cxdy/grain/releases/tag/v0.2.1
[0.2.0]: https://github.com/cxdy/grain/releases/tag/v0.2.0
[0.1.4]: https://github.com/cxdy/grain/releases/tag/v0.1.4
[0.1.3]: https://github.com/cxdy/grain/releases/tag/v0.1.3
[0.1.2]: https://github.com/cxdy/grain/releases/tag/v0.1.2
[0.1.1]: https://github.com/cxdy/grain/releases/tag/v0.1.1
[0.1.0]: https://github.com/cxdy/grain/releases/tag/v0.1.0
