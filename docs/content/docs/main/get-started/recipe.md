---
title: "Sandbox recipes (YAML create + bootstrap)"
description: "Portable recipe files for create options and bootstrap steps that stamp readiness before grain says ready."
section: get-started
keywords:
  - recipe
  - yaml
  - bootstrap
  - grain new --recipe
  - sandbox recipe
---

**Goal:** check in a YAML file that creates a sandbox, runs install steps, and only reports **ready** when those steps finish with zero failures.

Deep contract: [Readiness protocol](../explain/readiness/). Manual cloud-init path: [Bootstrap until ready](../bootstrap/).

---

## 1. Minimal recipe

Save as `git-lab.recipe.yaml`:

```yaml
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: git-lab
spec:
  image: grain-ubuntu
  cpus: 2
  memory_mb: 2048
  ready_timeout: 10m
  bootstrap:
    steps:
      - name: packages
        message: installing git
        run: |
          export DEBIAN_FRONTEND=noninteractive
          apt-get update -qq
          apt-get install -y -qq git
```

```bash
grain up
grain image pull grain-ubuntu
grain recipe validate ./git-lab.recipe.yaml
grain new --recipe ./git-lab.recipe.yaml
grain status git-lab
grain sh git-lab
```

When `bootstrap.steps` is set, create defaults to **`--wait bootstrap`**. Ready means: VM up, agent healthy, all steps succeeded (`state=ready`).

---

## 2. What a recipe can set

| Field | Role |
|-------|------|
| `metadata.name` | Default VM name (`-n` overrides) and default `ready_name` |
| `spec.image` / `cpus` / `memory_mb` / `disk_gb` / `persistent` | Create resources |
| `spec.preset` | Merge `docker` / `k3s` / `act` cloud-init |
| `spec.mounts` | Host shares (`.` = your current directory) |
| `spec.forwards` | Port publish (`guest_port`, optional `host_port`) |
| `spec.userdata` / `userdata_file` | Extra cloud-init or shell (merged before bootstrap steps) |
| `spec.bootstrap.steps` | Ordered guest scripts; each becomes a readiness `phase` |
| `spec.wait` / `ready_timeout` | Wait mode and create timeout |

CLI flags still win over recipe fields (same idea as profiles).

Examples in-repo: [`examples/recipes/`](https://github.com/cxdy/grain/tree/main/examples/recipes).

From **Grain Desktop**, select a sandbox → inspector **More → Export as recipe…** to save create options (image, resources, mounts, forwards) as a recipe file. Bootstrap steps and first-boot userdata are not recovered from a live VM — add those by hand if needed.

---

## 3. Inspect without creating

```bash
grain recipe validate ./git-lab.recipe.yaml
grain recipe show ./git-lab.recipe.yaml
grain recipe show ./git-lab.recipe.yaml --userdata   # compiled cloud-init
```

---

## 4. vs profiles and presets

| Mechanism | Portable file? | Bootstrap readiness? |
|-----------|----------------|----------------------|
| **Profile** (`~/.grain/config.yaml`) | No (host config) | Only if you attach userdata yourself |
| **Preset** (`docker` / `k3s` / `act`) | Built into grain | Preset install scripts (not recipe steps) |
| **Recipe** (`--recipe`) | Yes (repo-friendly) | Yes — steps stamp readiness automatically |

---

## Next

- [Bootstrap until ready](../bootstrap/) — hand-written cloud-init  
- [Readiness protocol](../explain/readiness/) — guest files + agent API  
- [Profiles & presets](../../guides/profiles/) — named host defaults  
