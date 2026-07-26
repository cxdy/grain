# grain

**Fast Linux microVM sandboxes on your own hardware.** Free and open source (Apache-2.0).

Ephemeral by default. Persistent when you want. Short commands. Local-first.

```text
grain up
grain image pull          # once
grain new                 # ephemeral sandbox
grain new -p -n lab       # keep it
grain ls
grain sh sbox-1
grain x sbox-1 -- uname -a
grain rm sbox-1
grain down
```

## Install (dev)

```bash
# Go 1.23+
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
| `new` | launch sandbox (`-p` persist, `-n` name, `-c` cpus, `-m` mem, `-d` disk) |
| `ls` / `rm` | list / delete |
| `sh` / `x` | shell / exec |
| `cp` | `host path` or `NAME:path` |
| `image ls` / `image pull` | base images |
| `doctor` | dependency check |
| `version` | print version |

## How it works

1. **Daemon** (`grain up`) — unix socket API + optional TCP `/metrics`
2. **Image** — download once (`ubuntu-cloud` default)
3. **Disk** — qcow2 overlay or APFS CoW clone per VM
4. **Boot** — QEMU (HVF on Apple Silicon) + cloud-init seed (SSH key)
5. **Access** — SSH via host-forwarded port; grain manages the key in `~/.grain/ssh/`

Ephemeral VMs are removed on `rm`, `shutdown`, or daemon stop. Persistent (`-p`) keep their disk.

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
