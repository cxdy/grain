---
title: "Images and boot (golden vs cloud)"
description: Why base images matter, how golden boots differ from cloud images, and how to think about speed.
section: explain
keywords:
  - images
  - boot
  - golden
  - grain-ubuntu
  - cloud-init
  - bench
  - bake
---

{{< only-need href="guides/images/" >}}
Pull, import, bake, and day-to-day image commands live in the Images guide.
{{< /only-need >}}

## Catalog at a glance

| ID | Role |
|----|------|
| `ubuntu-cloud` | General Ubuntu 24.04 minimal cloud; agent deployed over SSH |
| `grain-ubuntu` | Golden with agent baked; minimal seed on clone; pull from `golden-latest` when published |
| `alpine-cloud` | Alpine generic UEFI + cloud-init; lighter userspace; agent via deploy |

Config `image: auto` prefers a **Ready** local `grain-ubuntu`, else `ubuntu-cloud`.

## What “boot time” means

End-to-end create latency is usually:

1. Clone disk  
2. Hypervisor start  
3. Guest kernel + userspace  
4. cloud-init (heavy on first cloud boot)  
5. Agent healthy **or** SSH accept  

Golden images cut step 4–5 by baking the agent and using a **minimal** cloud-init seed for clones.

## Measuring

```bash
grain up
grain image pull grain-ubuntu   # or ubuntu-cloud
./scripts/bench-create.sh
```

Report p50/p95 on your hardware; do not assume numbers from other tools.

## Baking your own golden

```bash
just build && just agent-linux
./scripts/bake-golden.sh
# or CI: ./scripts/ci-bake-golden.sh
```

See [Images guide](../guides/images/).
