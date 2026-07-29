---
title: Guest architecture
description: Run arm64 or amd64 Linux guests, including x86_64 on Apple Silicon via QEMU TCG.
section: guides
---

By default grain matches the **host** architecture (Apple Silicon → arm64 guests, Intel/AMD → amd64).

## Select guest arch

```bash
# native (default)
grain new --wait agent

# x86_64 guest (on Apple Silicon this uses QEMU TCG — slower, not Apple Rosetta)
grain new --arch amd64 -i ubuntu-cloud --wait ssh
```

Config default:

```yaml
guest_arch: amd64   # or arm64 / empty for host
```

## Images

Use an image built for the guest ISA. The golden `grain-ubuntu` pull matches the **host** arch. For cross-arch boots, pull or import a matching cloud image (for example Ubuntu cloud amd64 on an arm64 Mac).

## Rosetta note

Apple’s **Rosetta for Linux** is a Virtualization.framework feature, not QEMU HVF. grain’s multi-arch path on Apple Silicon uses **QEMU TCG** for amd64 guests. That is correct for many CI/debug cases but is not Rosetta translation inside a VZF VM.

## See also

- [Images](/docs/0.4.0/guides/images/)  
