---
title: k3s lab
description: Spin up a disposable or persistent single-node k3s cluster with grain’s k3s preset.
---


Spin up a disposable (or persistent) k3s node with grain’s embedded `k3s` preset, forward the API port, and pull `kubeconfig` onto the host.

## Prerequisites

```bash
grain up
grain image pull
# Optional: kubectl on the host
# brew install kubectl
```

## 1. Create with the k3s preset

```bash
grain new --preset k3s -n k3s-lab -p --wait userdata
```

| Flag | Effect |
|------|--------|
| `--preset k3s` | cloud-init installs k3s; suggests **2 CPU / 4096 MiB** when unset; auto-publishes guest **6443** |
| `-p` | persistent disk (k3s state survives stop/start) |
| `--wait userdata` | wait until guest userdata (including k3s install) finishes — needs guest agent |

Faster ready (SSH only), then watch install:

```bash
grain new --preset k3s -n k3s-lab -p --wait ssh
grain logs -f k3s-lab
# later:
grain agent health k3s-lab
```

Create output includes something like `tcp=:<hostPort>→6443` for the API server.

## 2. Inspect port forwards

```bash
grain fwd ls k3s-lab
# NAME      PROTO  HOST        GUEST   NOTE
# k3s-lab   tcp    :5xxxx      6443
# k3s-lab   tcp    :5yyyy      22      ssh
```

If you need an extra mapping on a running VM:

```bash
grain fwd add k3s-lab 16443:6443
grain fwd rm  k3s-lab 16443
```

## 3. Grab kubeconfig via agent `cp`

k3s writes `/etc/rancher/k3s/k3s.yaml` (also copied to `/home/ubuntu/.kube/config` by the preset).

```bash
# Wait until the file exists
grain x k3s-lab -- sudo test -f /etc/rancher/k3s/k3s.yaml

# Copy to host (agent path; sudo not available over agent file API —
# copy the ubuntu-owned kubeconfig instead):
grain cp k3s-lab:/home/ubuntu/.kube/config ./kubeconfig-k3s.yaml
```

If only the root path exists:

```bash
grain x k3s-lab -- sudo cp /etc/rancher/k3s/k3s.yaml /tmp/k3s.yaml
grain x k3s-lab -- sudo chown ubuntu:ubuntu /tmp/k3s.yaml
grain cp k3s-lab:/tmp/k3s.yaml ./kubeconfig-k3s.yaml
```

## 4. Point kubectl at the host-forwarded API

Replace the server address with `127.0.0.1` and the **host** port from `grain fwd ls`:

```bash
HOST_PORT=$(grain fwd ls k3s-lab | awk '$3 ~ /6443/ || $4 == "6443" { gsub(/^:/,"",$3); print $3; exit }')
# or read HOST from create output / fwd ls manually

# server: https://127.0.0.1:HOST_PORT
sed -e "s|server: https://127.0.0.1:6443|server: https://127.0.0.1:${HOST_PORT}|" \
    -e "s|server: https://localhost:6443|server: https://127.0.0.1:${HOST_PORT}|" \
    ./kubeconfig-k3s.yaml > ./kubeconfig-local.yaml

export KUBECONFIG="$PWD/kubeconfig-local.yaml"
kubectl get nodes
kubectl get pods -A
```

TLS: the default cert is issued for the in-guest name; use the kubeconfig as-is for lab work, or regenerate certs for production-like hosts (out of scope here).

## 5. Profile shortcut

```yaml
# ~/.grain/config.yaml
profiles:
  k3s-lab:
    cpus: 2
    memory_mb: 4096
    disk_gb: 20
    persistent: true
    preset: k3s
```

```bash
grain new --profile k3s-lab -n k3s-lab --wait userdata
```

## 6. Cleanup

```bash
grain stop k3s-lab     # keeps disk (-p)
grain start k3s-lab
# or destroy everything:
grain rm k3s-lab
```

## Notes

- First boot downloads k3s — several minutes; use `grain logs -f`.
- Guest API is only reachable via hostfwd on loopback (`127.0.0.1`), not LAN.
- See [profiles.md](../profiles.md) and [networking.md](../networking.md).
