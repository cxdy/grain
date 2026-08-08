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

grain runs headless by default (`-display none`). Enable **virtio-gpu** when the guest needs a virtio display device (Wayland/X, GPU-aware tools, and similar).

This is **not** full GPU passthrough (VFIO / Metal). It attaches a **virtio-gpu-pci** device that covers many desktop-in-VM cases without binding a physical GPU.

## Enable

```bash
grain new --gpu virtio --wait agent
```

Or in config:

```yaml
gpu: virtio
```

## Notes

- Host stays headless; no local window opens
- QEMU must support `virtio-gpu-pci`
- Mounts and port forwards work as usual

## See also

- [CLI reference](../../reference/cli/)  
