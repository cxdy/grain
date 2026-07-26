# Base images

## Default: `ubuntu-cloud`

grain’s catalog ships **Ubuntu 24.04 minimal cloud** as `ubuntu-cloud` (architecture-specific URL):

- arm64 / amd64 builds from [Ubuntu cloud images](https://cloud-images.ubuntu.com/minimal/releases/noble/release/)
- qcow2 disk, **cloud-init** NoCloud, default SSH user **`ubuntu`**
- Sized for a full cloud stack (~300 MB download), not a minimal initramfs toy

```bash
grain image ls
grain image pull              # default id from config (ubuntu-cloud)
grain image pull ubuntu-cloud
```

Config default:

```yaml
image: ubuntu-cloud
ssh_user: ubuntu
```

Override per VM:

```bash
grain new -i ubuntu-cloud
```

## Download once

Images land under `~/.grain/images/<id>/` (e.g. `disk.qcow2`).  
`grain image pull` **no-ops** if a usable base disk is already present. Each new VM uses a qcow2 overlay (or CoW clone) on top of that shared base—you do not re-download per sandbox.

```bash
grain image pull    # once per machine (or after deleting the image dir)
grain new           # overlay on local base
grain new           # same base again
```

## SHA-256 verification

Catalog entries pin a **SHA-256** digest for the current noble minimal release files. After download (before rename into place):

1. grain hashes the partial file
2. on **mismatch**, the partial is deleted and pull fails with `sha256 mismatch: got … want …`
3. on success, the file is renamed to `disk.qcow2` (or `.img`) and `source.url` / `ssh_user` hints are written

If a digest is empty in the catalog (dev-only), verification is skipped.

When Ubuntu rolls the release pointer, digests in the catalog must be refreshed to match [SHA256SUMS](https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS).

## Why not tiny custom images by default?

grain targets **real cloud-init Linux** sandboxes: SSH, packages, agents, k3s labs. That needs:

| Requirement | Cloud image | Tiny custom |
|-------------|-------------|-------------|
| cloud-init NoCloud seed (SSH keys, hostname, runcmd, mounts) | yes | often missing or half-broken |
| virtio disk/net + UEFI (esp. aarch64) | tested | easy to misconfigure |
| familiar `apt` / `ubuntu` user | yes | custom users and paths |
| security updates from upstream | yes | you own the rebuild |

A smaller rootfs can be added later as another catalog id; the default stays a known-good Ubuntu cloud image so `grain new` + `grain sh` works without hand-rolled kernels or init.

## Related

- [Troubleshooting](troubleshooting.md) — pull failures, doctor image check
- [Mounts](mounts.md) — 9p shares into the guest
