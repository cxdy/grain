# Security policy

## Reporting a vulnerability

Please report security issues privately via **[GitHub Security Advisories](https://github.com/cxdy/grain/security/advisories/new)** for [cxdy/grain](https://github.com/cxdy/grain).

Do not open a public issue for vulnerabilities that could enable host compromise, unauthenticated remote control of the daemon, or token/credential theft.

We will acknowledge reports as soon as practical and coordinate disclosure after a fix is available.

## In scope

- Daemon HTTP/unix-socket **API** (auth bypass, unsafe binds, privilege issues in control-plane handlers)
- **Guest agent** protocol and host-side agent proxy paths (host escape, unexpected host FS access)
- **Install script** (`scripts/install.sh`) — supply chain / install-path issues
- **Remote API auth** (`api_token` / `GRAIN_TOKEN`, non-loopback bind rules)

## Out of scope

- **Guest workload compromise without host escape.** The expected isolation boundary is the microVM (QEMU/HVF or Firecracker/KVM). Code running as root *inside* a guest that cannot reach the host is not a grain host vulnerability by itself.
- Multi-tenant hard isolation for untrusted co-tenants on one host without additional hardening (see [Security model](https://grainvm.com/explain/security/)).
- Issues solely in third-party guests, base cloud images, or tools you run inside sandboxes (for example act, Docker, k3s), unless grain misconfigures them in a way that breaks the host boundary.

## Safe defaults (operators)

**Never open the API on `0.0.0.0` (or any non-loopback address) without `api_token`.** The daemon refuses insecure open binds; do not bypass this with custom builds or reverse proxies that strip auth.

- Prefer `api: 127.0.0.1:7474` plus SSH tunnel (`ssh -L 7474:127.0.0.1:7474 host`) or a **TLS-terminating reverse proxy** in front of loopback.
- The daemon serves **cleartext HTTP**. A Bearer `api_token` authenticates clients but does **not** encrypt the path — on a shared LAN, tokens and request bodies are sniffable if you dial `http://host:7474` directly. Prefer tunnel-to-loopback or `https://` via a reverse proxy.
- CLI clients print a one-time stderr warning when `GRAIN_API` / `--api` / `api_url` is non-loopback `http://`. Silence only if you accept the risk: `GRAIN_INSECURE_HTTP=1`.
- Firewall control-plane and egress-proxy ports on shared hosts.
- Guest agent (`grain-agent` on guest `:7475`) is **unauthenticated**; hostfwd is loopback-only. Remote access should use the **authenticated daemon proxy**, not raw agent ports.
- `network: overlay` puts VMs on a **shared L2** — peers can reach each other’s agents. Use only among mutually trusted guests; default `slirp` keeps guests isolated from each other.
- Full team-box setup: **[Remote sandbox host](https://grainvm.com/guides/remote-host/)** (`docs/content/docs/main/guides/remote-host.md`). Happy path: **[Remote lab](https://grainvm.com/guides/remote-lab/)**.

## Further reading

- [Security model](https://grainvm.com/explain/security/)
- [Overlay network](https://grainvm.com/guides/networking-overlay/)
- [Remote sandbox host](https://grainvm.com/guides/remote-host/)
- [Contributing](CONTRIBUTING.md)
