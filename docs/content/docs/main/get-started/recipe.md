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

### Local library (`~/.grain/recipes`)

Install recipes once, then create by **name** (CLI, Desktop, MCP):

```bash
# Import (never creates a VM)
grain recipe add ./git-lab.recipe.yaml          # → ~/.grain/recipes/git-lab.yaml
grain recipe preview https://example.com/lab.yaml  # validate + summary, no install
grain recipe add https://example.com/lab.yaml   # http(s) YAML
grain recipe search                             # official catalog index only
grain recipe add git-lab                        # pull one official body into the library

grain recipe list
grain recipe show git-lab
grain recipe validate git-lab
grain new --recipe git-lab                      # name resolves under ~/.grain/recipes
grain recipe delete git-lab                     # library file only — not sandboxes
```

### Official catalog recipes

In-repo under [`recipes/`](https://github.com/cxdy/grain/tree/main/recipes) (index: `recipes/catalog.json`):

| id | Notes |
|----|--------|
| `git-lab` | Minimal bootstrap (git) |
| `node-dev` | Repo mount + git/curl |
| `python-dev` | python3/pip/venv bootstrap |
| `docker-lab` | Preset `docker` |
| `k3s-lab` | Preset `k3s` + publish 6443 |
| `act-lab` | Preset `act` + mount `.` → `/work` |
| `remote-coding` | Persistent 4 vCPU / 8 GiB coding lab |

**Contributing official recipes:** open a PR that adds `recipes/<id>.yaml` and updates `catalog.json` (sha256 of the file). No accounts or marketplace backend — GitHub PRs only.

**Download counts (no extra infra):** when recipe bundles ship as GitHub Release assets, use the Releases API `download_count` per asset (`gh api repos/cxdy/grain/releases` / browser on the release page). The catalog itself is git-sourced until a release pin is published.

### Fast sandboxes (snapshot spawn + warm pool)

Cold `grain new --wait agent` is ~seconds (guest boot). For fast labs:

```bash
# One-time template
grain new -i grain-ubuntu -n golden -p --wait agent
# ... optional setup ...
grain suspend golden          # qcow2 savevm grain-suspend when possible

# On-demand fast copies (clone disk + -loadvm when snapshotted)
grain new --from golden -n work1
grain new --from golden -n work2

# Warm pool: pre-clone suspended members, claim without re-cloning
# ~/.grain/config.yaml:
#   warm_pool:
#     template: golden
#     size: 2
grain pool fill
grain new --from-pool -n work3   # or: grain pool claim -n work3
grain pool status
```

API: `POST /vms` with `{"from":"golden","name":"work1"}` or `{"from_pool":true,"name":"work3"}`.
Also `GET /pool`, `POST /pool/fill|claim|drain`. See [lifecycle](../guides/lifecycle/).

**Desktop:** **Recipes** tab → Import file / URL / Browse official → Edit YAML (valid-only save) → **Deploy…** (name override + wait until ready).

**Trust:** URL import is **preview then add** (Desktop fetches and validates, shows name/image/resources/mounts/bootstrap, then you confirm install). Official **Add** and CLI `recipe add <url>` install into the library only. Deploy/create is always a separate step. Prefer HTTPS; HTTP is allowed with a warning. Catalog entries may pin `sha256` (fail closed on mismatch). Offline: library always works; `recipe search` uses a cached index when present.

Env overrides: `GRAIN_HOME` (library under `$GRAIN_HOME/recipes`), `GRAIN_RECIPE_CATALOG_URL` (official index URL).

---

## 3. Inspect without creating

```bash
grain recipe validate ./git-lab.recipe.yaml
grain recipe show ./git-lab.recipe.yaml
grain recipe show git-lab                        # library name
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
