# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`grain act`** — run [nektos/act](https://github.com/nektos/act) inside an ephemeral microVM (`--preset act`: Docker Engine + act); mounts the project at `/work`, waits for docker/act, streams the run, deletes the sandbox unless `--keep`. Recipe: [guides/recipes/act](https://grainvm.com/guides/recipes/act/).
- **Python client SDK** — `sdk/python` (`cxdy-grain`, `import grain`); stdlib-only TCP/Unix socket client with create/stream, exec, lifecycle, forwards, and guest fs/cp. Docs: [reference/python-sdk](https://grainvm.com/reference/python-sdk/).
- **Guest agent (`grain-agent`)** — in-guest HTTP server for health, streaming/buffered exec, interactive shell (PTY), file copy, filesystem ops, stats, and secret materialization; deploy over SSH when missing; optional vsock transport with TCP hostfwd fallback.
- **Create wait modes** — `auto` (default: agent when image HasAgent, else ssh), `ssh`, `agent`, `userdata`.
- **Golden image `grain-ubuntu`** — bake scripts, CI bake workflow, pull from GitHub Release tag `golden-latest` with companion `.sha256` sidecars; minimal cloud-init seed for agent-ready clones; auto default when local Ready.
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

### Changed

- Default image selection prefers local **`grain-ubuntu`** when Ready (`image: auto`).
- Create readiness auto-selects **agent wait** for golden / HasAgent images.
- Release binaries can ship the guest agent for multi-arch deploys.

### Explicitly deferred (out of scope for v0.1)

- macOS menu-bar tray app, Rosetta x86_64 guests, GPU/PCI passthrough
- Full HA multi-node cluster networking (single-node k3s preset only)
- Production-hardened Firecracker (still experimental)
- Measured sub-second marketing claims without hardware bench (use `scripts/bench-create.sh`)
- Windows host and **WSL / WSL2** (macOS + Linux only; use remote API/SDK from Windows if needed)

### Notes

- v0.1.0-dev journey: control plane → cloud images & SSH → guest agent → golden bake/publish → proxy/secrets → Firecracker spike → multi-distro + TS SDK polish.
