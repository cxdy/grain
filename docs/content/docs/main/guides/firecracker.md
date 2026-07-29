---
title: "Firecracker on Linux (experimental)"
description: "Experimental Firecracker hypervisor backend."
section: guides
keywords:
  - Firecracker
  - KVM
  - hypervisor
  - Linux
  - experimental
  - vsock
  - rootfs
---

{{< only-need href="get-started/quickstart/" >}}
Default backend is QEMU — use that path unless you need Firecracker.
{{< /only-need >}}

grain can launch sandboxes with [Firecracker](https://firecracker-microvm.github.io/) instead of QEMU.

```yaml
# ~/.grain/config.yaml
hypervisor: firecracker
firecracker_binary: firecracker   # PATH lookup (default)
kernel_path: ""                   # optional; default ~/.grain/kernels/vmlinux
```

**Status:** experimental. Default remains `hypervisor: qemu`. The mock backend is unchanged for unit tests. macOS and hosts without Firecracker fail with clear errors.

## Requirements

| Dependency | Notes |
|------------|--------|
| **Linux** | Firecracker is Linux-only (`KVM`). On macOS: `firecracker requires linux`. |
| **firecracker** binary | On `PATH`, or set `firecracker_binary` |
| **Guest kernel** (`vmlinux`) | Uncompressed Linux kernel built for Firecracker (virtio MMIO, no PCI). Default path: `~/.grain/kernels/vmlinux` or `kernel_path` |
| **Raw rootfs** | Firecracker root drives are **raw** block files, not qcow2 |
| **qemu-img** | Used to convert qcow2 → raw when the VM disk is a qcow2 overlay |
| **KVM** | `/dev/kvm` accessible to the grain daemon user |

Check with:

```bash
grain doctor   # with hypervisor: firecracker in config
```

## Image / rootfs notes

grain’s catalog images (`ubuntu-cloud`, `grain-ubuntu`) are **qcow2 cloud images** aimed at QEMU + cloud-init. For Firecracker:

1. Prefer a **raw** golden rootfs (ext4/squashfs layout that boots with the FC kernel’s `root=/dev/vda`).
2. If the VM disk is still **qcow2**, Start runs `qemu-img convert -O raw` into `disk.raw` under the VM dir (when `qemu-img` is available). Otherwise Start refuses with a conversion hint.
3. Standard Ubuntu cloud images need a **matching Firecracker-capable kernel**; they are not drop-in FC guests without extra work (kernel + init + virtio drivers).

See also [Images](../images/) for QEMU/golden workflow; FC is a separate experimental path.

### Suggested layout

```text
~/.grain/
  kernels/
    vmlinux              # Firecracker guest kernel
  config.yaml            # hypervisor: firecracker
  vms/<name>/
    disk.raw             # rootfs (converted or imported)
    firecracker.json     # generated config
    firecracker.sock     # FC API unix socket
    fc-vsock.sock        # host end of virtio-vsock
    firecracker.pid
```

## Networking and agent

This backend is **CNI-less / TAP-less**: no SLIRP, no hostfwd, no SSH port.

| Channel | Status |
|---------|--------|
| SSH / port forwards | Not configured (experimental) |
| grain-agent | **Firecracker vsock** only |

On Start, grain:

- Allocates a guest **CID** (`AgentCID`, same allocator as QEMU vsock)
- Configures Firecracker `vsock` with `uds_path` = `…/fc-vsock.sock`

Firecracker’s host-side vsock is **not** AF_VSOCK/`/dev/vhost-vsock`. Host clients connect to the UDS and send `CONNECT <port>\n` (see [Firecracker vsock docs](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)). Guest agent listens on AF_VSOCK port **7475**.

QEMU’s `agent_transport: auto|tcp|vsock` path (vhost-vsock / TCP hostfwd) does not apply here. Full CLI `grain agent` dial over FC UDS may need a small host connector; the guest agent binary is unchanged.

## Start / stop / pause

| Operation | Behavior |
|-----------|----------|
| **Start** | Writes `firecracker.json`, runs jailer-less `firecracker --api-sock … --config-file …` |
| **Stop** | `SendCtrlAltDel` via FC API, then SIGTERM/SIGKILL |
| **Pause / Resume** | `PATCH /vm` with `Paused` / `Resumed` when the API socket is up |
| **SaveVM / suspend snapshot** | Unsupported (`savevm is not supported for firecracker`) |

Logs: `~/.grain/logs/<name>.log` (Firecracker stdout/stderr).

## Cloud-init seed

If `seed.iso` exists in the VM dir, it is attached as a second read-only drive (`cidata`). NoCloud typically expects a labeled ISO/FAT volume; success depends on the guest image. Prefer baking keys/agent into a FC-oriented rootfs for reliable boots.

## Example

```yaml
# ~/.grain/config.yaml
hypervisor: firecracker
firecracker_binary: firecracker
kernel_path: /var/lib/grain/kernels/vmlinux-5.10
image: my-fc-rootfs   # local raw import; not ubuntu-cloud by default
cpus: 2
memory_mb: 1024
```

```bash
# Import a raw rootfs as a local image id (example)
grain image import ./rootfs.ext4 --id my-fc-rootfs

grain up
grain new -i my-fc-rootfs
grain stop <name>
```

## Related

- [Images](../images/) — base images, golden bake (QEMU-oriented)
- [Networking](../networking/) — QEMU SLIRP / hostfwd (not used by FC yet)
- [Guest agent](../agent/) — guest agent HTTP API
- [Troubleshooting](../troubleshooting/) — doctor and logs
