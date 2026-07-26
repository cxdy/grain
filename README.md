# grain

**Fast Linux microVM sandboxes on your own hardware.** Free and open source (Apache-2.0).

Ephemeral by default. Persistent when you want. Short commands. Local-first.

```text
# 1) start daemon (once per session)
grain up

# 2) download base image (once)
grain image pull

# 3) shell in — auto-creates a VM if none exist
grain sh

# or step-by-step:
grain new                 # prints: next: grain sh sbox-1
grain ls
grain sh                  # name optional if only one VM
grain x -- uname -a
grain rm                  # name optional if only one
grain down
```

## Install

```bash
# from source (Go 1.23+)
go install github.com/cxdy/grain/cmd/grain@latest

# or download a release binary from GitHub Releases:
#   https://github.com/cxdy/grain/releases
# pick grain_<os>_<arch> (darwin/linux × arm64/amd64), chmod +x, move to PATH
```

### Build from checkout

```bash
make test && make build
# Real VMs need QEMU:
brew install qemu
./bin/grain doctor
./bin/grain image pull    # Ubuntu cloud (~300MB)
```

## Commands

| Cmd | Meaning |
|-----|---------|
| `up` / `down` | start/stop daemon |
| `new` | launch sandbox (`-p` persist, `-n` name, `-c` cpus, `-m` mem, `-d` disk, `-i` image) |
| `new -P` / `--publish` | host→guest ports (`HOST:GUEST` or `GUEST`; repeatable) |
| `new -v` / `--volume` | share host dir `HOST:GUEST` via virtio-9p (repeatable) |
| `new --profile NAME` | named profile from config (flags override profile fields) |
| `new --preset docker\|k3s` | embedded cloud-init userdata preset |
| `new --userdata-file` | cloud-init userdata or shell script |
| `profile ls` | list named create profiles |
| `stop` / `start` | stop VM (ephemeral deleted; persistent kept) / start stopped persistent |
| `ls` / `rm` | list / delete |
| `sh` / `x` | shell / exec (`x` prefers guest agent, SSH fallback; `--agent` / `--ssh`) |
| `agent health` | guest agent readiness (`GET /health`) |
| `logs` | guest serial (default) or `--qemu` hypervisor log; `-f` follow |
| `fwd ls` | list SSH + published port forwards |
| `cp` | `host path` or `NAME:path` |
| `image ls` / `image pull` | base images |
| `doctor` | dependency check |
| `version` | print version |

**Guest agent:** each VM host-forwards guest `:7475`. After SSH is up, grain deploys `grain-agent` over SSH when `bin/grain-agent-linux-$(arch)` is present (`make agent-linux`), then waits for `/health`. `grain x` uses the agent when available. Soft-fail: VMs still work SSH-only.

**Profiles** (`~/.grain/config.yaml` → `profiles:`) set create defaults; resolve order is CLI flags → profile → global defaults. Instances get `Tags["profile"]=name`. **Presets** (`docker`, `k3s`) merge into userdata; `k3s` also suggests 2 CPU / 4096 MiB when unset and auto-publishes guest 6443.

```bash
grain new --profile agent
grain new --preset docker
grain new --preset k3s -n lab -p
grain profile ls
```

## Docs

| Guide | Topics |
|-------|--------|
| [docs/networking.md](docs/networking.md) | SLIRP, SSH, `--publish`, `fwd ls`, privileged ports |
| [docs/mounts.md](docs/mounts.md) | `-v HOST:GUEST`, 9p, mapped-xattr, cloud-init mounts |
| [docs/profiles.md](docs/profiles.md) | named profiles, docker/k3s presets |
| [docs/images.md](docs/images.md) | `ubuntu-cloud`, pull once, SHA verify |
| [docs/troubleshooting.md](docs/troubleshooting.md) | doctor, logs, UEFI/HVF, cloud-init, resource caps |

## How it works

1. **Daemon** (`grain up`) — unix socket API + optional TCP `/metrics`
2. **Image** — download once (`ubuntu-cloud` default)
3. **Disk** — qcow2 overlay or APFS CoW clone per VM
4. **Boot** — QEMU (HVF on Apple Silicon) + cloud-init seed (SSH key)
5. **Access** — SSH via host-forwarded port; grain manages the key in `~/.grain/ssh/`

Ephemeral VMs are removed on `rm`, `stop`, or daemon stop. Persistent (`-p`) keep their disk and can be brought back with `start`.

## Tests & TDD

```bash
make test          # unit tests (mock hypervisor)
make smoke-api     # CLI + daemon e2e without QEMU
make cover
```

## Observability (optional)

```bash
make obs-up   # Prometheus :9090, Grafana :3000, Loki :3100
curl -s http://127.0.0.1:7474/metrics
```

JSON logs on stderr. Metrics: `grain_vms_*`.

## Config

`~/.grain/config.yaml` (all optional):

```yaml
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474
hypervisor: qemu          # or mock
image: ubuntu-cloud
cpus: 2
memory_mb: 2048
disk_gb: 8
ssh_user: ubuntu
ready_timeout: 2m
log_level: info

# Resource caps (0 = unlimited). Stopped VMs do not count.
max_vms: 8
max_cpus_total: 16
max_memory_mb_total: 32768
max_cpus_per_vm: 8
max_memory_mb_per_vm: 16384

# Named profiles for `grain new --profile NAME`
profiles:
  agent:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    image: ubuntu-cloud
    persistent: false
    preset: ""            # optional: docker | k3s
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}  # host port auto
  k3s-lab:
    cpus: 2
    memory_mb: 4096
    disk_gb: 20
    persistent: true
    preset: k3s
```

## API

| Method | Path |
|--------|------|
| GET | `/healthz`, `/info`, `/metrics` |
| GET/POST | `/vms` |
| GET/DELETE | `/vms/{name}` |
| POST | `/vms/{name}/shutdown` |

```bash
curl --unix-socket ~/.grain/grain.sock http://grain/vms
```

## License

Apache-2.0 — see [LICENSE](LICENSE).
