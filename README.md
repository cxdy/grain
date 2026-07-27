# grain

<p align="center">
  <img src="logo.png" alt="grain logo" width="200">
</p>

**Fast Linux microVM sandboxes on your own hardware.** Apache-2.0.

Run **GitHub Actions** (`grain act`) and **throwaway k3s** labs in isolated microVMs — plus everyday sandboxes with mounts, ports, and a guest agent.

Ephemeral by default · persistent when you want · local-first · macOS & Linux (not Windows/WSL).

**Documentation:** [https://grainvm.com](https://grainvm.com) · **Quick start:** [https://grainvm.com/get-started/quickstart/](https://grainvm.com/get-started/quickstart/)

## Quick start

```bash
# Install CLI + QEMU
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
brew install qemu          # macOS; on Linux: qemu-system + qemu-img
grain doctor

# Optional starter config
mkdir -p ~/.grain
cat > ~/.grain/config.yaml <<'EOF'
api: 127.0.0.1:7474
image: grain-ubuntu
cpus: 2
memory_mb: 2048
disk_gb: 8
profiles:
  work:
    cpus: 4
    memory_mb: 4096
    mounts:
      - {host: ".", guest: "/work"}
EOF

# First sandbox
grain up
grain image pull grain-ubuntu
grain new                    # or: grain new --profile work
grain sh
grain rm && grain down
```

More detail: **[Quick start](https://grainvm.com/get-started/quickstart/)**.

## Try a real workload

### GitHub Actions (`grain act`)

Run [nektos/act](https://github.com/nektos/act) inside a disposable microVM so host Docker stays clean:

```bash
grain up
grain image pull grain-ubuntu
cd /path/to/your/repo
grain act -- -l              # list workflows / jobs
grain act -- -j test         # run a job
```

Guide: [GitHub Actions with act](https://grainvm.com/guides/recipes/act/).

### Throwaway k3s lab

```bash
grain up
grain image pull grain-ubuntu
grain new --preset k3s -n lab -p --wait userdata
grain fwd ls lab             # API server host port → guest 6443
```

Guide: [k3s recipe](https://grainvm.com/guides/recipes/k3s/) (kubeconfig pull-down and `kubectl`).

## What you get

| | |
|--|--|
| **Workloads** | `grain act` · `--preset k3s` · `--preset docker` |
| **CLI** | `up` / `new` / `sh` / `x` / `rm` · mounts · port forwards · profiles |
| **Guest agent** | Fast exec, file copy, fs ops without SSH when healthy |
| **API** | Unix socket + optional TCP · [OpenAPI](api/openapi.yaml) |
| **SDKs** | [Go](https://pkg.go.dev/github.com/cxdy/grain/client) · [TypeScript](https://www.npmjs.com/package/@cxdy/grain) · [Python](https://pypi.org/project/cxdy-grain/) |

## Documentation

| | |
|--|--|
| [Install](https://grainvm.com/get-started/install/) | Platforms, script, binaries, from source |
| [Quick start](https://grainvm.com/get-started/quickstart/) | Install + config + first VM |
| [GitHub Actions (act)](https://grainvm.com/guides/recipes/act/) | Isolated `act` in a microVM |
| [k3s lab](https://grainvm.com/guides/recipes/k3s/) | Single-node cluster preset |
| [First sandbox](https://grainvm.com/get-started/first-sandbox/) | Tutorial + interactive demo |
| [Guides](https://grainvm.com/guides/) | Images, agent, networking, mounts, proxy, remote host |
| [Reference](https://grainvm.com/reference/cli/) | CLI, config, API, SDKs |
| [Architecture](https://grainvm.com/explain/architecture/) | How the daemon, images, and VMs fit together |

This repo’s `docs/` directory is the [grainvm.com](https://grainvm.com) Jekyll site.

## Develop

```bash
just test          # unit tests (mock hypervisor)
just smoke-api     # CLI + daemon e2e without QEMU
just build
```

See [CONTRIBUTING.md](CONTRIBUTING.md). Security: [SECURITY.md](SECURITY.md). Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Releases: [docs/developer/releasing.md](docs/developer/releasing.md).

## License

Apache-2.0 — see [LICENSE](LICENSE).
