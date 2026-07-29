---
title: "Virtio GPU (display device, not passthrough)"
description: Attach a virtio-gpu device to grain QEMU guests.
section: guides
keywords:
  - GPU
  - virtio-gpu
  - display
  - Wayland
  - graphics
---

{{< only-need href="get-started/quickstart/" >}}
Headless is the default — skip this unless you need a virtio display device.
{{< /only-need >}}

grain runs headless by default (`-display none`). For guests that want a virtio display device (Wayland/X in the guest, GPU-aware tools, etc.), enable **virtio-gpu**.

This is **not** full GPU passthrough (VFIO / Metal). It exposes a **virtio-gpu-pci** device suitable for many desktop-in-VM workflows without binding a physical GPU.

## Enable

```bash
grain new --gpu virtio --wait agent
```

Or in config:

```yaml
gpu: virtio
```

## Notes

- Host remains headless; no local window is opened
- Requires the QEMU binary to support `virtio-gpu-pci`
- Combine with mounts and port forwards as usual

## See also

- [CLI reference](../../reference/cli/)  
