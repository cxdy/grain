---
title: "Metrics (Prometheus + guest history)"
description: Daemon Prometheus scrape endpoint and per-sandbox guest stats history on the host.
section: reference
keywords:
  - metrics
  - Prometheus
  - observability
  - metrics.ring
  - guest stats
---

grain exposes two related surfaces:

1. **Prometheus text** at `GET /metrics` (daemon-wide counters/gauges)
2. **Per-sandbox guest history** on the host (`metrics.ring` under each VM dir), sampled in the background and shown in Desktop

## Prometheus endpoint

When TCP API is enabled (default `api: 127.0.0.1:7474`):

```bash
curl -s http://127.0.0.1:7474/metrics
```

Also available on the unix socket:

```bash
curl -s --unix-socket ~/.grain/grain.sock http://grain/metrics
```

### Series

Exact names may grow; common series include:

| Metric | Meaning |
|--------|---------|
| `grain_vms_created_total` | VMs created |
| `grain_vms_deleted_total` | VMs deleted |
| `grain_vms_running` | Running VM count (gauge-style; scrape carefully) |
| `grain_create_errors_total` | Failed creates |

Optional compose stack for Prometheus/Grafana: `deploy/observability/` (`just obs-up`).

## Guest stats history (host-side ring)

By default, **new sandboxes enable host-side metrics** (`sandbox_metrics_enabled: true` in config). The daemon samples guest `/stats` on an interval into:

```text
~/.grain/vms/<name>/metrics.ring
```

| Config key | Default | Meaning |
|------------|---------|---------|
| `sandbox_metrics_enabled` | `true` | Default for new creates |
| `sandbox_metrics_interval` | `15s` | Sample period |
| `sandbox_metrics_points` | `5760` | Ring capacity (~24h at 15s) |

Per-create override: create body / Desktop checkbox `metrics_enabled`. History remains readable after disable. Background sampling runs while the daemon is up (not only when Desktop polls).

Desktop: inspector overview charts when history exists. API: `GET /vms/{name}/metrics` (when implemented by the daemon route for Desktop).

## Activity log (related)

Control-plane **activity** (create/start/stop/…) is a separate ring at `data_dir/activity.json`, exposed as `GET /activity`. See [HTTP API](../api/#activity) and [Desktop activity](../../guides/desktop/).

## Logging

Daemon logs are structured JSON on stderr with `grain up --fg`. Create and pool claim paths log timing lines (`create timing`, `pool claim timing`, `spawn timing`) at INFO for latency debugging.
