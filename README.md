# grain

<p align="center">
  <img src="logo.png" alt="grain logo" width="200">
</p>

<p align="center">
  <strong>Linux microVM sandboxes on your own hardware.</strong><br>
  <a href="https://grainvm.com">Documentation</a>
  ·
  <a href="https://grainvm.com/docs/main/get-started/quickstart/">Quick start</a>
  ·
  <a href="https://github.com/cxdy/grain/releases">Releases</a>
</p>

<p align="center">
  Apache-2.0
  · macOS &amp; Linux
</p>

grain runs small, disposable Linux VMs locally — for a shell, for [GitHub Actions](https://grainvm.com/docs/main/guides/recipes/act/) (`grain act`), or for a [throwaway k3s](https://grainvm.com/docs/main/guides/recipes/k3s/) lab. Ephemeral by default; persistent when you want it.

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
```

Install QEMU, then check dependencies:

```bash
# macOS
brew install qemu

# Debian / Ubuntu
# sudo apt-get install -y qemu-system qemu-utils

grain doctor
```

---

## First sandbox

```bash
grain up
grain image pull grain-ubuntu
grain new
grain sh
```

When you’re done:

```bash
grain rm
grain down
```

Optional starter config and more flags: **[quick start](https://grainvm.com/docs/main/get-started/quickstart/)**.

### Faster creates (template / warm pool)

Cold boots are guest-bound (~seconds). After a golden is agent-ready:

```bash
grain new -i grain-ubuntu -n golden -p --wait agent
grain suspend golden
grain new --from golden -n work1                 # clone + loadvm when snapshotted
# config warm_pool.template/size, then:
# grain pool fill && grain new --from-pool -n work2
```

→ [Lifecycle: create path & warm pool](https://grainvm.com/docs/main/guides/lifecycle/)

### Recipes library

Portable YAML sandboxes (`grain/v1`) live under `~/.grain/recipes`. Import never creates a VM — deploy is a separate step.

```bash
grain recipe search              # official catalog (git index)
grain recipe add python-dev      # pull one official body into the library
grain recipe add ./my-lab.yaml   # or a local file / https URL
grain new --recipe python-dev    # create from a library name
```

Official pack includes act/k3s/docker labs, `python-dev`, `go-dev`, `remote-coding`, and more. Desktop has a full Recipes tab (catalog, form builder, deploy preflight).

→ [Sandbox recipes](https://grainvm.com/docs/main/get-started/recipe/)

### Desktop (optional GUI)

Operator console for the same daemon as the CLI — not Electron, not a second engine.

| Area | Notes |
|------|--------|
| **Sandboxes** | List, search, bulk start/stop/rm, multi-host **Run…** (re-run failed / copy all) |
| **Create** | Cold · from template · warm pool (prefer claim when ready) |
| **Recipes / Images** | Library + official catalog · deploy preflight · image pull |
| **Ops** | Activity feed (CLI/MCP/API too) · warm pool Settings · doctor · multi-host switcher |

```bash
# Prefers GitHub Release Desktop assets (v0.8.0+); else build from a checkout
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash -s -- --desktop
# or: just desktop-build && ./bin/grain-desktop
```

Release assets: macOS `Grain_darwin_<arch>.app.tar.gz` → `~/Applications/Grain.app`;  
Linux `grain-desktop_linux_<arch>.tar.gz` → `grain-desktop` on your PATH.

→ [Desktop guide](https://grainvm.com/docs/main/guides/desktop/) · [desktop/README.md](desktop/README.md)

### Firecracker (Linux + KVM)

Default hypervisor is still **QEMU** (macOS and Linux). On Linux with `/dev/kvm`, Firecracker is a **supported** second backend:

| Tier | What works |
|------|------------|
| **vFC-1 agent** | Pull `fc-kernel` + `grain-ubuntu-fc`; `grain new --wait agent`; exec/shell/cp/sync over vsock UDS + `CONNECT` |
| **vFC-2 partial net** | TAP + create-time `-P` / `grain fwd` (host TCP proxy; needs CAP_NET_ADMIN). Overlay, mounts, UDP stay **QEMU-only** |

```bash
# ~/.grain/config.yaml → hypervisor: firecracker
grain image pull fc-kernel
grain image pull grain-ubuntu-fc
grain doctor
grain new -i grain-ubuntu-fc --wait agent
# optional: ./scripts/smoke-fc.sh · ./scripts/smoke-fc-net.sh
```

→ [Firecracker on Linux](https://grainvm.com/docs/main/guides/firecracker/) · [Hypervisor matrix](https://grainvm.com/docs/main/explain/hypervisor-matrix/)

### MCP (coding agents)

MCP is built into `grain` (not a separate binary):

```bash
grain up --mcp                 # daemon + MCP at http://127.0.0.1:7476/mcp
# IDE stdio host:
#   command: grain, args: ["mcp"]
```

→ [MCP server guide](https://grainvm.com/docs/main/guides/mcp/)

---

## Workloads

### GitHub Actions

Run [nektos/act](https://github.com/nektos/act) inside an isolated microVM so host Docker stays clean.

```bash
cd /path/to/your/repo
grain act -- -l          # list workflows
grain act -- -j test     # run a job
```

→ [act guide](https://grainvm.com/docs/main/guides/recipes/act/)

### k3s lab

Single-node Kubernetes with the API published to the host.

```bash
grain new --preset k3s -n lab -p --wait userdata
grain fwd ls lab         # host port → guest 6443
```

→ [k3s guide](https://grainvm.com/docs/main/guides/recipes/k3s/)

---

## Features

| Area | What you get |
|------|----------------|
| **CLI** | `up` · `new` · `sh` · `x` · `rm` · mounts · port forwards · profiles · warm pool |
| **Recipes** | Library + official catalog · `grain new --recipe` · bootstrap readiness · [guide](https://grainvm.com/docs/main/get-started/recipe/) |
| **Presets** | `act` · `k3s` · `docker` |
| **Guest agent** | Exec, shell, file copy, and fs ops without living in SSH |
| **Hypervisors** | **QEMU** default (macOS/Linux); **Firecracker** supported on Linux+KVM ([vFC-1](https://grainvm.com/docs/main/guides/firecracker/) agent + [vFC-2](https://grainvm.com/docs/main/guides/firecracker/) partial net) |
| **API** | Unix socket + optional TCP · [OpenAPI](api/openapi.yaml) |
| **SDKs** | [Go](https://pkg.go.dev/github.com/cxdy/grain/client) · [TypeScript](https://www.npmjs.com/package/@cxdy/grain) · [Python](https://pypi.org/project/grainvm/) |
| **Desktop** | Optional Wails operator console · sandboxes, recipes, warm pool, activity · [guide](https://grainvm.com/docs/main/guides/desktop/) |

---

## Docs

| Page | |
|------|--|
| [Install](https://grainvm.com/docs/main/get-started/install/) | Platforms and install options |
| [Quick start](https://grainvm.com/docs/main/get-started/quickstart/) | Config + first VM |
| [First sandbox](https://grainvm.com/docs/main/get-started/first-sandbox/) | Tutorial + interactive demo |
| [Recipes](https://grainvm.com/docs/main/get-started/recipe/) | Library, official catalog, bootstrap readiness |
| [Desktop](https://grainvm.com/docs/main/guides/desktop/) | Optional operator GUI |
| [act](https://grainvm.com/docs/main/guides/recipes/act/) | GitHub Actions in a microVM |
| [k3s](https://grainvm.com/docs/main/guides/recipes/k3s/) | Single-node cluster preset |
| [Remote lab](https://grainvm.com/docs/main/guides/remote-lab/) | Host + laptop CLI happy path |
| [Firecracker](https://grainvm.com/docs/main/guides/firecracker/) | Linux+KVM backend (agent + TAP publish/fwd) |
| [Hypervisor matrix](https://grainvm.com/docs/main/explain/hypervisor-matrix/) | QEMU vs Firecracker capabilities |
| [Guides](https://grainvm.com/docs/main/guides/) | Images, agent, networking, mounts, proxy |
| [Reference](https://grainvm.com/docs/main/reference/cli/) | CLI, config, API, SDKs |

The site is built from this repo’s `docs/` directory.

---

## Develop

```bash
just test           # unit tests (mock hypervisor)
just smoke-api      # CLI + daemon e2e without QEMU
just build
just desktop-test   # Desktop backend unit tests
just desktop-build  # optional Grain Desktop (Wails; needs CGO + wails CLI)
```

[Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Code of conduct](CODE_OF_CONDUCT.md) · [Releasing](docs/developer/releasing.md)

## License

[Apache-2.0](LICENSE)
