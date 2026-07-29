---
title: "Recipe: Docker in the VM + socket forward pattern"
description: How-to recipe from the grain documentation set.
section: guides
---


Install Docker inside a grain VM (via the `docker` preset), then either use Docker only from the guest or expose the daemon socket to the host for local clients.

## Prerequisites

```bash
grain up
grain image pull
```

## 1. Create a VM with the docker preset

```bash
grain new --preset docker -n dock -c 2 -m 2048 -p --wait userdata
```

| Flag | Effect |
|------|--------|
| `--preset docker` | cloud-init installs `docker.io`, enables the service, adds `ubuntu`/`grain` to the `docker` group |
| `-p` | keep images/containers across stop/start |
| `--wait userdata` | wait for package install + docker enable (needs guest agent) |

Watch first boot:

```bash
grain logs -f dock
grain agent health dock
grain x dock -- docker version
```

## 2. Use Docker from inside the guest

```bash
grain x dock -- docker run --rm hello-world
grain x dock -- docker ps
```

Mount a project and build:

```bash
grain rm dock 2>/dev/null || true
grain new --preset docker -n dock -p \
  -v "$(pwd):/work" \
  --wait userdata
grain x dock -- bash -lc 'cd /work && docker build -t app .'
```

## 3. Socket forward pattern (host → guest Docker)

Docker listens on a Unix socket in the guest (`/var/run/docker.sock`). Host tools cannot use that path directly. Pattern:

1. **Inside the guest**, expose the daemon on a TCP port (lab only — no TLS).
2. **On the host**, publish or live-forward that port.
3. Point the host Docker client at `tcp://127.0.0.1:<hostPort>`.

### A. Enable TCP in the guest (lab)

```bash
grain x dock -- sudo mkdir -p /etc/systemd/system/docker.service.d
grain x dock -- bash -c 'echo -e "[Service]\nExecStart=\nExecStart=/usr/bin/dockerd -H fd:// -H tcp://0.0.0.0:2375" | sudo tee /etc/systemd/system/docker.service.d/override.conf'
grain x dock -- sudo systemctl daemon-reload
grain x dock -- sudo systemctl restart docker
grain x dock -- sudo ss -lntp | grep 2375 || true
```

> **Warning:** plain TCP Docker is unauthenticated. Only for local labs. Prefer TLS or never expose beyond loopback hostfwd.

### B. Forward guest 2375 to the host

At create time:

```bash
grain new --preset docker -n dock -p -P 2375:2375 --wait userdata
# then apply the systemd override above
```

Or on a running VM (live SSH tunnel forward):

```bash
grain fwd add dock 2375:2375
grain fwd ls dock
```

### C. Host client

```bash
export DOCKER_HOST=tcp://127.0.0.1:2375
docker version
docker run --rm hello-world
```

Clear when done:

```bash
unset DOCKER_HOST
grain fwd rm dock 2375   # if live-added
```

## 4. Alternative: CLI-only (no socket expose)

Keep Docker entirely in-guest and drive it with `grain x` / agent exec — no host `DOCKER_HOST`:

```bash
grain x dock -- docker compose -f /work/compose.yml up -d
grain cp dock:/work/out/artifact.tar ./artifact.tar
```

This is usually enough for CI and agents.

## 5. Cleanup

```bash
grain stop dock
# or
grain rm dock
```

## Notes

- Preset details: [profiles.md](../profiles.md).
- Port rules and live `fwd add`/`rm`: [networking.md](../networking.md).
- Host ports &lt; 1024 are rejected; use 2375 or another high port on the host side.
