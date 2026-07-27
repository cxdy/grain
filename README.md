# grain

**Fast Linux microVM sandboxes on your own hardware.** Apache-2.0.

Ephemeral by default · persistent when you want · local-first · macOS & Linux (not Windows/WSL).

**Docs:** [grainvm.com](https://grainvm.com) · **Quick start:** [grainvm.com/get-started/quickstart](https://grainvm.com/get-started/quickstart/)

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

More detail (install options, full config example, next steps): **[Quick start](https://grainvm.com/get-started/quickstart/)**.

## What you get

| | |
|--|--|
| **CLI** | `up` / `new` / `sh` / `x` / `rm` · mounts · port forwards · profiles · presets (`docker`, `k3s`, `act`) |
| **Guest agent** | Fast exec, file copy, fs ops without SSH when healthy |
| **API** | Unix socket + optional TCP · [OpenAPI](api/openapi.yaml) |
| **SDKs** | [Go](https://pkg.go.dev/github.com/cxdy/grain/client) · [TypeScript](https://www.npmjs.com/package/@cxdy/grain) · [Python](https://pypi.org/project/cxdy-grain/) |

## Documentation

| | |
|--|--|
| [Install](https://grainvm.com/get-started/install/) | Platforms, script, binaries, from source |
| [Quick start](https://grainvm.com/get-started/quickstart/) | Install + config + first VM |
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
