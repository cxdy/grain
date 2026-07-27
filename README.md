# grain

**Fast Linux microVM sandboxes on your own hardware.** Free and open source (Apache-2.0).

**Documentation:** [grainvm.com](https://grainvm.com) · Install, guides, API, and SDKs.

**Platforms:** macOS and Linux (amd64 / arm64). **Not supported:** Windows or WSL.

Ephemeral by default. Persistent when you want. Short commands. Local-first.

```text
# 1) start daemon (once per session)
grain up

# 2) download base image (once) — prefer golden for faster agent-ready creates
grain image pull grain-ubuntu   # agent baked in (from golden-latest)
# or: grain image pull ubuntu-cloud   # Ubuntu cloud (~300MB, SSH-deploy agent)
# or: grain image pull alpine-cloud   # Alpine cloud (SSH user alpine)

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
./bin/grain image pull grain-ubuntu   # preferred golden; or ubuntu-cloud / alpine-cloud
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
| `new --wait` | readiness: `auto` (default — agent if golden/HasAgent, else ssh), `ssh`, `agent`, or `userdata` |
| `new -P` / `--publish` | host→guest ports (`HOST:GUEST` or `GUEST`; repeatable) |
| `new -v` / `--volume` | share host dir `HOST:GUEST` via virtio-9p (repeatable) |
| `new --profile NAME` | named profile from config (flags override profile fields) |
| `new --preset docker\|k3s\|act` | embedded cloud-init userdata preset |
| `new --userdata-file` | cloud-init userdata or shell script |
| `new --proxy` | inject `HTTPS_PROXY` via cloud-init (guest → `10.0.2.2:3128`) |
| `act -- [act-args]` | run [nektos/act](https://github.com/nektos/act) in an ephemeral sandbox (`--keep` to retain) |
| `profile ls` | list named create profiles |
| `stop` / `start` | stop VM (ephemeral deleted; persistent kept) / start stopped persistent |
| `suspend` / `restore` | stop QEMU (free RAM); restore from disk/snapshot |
| `pause` / `resume` | QMP freeze / unfreeze guest vCPUs |
| `ls` / `rm` | list / delete |
| `sh` / `x` | shell / exec (`x` prefers guest agent with live streaming, SSH fallback; `--agent` / `--ssh`) |
| `agent health` | guest agent readiness (`GET /health` — version, uptime, userdata) |
| `stats` | guest resource stats via agent (uptime/mem/load) |
| `logs` | guest serial (default) or `--qemu` hypervisor log; `-f` follow |
| `fwd ls` | list SSH + published port forwards |
| `fwd add` / `fwd rm` | live-add / remove host→guest forwards on a running VM |
| `cp` | `host path` or `NAME:path` (prefers agent Put/Get; scp fallback; `--agent` / `--ssh`) |
| `fs ls` / `stat` / `mkdir` / `rm` | guest filesystem via agent (no SSH) |
| `secret ls` / `set` / `rm` / `inject` | host secrets store; inject into a running VM |
| `image ls` / `image pull` / `image import` | base images (`grain-ubuntu`, `ubuntu-cloud`, `alpine-cloud`; import offline) |
| `proxy up` / `down` / `allow` / `deny` / `ls` / `client` | host egress proxy (default-deny allowlist + secret inject) |
| `doctor` | dependency check (QEMU, image, optional agent binary + QMP) |
| `version` | print version |

**Also:** daemon **OpenAPI** (`api/openapi.yaml`, `GET /openapi.yaml`), **Go client SDK** (`github.com/cxdy/grain/client`), **TypeScript client SDK** ([`sdk/ts`](sdk/ts) — `@cxdy/grain`), **Python client SDK** ([`sdk/python`](sdk/python) — `cxdy-grain` / `import grain`), and optional **`api_token`** / `GRAIN_TOKEN` for Bearer auth.

**Guest agent:** each VM host-forwards guest `:7475`. After SSH is up, grain deploys `grain-agent` over SSH when `bin/grain-agent-linux-$(arch)` is present (`make agent-linux`), then waits for `/health`. `grain x` and `grain cp` use the agent when available (`x` streams stdout/stderr live; `cp` uses binary/tar file transfer). `grain fs` lists/stats/creates/removes guest paths without SSH. Soft-fail: VMs still work SSH-only (`--ssh` forces scp/ssh). Full overview: [Guest agent](https://grainvm.com/guides/agent/).

**Profiles** (`~/.grain/config.yaml` → `profiles:`) set create defaults; resolve order is CLI flags → profile → global defaults. Instances get `Tags["profile"]=name`. **Presets** (`docker`, `k3s`, `act`) merge into userdata; `k3s` and `act` suggest 2 CPU / 4096 MiB when unset; `k3s` also auto-publishes guest 6443.

```bash
grain new --profile agent
grain new --preset docker
grain new --preset k3s -n lab -p
grain new --wait agent -v "$(pwd):/work"
grain act -- -l                 # GitHub Actions via act (ephemeral sandbox)
grain act --keep -- -j test
grain profile ls
grain pause sbox-1 && grain resume sbox-1
grain fwd add sbox-1 8080:80
```

## Docs

Full site: **[grainvm.com](https://grainvm.com)** (this repo’s `docs/` is the Jekyll source).

| Guide | Topics |
|-------|--------|
| [guides/agent](https://grainvm.com/guides/agent/) | guest agent: health, exec, cp, fs, deploy, wait modes |
| [guides/images](https://grainvm.com/guides/images/) | `grain-ubuntu` / `ubuntu-cloud` / `alpine-cloud`, pull, bake, SHA, bench |
| [guides/proxy](https://grainvm.com/guides/proxy/) | host egress proxy, allowlist, secret inject, `new --proxy` |
| [guides/firecracker](https://grainvm.com/guides/firecracker/) | experimental Firecracker backend, kernel/rootfs, vsock |
| [guides/networking](https://grainvm.com/guides/networking/) | SLIRP, SSH, `--publish`, `fwd ls/add/rm`, privileged ports |
| [guides/mounts](https://grainvm.com/guides/mounts/) | `-v HOST:GUEST`, 9p, mapped-xattr, cloud-init mounts |
| [guides/profiles](https://grainvm.com/guides/profiles/) | named profiles, docker / k3s / act presets |
| [guides/troubleshooting](https://grainvm.com/guides/troubleshooting/) | doctor, logs, UEFI/HVF, cloud-init, resource caps |

### Recipes

| Recipe | Topics |
|--------|--------|
| [coding agent](https://grainvm.com/guides/recipes/coding-agent/) | sandbox + mount repo + run agent + `cp` results |
| [k3s](https://grainvm.com/guides/recipes/k3s/) | `--preset k3s`, port forwards, grab kubeconfig |
| [Docker socket](https://grainvm.com/guides/recipes/docker-socket/) | Docker in VM + host socket/TCP forward pattern |
| [CI ephemeral](https://grainvm.com/guides/recipes/ci-ephemeral/) | create → exec tests → `rm` (CI jobs) |
| [GitHub Actions (act)](https://grainvm.com/guides/recipes/act/) | `grain act` — nektos/act in an isolated microVM |

## How it works

1. **Daemon** (`grain up`) — unix socket API + optional TCP `/metrics`
2. **Image** — download once (`grain-ubuntu` golden preferred when local; else `ubuntu-cloud`; also `alpine-cloud`)
3. **Disk** — qcow2 overlay or APFS CoW clone per VM
4. **Boot** — QEMU (HVF on Apple Silicon) + cloud-init seed (SSH key)
5. **Access** — SSH via host-forwarded port; grain manages the key in `~/.grain/ssh/`

**Golden image (recommended):** pull the published golden so creates skip SSH agent deploy and default to `--wait agent`:

```bash
grain image pull grain-ubuntu   # from GitHub Releases tag golden-latest
grain new -i grain-ubuntu       # or: grain new  (auto if grain-ubuntu is local)
# offline: make agent-linux && ./scripts/bake-golden.sh
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
    preset: ""            # optional: docker | k3s | act
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
| POST | `/vms/{name}/shutdown`, `/vms/{name}/start` |
| POST | `/vms/{name}/pause`, `/vms/{name}/resume` |
| POST | `/vms/{name}/suspend`, `/vms/{name}/restore` |
| POST | `/vms/{name}/forwards` · DELETE `/vms/{name}/forwards/{hostPort}` |
| POST | `/vms/{name}/exec` |
| GET | `/vms/{name}/agent/health`, `/vms/{name}/stats` |
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

Package docs and lifecycle methods (pause/resume/suspend/restore, live forwards,
create wait modes): [`client/README.md`](client/README.md). Spec: [`api/openapi.yaml`](api/openapi.yaml).

### TypeScript client SDK

Thin fetch client for Node automation: [`sdk/ts`](sdk/ts) (`@cxdy/grain`). Not published to npm yet — install from a local path (or future git/npm publish).

```bash
cd sdk/ts && npm install && npm run build
# from your app:
npm install /path/to/grain/sdk/ts
```

```ts
import { GrainClient } from "@cxdy/grain";

const grain = new GrainClient({
  baseURL: "http://127.0.0.1:7474",
  token: process.env.GRAIN_TOKEN,
});

await grain.health();
const inst = await grain.create({ persistent: false });
const out = await grain.exec(inst.name, "uname", ["-a"]);
```

Unix socket via optional `undici` (`socketPath` or custom `fetch`) — see [`sdk/ts/README.md`](sdk/ts/README.md).

### Python client SDK

Stdlib-only client for Python 3.9+: [`sdk/python`](sdk/python) (`cxdy-grain`, `import grain`). Not published to PyPI yet — install from a local path or git.

```bash
pip install -e ./sdk/python
# or: pip install "git+https://github.com/cxdy/grain.git#subdirectory=sdk/python"
```

```python
from pathlib import Path
from grain import GrainClient, CreateRequest, WAIT_AGENT

grain = GrainClient.unix(str(Path.home() / ".grain" / "grain.sock"))
# or: GrainClient(base_url="http://127.0.0.1:7474", token=os.environ.get("GRAIN_TOKEN", ""))

grain.health()
inst = grain.create(CreateRequest(persistent=False), wait=WAIT_AGENT, timeout="3m")
out = grain.exec(inst.name, "uname", ["-a"])
```

See [`sdk/python/README.md`](sdk/python/README.md) and [grainvm.com/reference/python-sdk](https://grainvm.com/reference/python-sdk/).

## License

Apache-2.0 — see [LICENSE](LICENSE).
