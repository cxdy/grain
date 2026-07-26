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

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
```

The script detects OS/arch (`darwin`/`linux` × `arm64`/`amd64`), installs the latest GitHub release binary into `/usr/local/bin` or `~/.local/bin`, and falls back to `go install` when Go is present.

### From source

```bash
# Go 1.23+
go install github.com/cxdy/grain/cmd/grain@latest

# or download a release binary:
#   https://github.com/cxdy/grain/releases
# pick grain_<os>_<arch>, chmod +x, move to PATH
```

### Build from checkout

```bash
make test && make build
# Real VMs need QEMU:
brew install qemu
./bin/grain doctor
./bin/grain image pull    # Ubuntu cloud (~300MB)
```

After install:

```bash
brew install qemu   # macOS; on Linux install qemu-system + qemu-img
grain doctor
grain up
```

## Commands

| Cmd | Meaning |
|-----|---------|
| `up` / `down` | start/stop daemon |
| `new` | launch sandbox (`-p` persist, `-n` name, `-c` cpus, `-m` mem, `-d` disk, `-i` image) |
| `new --wait` | readiness: `ssh` (default), `agent`, or `userdata` |
| `new -P` / `--publish` | host→guest ports (`HOST:GUEST` or `GUEST`; repeatable) |
| `new -v` / `--volume` | share host dir `HOST:GUEST` via virtio-9p (repeatable) |
| `new --profile NAME` | named profile from config (flags override profile fields) |
| `new --preset docker\|k3s` | embedded cloud-init userdata preset |
| `new --userdata-file` | cloud-init userdata or shell script |
| `profile ls` | list named create profiles |
| `stop` / `start` | stop VM (ephemeral deleted; persistent kept) / start stopped persistent |
| `pause` / `resume` | QMP freeze / unfreeze guest vCPUs |
| `ls` / `rm` | list / delete |
| `sh` / `x` | shell / exec (`x` prefers guest agent with live streaming, SSH fallback; `--agent` / `--ssh`) |
| `agent health` | guest agent readiness (`GET /health` — version, uptime, userdata) |
| `logs` | guest serial (default) or `--qemu` hypervisor log; `-f` follow |
| `fwd ls` | list SSH + published port forwards |
| `fwd add` / `fwd rm` | live-add / remove host→guest forwards on a running VM |
| `cp` | `host path` or `NAME:path` (prefers agent Put/Get; scp fallback; `--agent` / `--ssh`) |
| `fs ls` / `stat` / `mkdir` / `rm` | guest filesystem via agent (no SSH) |
| `image ls` / `image pull` / `image import` | base images (pull ubuntu-cloud; import grain-ubuntu golden) |
| `doctor` | dependency check (QEMU, image, optional agent binary + QMP) |
| `version` | print version |

**Also in the surface area:** guest **stats** (`GET` agent `/stats` — uptime/mem/load), **secrets** (host store under `~/.grain/secrets`, agent `POST /secrets/materialize`), daemon **OpenAPI** (`api/openapi.yaml`, `GET /openapi.yaml`), **Go client SDK** (`github.com/cxdy/grain/client`), and optional **`api_token`** / `GRAIN_TOKEN` for Bearer auth.

**Guest agent:** each VM host-forwards guest `:7475`. After SSH is up, grain deploys `grain-agent` over SSH when `bin/grain-agent-linux-$(arch)` is present (`make agent-linux`), then waits for `/health`. `grain x` and `grain cp` use the agent when available (`x` streams stdout/stderr live; `cp` uses binary/tar file transfer). `grain fs` lists/stats/creates/removes guest paths without SSH. Soft-fail: VMs still work SSH-only (`--ssh` forces scp/ssh). Full overview: [docs/agent.md](docs/agent.md).

**Profiles** (`~/.grain/config.yaml` → `profiles:`) set create defaults; resolve order is CLI flags → profile → global defaults. Instances get `Tags["profile"]=name`. **Presets** (`docker`, `k3s`) merge into userdata; `k3s` also suggests 2 CPU / 4096 MiB when unset and auto-publishes guest 6443.

```bash
grain new --profile agent
grain new --preset docker
grain new --preset k3s -n lab -p
grain new --wait agent -v "$(pwd):/work"
grain profile ls
grain pause sbox-1 && grain resume sbox-1
grain fwd add sbox-1 8080:80
```

## Docs

| Guide | Topics |
|-------|--------|
| [docs/agent.md](docs/agent.md) | guest agent: health, exec, cp, fs, deploy, wait modes |
| [docs/networking.md](docs/networking.md) | SLIRP, SSH, `--publish`, `fwd ls/add/rm`, privileged ports |
| [docs/mounts.md](docs/mounts.md) | `-v HOST:GUEST`, 9p, mapped-xattr, cloud-init mounts |
| [docs/profiles.md](docs/profiles.md) | named profiles, docker/k3s presets |
| [docs/images.md](docs/images.md) | `ubuntu-cloud`, `grain-ubuntu` golden import/bake, SHA verify |
| [docs/troubleshooting.md](docs/troubleshooting.md) | doctor, logs, UEFI/HVF, cloud-init, resource caps |

### Recipes

| Recipe | Topics |
|--------|--------|
| [docs/recipes/coding-agent.md](docs/recipes/coding-agent.md) | sandbox + mount repo + run agent + `cp` results |
| [docs/recipes/k3s.md](docs/recipes/k3s.md) | `--preset k3s`, port forwards, grab kubeconfig |
| [docs/recipes/docker-socket.md](docs/recipes/docker-socket.md) | Docker in VM + host socket/TCP forward pattern |
| [docs/recipes/ci-ephemeral.md](docs/recipes/ci-ephemeral.md) | create → exec tests → `rm` (CI jobs) |

## How it works

1. **Daemon** (`grain up`) — unix socket API + optional TCP `/metrics`
2. **Image** — download once (`ubuntu-cloud` default), or import a baked golden (`grain-ubuntu`)
3. **Disk** — qcow2 overlay or APFS CoW clone per VM
4. **Boot** — QEMU (HVF on Apple Silicon) + cloud-init seed (SSH key)
5. **Access** — SSH via host-forwarded port; grain manages the key in `~/.grain/ssh/`

**Golden image (optional):** bake once so creates skip SSH agent deploy:

```bash
make agent-linux && ./scripts/bake-golden.sh
grain new -i grain-ubuntu
# or: grain image import ./my-golden.qcow2 --id grain-ubuntu
```

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

JSON logs on stderr. Metrics: `grain_vms_*`. Guest agent health exposes uptime/version via `grain agent health`.

## Config

`~/.grain/config.yaml` (all optional):

```yaml
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474
# Optional API auth (Bearer). Empty = no auth (default for local unix socket).
# CLI also reads env GRAIN_TOKEN.
api_token: ""
hypervisor: qemu          # or mock | firecracker (experimental, Linux)
# firecracker_binary: firecracker
# kernel_path: ""         # optional vmlinux for firecracker
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

