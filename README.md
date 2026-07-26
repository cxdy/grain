# grain

**Fast Linux microVM sandboxes on your own hardware.** Free and open source (Apache-2.0).

Spin up an ephemeral sandbox, run tests (including k3s/Helm labs), copy results out, tear it down. Keep a VM long-term only when you ask for it.

```text
grain up              # start local daemon
grain new             # ephemeral sandbox (default)
grain new -p -n lab   # persistent, named
grain ls
grain sh sbox-1       # shell
grain x sbox-1 -- uname -a
grain rm sbox-1
grain down
```

## Why

Containers share a host kernel. Sometimes you need a **real Linux guest** (systemd, custom modules, k3s, isolation) without paying a cloud tax to use **your** laptop or lab machines. grain is that control plane: short commands, local-first, no metering.

## Status

**v0.1 scaffold** — control plane, CLI, tests, mock hypervisor, and QEMU backend wiring. Bootable base images + guest agent hardening are next.

| Piece | State |
|-------|--------|
| CLI (`up` / `new` / `ls` / `rm` / `sh` / `x` / `cp`) | done |
| Daemon + HTTP API on unix socket | done |
| Ephemeral by default, `-p` to persist | done |
| Unit tests (TDD) | done |
| Mock hypervisor (CI without QEMU) | done |
| QEMU/HVF backend | wired (needs `brew install qemu` + image) |
| Optional Prometheus/Grafana/Loki | `make obs-up` |

## Quick start (dev / tests)

```bash
# requires Go 1.23+
make test
make build
make smoke-api          # mock hypervisor end-to-end, no QEMU
./bin/grain version
```

### Mock daemon (no QEMU)

```bash
make smoke-api
# or interactive:
printf 'hypervisor: mock\ndata_dir: /tmp/grain-dev\nsocket: /tmp/grain-dev/grain.sock\napi: 127.0.0.1:7474\n' > /tmp/grain-dev.yaml
./bin/grain --config /tmp/grain-dev.yaml up --fg   # terminal 1
./bin/grain --config /tmp/grain-dev.yaml new       # terminal 2
./bin/grain --config /tmp/grain-dev.yaml ls
```

### Real VMs (QEMU)

```bash
brew install qemu
# place bootable disk under ~/.grain/images/<image>/disk.img (image pull coming soon)
# config: hypervisor: qemu
grain up
grain new
```

## Observability (optional)

JSON logs to stderr. Prometheus metrics at `GET /metrics`.

```bash
make obs-up     # Prometheus :9090, Grafana :3000 (admin/admin), Loki :3100
# with daemon on default API:
#   curl -s http://127.0.0.1:7474/metrics
make obs-down
```

Metrics: `grain_vms_created_total`, `grain_vms_deleted_total`, `grain_vms_running`, `grain_create_errors_total`.

## Config

Default file: `~/.grain/config.yaml` (all optional).

```yaml
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474          # HTTP + /metrics
hypervisor: qemu             # or mock
cpus: 2
memory_mb: 2048
disk_gb: 8
log_level: info
```

## API (local)

Unix socket (preferred) or TCP `api`:

| Method | Path | |
|--------|------|--|
| GET | `/healthz` | liveness |
| GET | `/info` | version |
| GET | `/metrics` | Prometheus |
| GET | `/vms` | list |
| POST | `/vms` | create (`{"persistent":false}`) |
| GET | `/vms/{name}` | get |
| DELETE | `/vms/{name}` | delete |
| POST | `/vms/{name}/shutdown` | stop (ephemeral deleted) |

```bash
curl --unix-socket ~/.grain/grain.sock http://grain/vms
```

## Development

```bash
make test          # unit tests
make cover         # coverage summary
make build         # bin/grain
make fmt
```

Practice: **write tests with behavior**, keep packages small (`names`, `store`, `manager`, `api`, `hypervisor`). Prefer the **mock** hypervisor in tests.

## License

Apache-2.0 — see [LICENSE](LICENSE).
