---
title: "Egress proxy"
description: "Default-deny HTTP(S) proxy with allow rules and secrets."
section: guides
---


grain can run a **host-side HTTP(S) forward proxy** that guest VMs use via
`HTTPS_PROXY` / `HTTP_PROXY`. Outbound traffic is **default-deny**: only
explicit allow rules (host, optional method, optional path prefix) pass.
Rules may also **inject** an `Authorization` header from the host secrets
store so the guest never holds the real token.

```text
guest ──HTTPS_PROXY──► grain-proxy (host) ──► internet
                         │
                         ├ allow rules (host, method, path prefix)
                         └ secrets (inject Bearer on match)
```

The proxy is a **separate process** from the control-plane daemon (`grain up`)
so a proxy crash does not take down VM management. Both share
`~/.grain/secrets`.

## Quick start

```bash
# 1) start proxy (listens 0.0.0.0:3128 by default)
grain proxy up

# 2) create a client token (optional but recommended)
grain proxy client create agent
# prints: token <hex>
# guest:  export HTTPS_PROXY=http://<token>@10.0.2.2:3128

# 3) allow only the APIs you need
grain secret set openai-key --value 'sk-…'
grain proxy allow --host api.openai.com --secret openai-key
# HTTPS from the guest uses CONNECT; host match is enough (path not visible)

# Plain HTTP can also match method/path and inject Authorization:
grain proxy allow --host api.example.com --method POST --path /v1/ --secret openai-key

# 4) launch a VM with proxy env baked in
grain new --proxy -n agent

# or set manually inside the guest:
#   export HTTPS_PROXY=http://TOKEN@10.0.2.2:3128
#   export HTTP_PROXY=$HTTPS_PROXY
#   export NO_PROXY=localhost,127.0.0.1,10.0.2.0/24
```

List and remove rules:

```bash
grain proxy ls
grain proxy deny rule-1
grain proxy down
```

## Listen address (important)

| Bind | Guest reachability |
|------|--------------------|
| **`0.0.0.0:3128`** (default) | Guests reach the host as **`10.0.2.2:3128`** (QEMU SLIRP gateway) |
| `127.0.0.1:3128` | **Not** reachable from VMs via SLIRP |

Config (`~/.grain/config.yaml`):

```yaml
proxy_listen: 0.0.0.0:3128
```

CLI override:

```bash
grain proxy up --listen 0.0.0.0:3128
```

**Firewall:** binding `0.0.0.0` accepts connections on all host interfaces.
On multi-user or exposed machines, restrict with a host firewall (e.g. allow
only loopback + the SLIRP path) or put the host behind a private network.
Create proxy **clients** so a token is required; until the first client
exists, the proxy accepts unauthenticated connections (convenient for local
dev, not for shared hosts).

## Allow rules

Rules are stored in `~/.grain/proxy/rules.json`.

| Field | Meaning |
|-------|---------|
| `host` | Exact hostname, or `*.example.com` (subdomains only, not the bare domain) |
| `method` | Empty = any. Use `CONNECT` to constrain HTTPS tunnels only |
| `path_prefix` | Empty = any. Applies to plain HTTP only (CONNECT has no path) |
| `secret_name` | Optional name from `grain secret`; injects `Authorization` |

**HTTPS (CONNECT):** browsers and `curl` open a tunnel; the proxy only sees
`host:port`. Path/method allowlists do not apply inside the TLS session.
Use host allow rules for HTTPS. Secret injection on CONNECT is **not**
possible (payload is opaque TLS); inject only works for **plain HTTP**
proxied requests (or tools that speak HTTP to the proxy with absolute URLs).

**Default deny:** if no rule matches, the proxy returns **403**.

## Secret injection

```bash
grain secret set gh-token --value 'ghp_…'
grain proxy allow --host api.github.com --path / --secret gh-token
```

On a matching plain-HTTP request, the proxy sets:

```http
Authorization: Bearer <secret bytes>
```

If the secret already starts with `Bearer ` or `Basic `, it is used as the
full header value.

## Client authentication

```bash
grain proxy client create myvm
```

Clients live in `~/.grain/proxy/clients.json` (mode 0600). When **any** client
exists, the proxy requires a token via:

- URL userinfo: `http://TOKEN@10.0.2.2:3128`
- Or `Proxy-Authorization: Basic …` / `Bearer …`

## Guest integration

### `grain new --proxy`

Creates a client token if none exist, then merges cloud-init that writes
`/etc/profile.d/grain-proxy.sh` and `/etc/environment` with
`HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` pointing at
`http://TOKEN@10.0.2.2:<port>`.

### Manual / profile

```yaml
#cloud-config
write_files:
  - path: /etc/profile.d/grain-proxy.sh
    permissions: '0644'
    content: |
      export HTTPS_PROXY=http://TOKEN@10.0.2.2:3128
      export HTTP_PROXY=$HTTPS_PROXY
      export NO_PROXY=localhost,127.0.0.1,10.0.2.0/24
```

### Coding agents

See [recipes/coding-agent.md](/docs/0.2.2/guides/recipes/coding-agent/) for a sandbox with a
mounted repo; add `grain proxy up` + allow rules for model/API hosts and
`grain new --proxy` so the agent cannot reach the open internet.

## Security model

1. **Default deny** — no implicit open egress through the proxy.
2. **Allowlist** — only listed hosts (and optional method/path for HTTP).
3. **Host-held secrets** — tokens stay on the host; guests get a proxy client
   token only (revocable by rotating clients).
4. **Process isolation** — proxy pid/log under `~/.grain/proxy/`; separate
   from `grain up`.
5. **Not a network policy engine** — guests can still use non-proxy egress
   (raw TCP via SLIRP) unless you add further host controls. The proxy is an
   **application-level** control for tools that honor `HTTP(S)_PROXY`.

## Files

| Path | Purpose |
|------|---------|
| `~/.grain/proxy/rules.json` | Allow rules |
| `~/.grain/proxy/clients.json` | Client tokens |
| `~/.grain/proxy/proxy.pid` | Running process |
| `~/.grain/proxy/proxy.log` | Background logs |
| `~/.grain/secrets/` | Shared with `grain secret` |

## Related

- [Networking](/docs/0.2.2/guides/networking/) — SLIRP, hostfwd, ports
- [Recipes: coding agent](/docs/0.2.2/guides/recipes/coding-agent/)