Daemon HTTP over the unix socket and optional TCP (`api`). Spec: [`api/openapi.yaml`](api/openapi.yaml).

| Method | Path |
|--------|------|
| GET | `/healthz`, `/info`, `/metrics` |
| GET/POST | `/vms` |
| GET/DELETE | `/vms/{name}` |
| POST | `/vms/{name}/shutdown` |
| POST | `/vms/{name}/pause`, `/vms/{name}/resume` |
| POST | `/vms/{name}/forwards` · DELETE `/vms/{name}/forwards/{hostPort}` |
| POST | `/vms/{name}/exec` |
| GET | `/vms/{name}/agent/health` |
| PUT/GET | `/vms/{name}/cp` |
| GET/POST/DELETE | `/vms/{name}/fs/…` |

When `api_token` (or `auth_token`) is set, all routes except `GET /healthz` require `Authorization: Bearer <token>`. The CLI sends `GRAIN_TOKEN` or the config token automatically.

```bash
curl --unix-socket ~/.grain/grain.sock http://grain/vms
# with token:
curl -H "Authorization: Bearer $GRAIN_TOKEN" --unix-socket ~/.grain/grain.sock http://grain/vms
```

### Go client SDK

```go
import "github.com/cxdy/grain/client"

c, err := client.DialUnix(filepath.Join(os.Getenv("HOME"), ".grain", "grain.sock"))
// or: client.DialHTTP("http://127.0.0.1:7474", os.Getenv("GRAIN_TOKEN"))

inst, err := c.Create(ctx, client.CreateRequest{Persistent: false, Wait: "agent"})
_ = c.Exec(ctx, inst.Name, "uname", "-a")
```

Package docs mirror the OpenAPI shapes; see [`client/`](client/) and [`api/openapi.yaml`](api/openapi.yaml).

## License

Apache-2.0 — see [LICENSE](LICENSE).
