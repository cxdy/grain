# Networking

grain VMs use QEMU **user networking (SLIRP)**. Each VM gets a private guest network; the host reaches guest services only through **hostfwd** port mappings bound to `127.0.0.1`.

## Built-in SSH forward

On every start, grain allocates a free host TCP port and maps it to guest port **22**:

```text
hostfwd=tcp:127.0.0.1:<sshPort>-:22
```

`grain sh` / `grain x` / `grain cp` use that port and the key under `~/.grain/ssh/`.  
List it with:

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
- Forwards are set **at create only**. There is no hot-add: change mappings by recreating the VM with the desired `-P` flags. `grain start` re-applies stored forwards but does not add new ones.

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

## Related

- [Mounts](mounts.md) — share host directories into the guest
- [Troubleshooting](troubleshooting.md) — SSH wait, serial logs, resource caps
