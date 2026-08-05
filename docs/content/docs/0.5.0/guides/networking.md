---
title: "Networking and ports (SLIRP, publish, hostfwd)"
description: "SLIRP, publish, live forwards, and limits."
section: guides
keywords:
  - networking
  - ports
  - publish
  - hostfwd
  - SLIRP
  - SSH
  - port forward
---

{{< only-need href="get-started/quickstart/" >}}
Publish a port on create with `-P` and open a shell — no deep networking yet.
{{< /only-need >}}

grain VMs use QEMU **user networking (SLIRP)**. Each VM gets a private guest network; the host reaches guest services only through **hostfwd** port mappings bound to `127.0.0.1`.

## Built-in SSH forward

On every start, grain allocates a free host TCP port and maps it to guest port **22**:

```text
hostfwd=tcp:127.0.0.1:<sshPort>-:22
```

`grain sh` / `grain x --ssh` / `grain cp --ssh` use that port and the key under `~/.grain/ssh/`.  
(Default `x` / `cp` prefer the guest agent on the forwarded agent port when healthy.)  
List SSH forwards with:

```bash
grain fwd ls
# NAME         PROTO  HOST       GUEST      NOTE
# sbox-1       tcp    :52341     22         ssh
```

SSH ports are **re-allocated** each time the VM starts (they are not fixed across restarts).

## Publish extra ports (`--publish` / `-P`)

At create time, publish host→guest ports:

```bash
# fixed host port → guest port
grain new -P 8080:80

# auto host port (omit host, or use 0)
grain new -P 80
grain new -P 0:443

# multiple
grain new -P 8080:80 -P 4430:443

# optional proto prefix (default tcp)
grain new -P tcp/8080:80
grain new -P udp/5353:53
```

Accepted forms for each `-P` value:

| Form | Meaning |
|------|---------|
| `HOST:GUEST` | map host `HOST` → guest `GUEST` |
| `GUEST` or `:GUEST` or `0:GUEST` | allocate a free host port → guest `GUEST` |
| `tcp/HOST:GUEST`, `udp/HOST:GUEST` | same with explicit protocol |

Forwards are stored in the VM metadata and **re-applied on `grain start`**. Host ports that were auto-allocated at create stay in meta; explicit host ports are reused as stored.

### Limits

- **Host ports &lt; 1024** (privileged) are rejected. Use a port ≥ 1024, or omit the host side so grain picks a free high port.
- Guest ports must be in `1–65535`.
- Protocols: `tcp` or `udp` only.
- All hostfwds bind to **loopback only** (`127.0.0.1`), not `0.0.0.0`.
- Create-time SLIRP hostfwds are fixed for the life of the process; change them by recreating the VM. `grain start` re-applies stored SLIRP forwards.
- **Live forwards** can be added while a VM is running via `grain fwd add` (SSH local tunnels).

## Live forwards (running VM)

Hot-add a host→guest mapping without recreating the VM:

```bash
grain fwd add sbox-1 8080:80
grain fwd rm  sbox-1 8080
```

API: `POST /vms/{name}/forwards`, `DELETE /vms/{name}/forwards/{hostPort}`.
Live forwards are cleared on stop/delete.

## Inspect forwards

```bash
grain fwd ls           # all VMs
grain fwd ls sbox-1    # one VM
```

Shows the built-in SSH row plus any `--publish` entries.

## What SLIRP does *not* do

- No bridged/TAP networking and no guest-visible LAN IP on the host interface.
- No inbound connections from other machines on your network (loopback hostfwd only).
- Guest outbound internet works through SLIRP (typical QEMU user-net behavior).

## Egress proxy (optional)

From the guest, the host is **`10.0.2.2`**. Run `grain proxy up` on the host
(default listen `0.0.0.0:3128`) and point `HTTPS_PROXY` at
`http://TOKEN@10.0.2.2:3128` for default-deny allowlisted HTTP(S) and optional
secret injection. Details: [Egress proxy](../proxy/).

## Related

- [Mounts](../mounts/) — share host directories into the guest
- [Egress proxy](../proxy/) — allowlist + secret injection
- [Troubleshooting](../troubleshooting/) — SSH wait, serial logs, resource caps
