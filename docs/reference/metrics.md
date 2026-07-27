---
title: Metrics
description: Prometheus metrics exposed by the grain daemon.
---

## Endpoint

When TCP API is enabled (default `api: 127.0.0.1:7474`):

```bash
curl -s http://127.0.0.1:7474/metrics
```

Also available on the unix socket:

```bash
curl -s --unix-socket ~/.grain/grain.sock http://grain/metrics
```

## Series

Exact names may grow; common counters include:

| Metric | Meaning |
|--------|---------|
| `grain_vms_created_total` | VMs created |
| `grain_vms_deleted_total` | VMs deleted |
| `grain_vms_running` | Gauge-style running count (implementation may use counter semantics — scrape and graph carefully) |
| `grain_create_errors_total` | Failed creates |

Optional compose stack for Prometheus/Grafana lives under `deploy/observability/` in the repository (`just obs-up`).

## Logging

Daemon logs are structured JSON on stderr when running with `grain up --fg` or the background process log configuration for your install.
