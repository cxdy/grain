# Next release TODO

Roadmap from a main-branch audit (post coverage #39). Grain’s isolation model and control-plane defaults are solid for local/single-user use; items below are multi-user, remote-LAN, MCP, and agent-lab polish.

**Strategic niche:** hardware-isolated Linux sandboxes on your Mac/Linux (or team box), agent/MCP-first.  
**Do not:** multi-tenant SaaS, Docker Desktop clone, GUI tray revival, Windows host hypervisor, full Firecracker productionization.

---

## What’s already strong

| Area | Notes |
|------|--------|
| API bind policy | Non-loopback `api` refused without `api_token`; Bearer constant-time compare |
| Hostfwd | SSH/agent on **127.0.0.1** only |
| Unix socket | `0600` |
| Path safety | Sync + host tar reject `..` |
| Secrets | Name regex; store `0700` / data `0600` |
| Product core | Lifecycle, agent I/O, golden image, recipes/readiness, remote API, MCP, sync, clipboard/TERM |

---

## Security / correctness

### High

| ID | Item | Status |
|----|------|--------|
| H1 | **MCP HTTP auth + non-loopback bind guard** — `/mcp` has no Bearer; daemon dials unix socket fully privileged; non-loopback MCP not refused | P0 |
| H2 | **Guest agent unauthenticated + overlay** — agent `:7475` no token; overlay L2 ⇒ cross-VM takeover | P1 (docs + optional token) |
| H3 | **QEMU mount path/tag injection** — raw `path=` / `mount_tag=` in fsdev args | P0 |
| H4 | **Remote API cleartext HTTP** — token/secrets sniffable on LAN | P1 (TLS or enforce tunnel story) |
| H5 | **Egress proxy `0.0.0.0:3128`** — auth optional until first client | P1 |

### Medium

- Guest put-tar symlink-then-write escape (defense-in-depth)
- Concurrent create same name (TOCTOU)
- Alpine image empty SHA
- Data dir / meta often world-readable (`0755`/`0644`)
- Host clipboard via guest `GET /clipboard` during `grain sh`
- Silent ignore of bad JSON on create
- No max body on large puts

### Low / non-bugs

- `/healthz` open — intentional
- Loopback API without token — local trust domain
- Agent unauth via loopback hostfwd — intended; remote uses daemon proxy
- Recipe `run` as guest shell — trust the YAML

---

## Feature / product gaps

| Item | Notes | Priority |
|------|--------|----------|
| `grain sync --checksum` | Flag reserved, not active | P0 hide or P1 implement |
| Sync plan summary honesty | kept_dest / conflicts always clear | P0 |
| Sync symlinks | Inventoried, not first-class | P2 |
| `grain agent deploy` remote | Local daemon only | P1 |
| SDK / OpenAPI lag | `wait=bootstrap`, recipes, sync; version lag | P0 |
| MCP vs CLI | Missing pause/suspend/restore/status, secrets, proxy | P1 |
| Firecracker | Experimental | P2 |
| Tunnel helper for published ports | `ssh -L` for remote labs | P1 |
| VM snapshot/clone | Agent fork killer feature | P2 |

### UX cliffs

1. Published ports bind host `127.0.0.1` → tunnels from laptop  
2. `-v` is path on **sandbox host**, not laptop  
3. `grain up --mcp` when already up does not attach MCP  
4. Sync dest-ahead kept unless `--force`  
5. fs/sync require agent; sh/cp fall back to SSH  

---

## P0 — next release (ship / harden)

- [ ] **P0.1** MCP authentication + refuse non-loopback MCP without token  
- [ ] **P0.2** Sanitize QEMU mount host path / tag (reject `,` `=` controls; strict tags)  
- [ ] **P0.3** Sync honesty — plan summary always shows kept/conflicts; hide or implement `--checksum`  
- [ ] **P0.4** Remote lab happy-path doc (token, tunnel, profile, sync, no laptop `-v`)  
- [ ] **P0.5** SDK + OpenAPI: `wait=bootstrap`, version bump, note recipe is CLI-side  
- [ ] **P0.6** (release cut) Package unreleased as a versioned release: changelog, docs version, golden regression  

### P0.6 release cut notes

Stabilize unreleased as ship: sync, recipes, readiness, MCP tools, agent deploy, remote-coding, clipboard/TERM, daemon race fix. Effort **M** (docs versioning, OpenAPI, changelog).

---

## P1 — agent / remote differentiation

- [ ] Sync `--checksum` (content hash refine) — **M**  
- [ ] MCP lifecycle completeness (pause/suspend/restore/status; optional secrets) — **M**  
- [ ] Agent deploy over remote API — **M**  
- [ ] Port-forward tunnel helper (`ssh -L` / machine-readable) — **S–M**  
- [ ] Proxy: auth-on when non-loopback; constant-time token compare — **S**  
- [ ] Overlay/agent isolation docs + optional agent token — **M**  
- [ ] Remote API TLS or hard-require reverse-proxy HTTPS — **M–L**  

---

## P2 — later

- [ ] VM snapshot / clone (qcow2)  
- [ ] Atomic exclusive create  
- [ ] Data dir `0700` defaults  
- [ ] Pin Alpine digests; fail-closed golden sidecar  
- [ ] Firecracker only if Linux density is strategic  
- [ ] Watch / continuous sync (after checksum)  
- [ ] Symlink-aware sync  
- [ ] Multi-user RBAC / quotas  

---

## Explicitly defer

- Multi-tenant SaaS isolation / billing  
- Docker/OCI compatibility layer  
- Production Firecracker + full networking before QEMU agent path is best-in-class  
- Windows host hypervisor  
- GPU/PCI passthrough  
- CGO tray / menubar  
- Bidirectional always-on sync daemon competing with Mutagen  
- Competitor marketing tables in-product  
- Sub-second boot claims without benches  

---

## If we only do five things

1. MCP authentication + bind policy  
2. QEMU fsdev path/tag validation  
3. Ship unreleased as 0.x with SDK/OpenAPI parity  
4. Sync: checksum or hide flag + clearer plan summary  
5. Remote coding: tunnel helper + agent-deploy-over-API  

---

## PR tracking

| Item | Branch / PR |
|------|-------------|
| This file | (docs) |
| P0.1 MCP auth | |
| P0.2 QEMU mounts | |
| P0.3 Sync honesty | |
| P0.4 Remote happy path | |
| P0.5 SDK/OpenAPI | |
