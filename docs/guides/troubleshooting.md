---
title: "Troubleshooting"
description: "Diagnose boot, agent, and QEMU issues."
---


## Supported platforms

grain supports **macOS** and **Linux** only (amd64 / arm64). **Windows and WSL are not supported** — the CLI installer rejects them, and nested QEMU inside WSL2 is not a supported configuration. See [Install → Supported platforms]({{ '/get-started/install/' | relative_url }}).

## `grain doctor`

```bash
grain doctor
```


Checks:

| Check | Notes |
|-------|--------|
| data dir | `~/.grain` (or `data_dir`) and subdirs |
| ssh key | creates/loads key under data dir |
| `qemu-system-*` | arch-specific binary; install with `brew install qemu` |
| `qemu-img` | same QEMU package |
| `hdiutil` | macOS only — builds cloud-init seed ISO |
| base image | default `image` from config; pull if missing |
| guest agent binary | soft warning if `grain-agent-linux-$arch` missing (`just agent-linux`); SSH-only still works |
| qmp | soft check that QEMU documents `-qmp` (needed for pause/resume/graceful stop) |
| daemon socket | optional; prints if not running |

Hard failures print `✗` and cause a non-zero exit. Soft items print `·` and do not fail doctor.

Fix reported `✗` lines, then re-run doctor until `all good`.

## Serial and QEMU logs

```bash
grain logs              # only VM, or prompts if many
grain logs sbox-1
grain logs -f sbox-1    # follow (tail -F style)
grain logs --qemu sbox-1
```

| Source | Path |
|--------|------|
| Guest serial (default) | `~/.grain/vms/<name>/serial.log` |
| QEMU process stdout/stderr | `~/.grain/logs/<name>.log` |

Use **serial** for cloud-init, kernel, and login noise. Use **`--qemu`** for hypervisor start failures (missing firmware, bad args, accelerate errors).

Works offline for a named VM by path; without a name it prefers the daemon list, then scans `vms/`.

## VM will not become ready / SSH timeout

Create waits up to the API client timeout (~5 m); SSH readiness uses `ready_timeout` (default **2m**).

1. `grain logs -f <name>` — look for cloud-init errors or stuck boot.
2. `grain logs --qemu <name>` — QEMU failed immediately?
3. Confirm image: `grain image ls` / `grain image pull`.
4. On Apple Silicon, confirm EDK2 firmware is present (see UEFI below).
5. Increase wait in config:

```yaml
ready_timeout: 5m
```

## UEFI / HVF (Apple Silicon and aarch64)

- **accel**: on Darwin, machine type uses **HVF** (`virt,accel=hvf` / `q35,accel=hvf`). On Linux, **KVM** with TCG fallback.
- **UEFI**: aarch64 guests need EDK2 pflash. grain looks for code/vars under Homebrew and common distro paths, e.g.:
  - `/opt/homebrew/share/qemu/edk2-aarch64-code.fd`
  - `/usr/local/share/qemu/edk2-aarch64-code.fd`
  - `/usr/share/AAVMF/…` (Linux)

If firmware is missing, install/reinstall QEMU (`brew install qemu`) and check QEMU logs. Vars are copied per-VM under the VM dir as `flash-vars.fd`.

## cloud-init

- Seed ISO is `seed.iso` in the VM dir (NoCloud, volume label `cidata`).
- macOS builds the seed with **hdiutil**; doctor checks for it.
- Base userdata sets hostname, SSH keys, `ubuntu`/`grain` users, and a `grain-ready` marker.
- `--userdata-file`: plain shell is appended as `runcmd`; `#cloud-config` merges keys (`packages`, `runcmd`, `write_files`, `users` append).
- `-v` mounts add `mount -t 9p …` runcmds on first seed generation.

If the guest never accepts SSH, serial often shows cloud-init failures (bad YAML in userdata, package errors). Fix userdata and **recreate** the VM (seed is first-boot oriented).

## Image pull / SHA mismatch

```text
sha256 mismatch: got … want …
```

The partial download is deleted. Causes: interrupted/corrupt download, or Ubuntu moved the release file while the catalog digest is stale. Retry pull; if it keeps failing, update grain (catalog digests) or check network/proxy MITM.

```text
image "…" not pulled (run: grain image pull …)
```

Run `grain image pull` before `grain new` (or let doctor tell you).

## Resource caps

Defaults (and overrides in `~/.grain/config.yaml`) limit concurrent load. **Stopped** VMs do not count toward running totals. `0` means unlimited for that knob.

```yaml
max_vms: 8
max_cpus_total: 16
max_memory_mb_total: 32768
max_cpus_per_vm: 8
max_memory_mb_per_vm: 16384
```

Errors look like:

```text
resource cap: max_vms is 8 (already 8 running)
resource cap: max_cpus_per_vm is 8 (requested 16)
```

Stop or remove VMs, lower `-c`/`-m`, or raise caps in config and restart the daemon if needed.

## stop / start / rm behavior

| Command | Ephemeral | Persistent (`-p`) |
|---------|-----------|-------------------|
| `grain stop` | deleted | stopped; disk kept |
| `grain start` | n/a (gone) | boots existing disk; re-applies forwards/mounts |
| `grain rm` | deleted | deleted |
| `grain down` | ephemeral cleaned up with daemon lifecycle | persistent disks remain under `vms/` |

`grain start` requires the disk path still present and host mount dirs still valid.

## Daemon not up

```text
daemon not up — run: grain up
```

```bash
grain up
grain doctor   # should show socket OK
```

## Port publish rejected

```text
host port 80 is privileged (< 1024)
```

Use `-P 8080:80` or `-P 80` (auto high host port). See [Networking](/guides/networking/).

## Related

- [Networking](/guides/networking/)
- [Mounts](/guides/mounts/)
- [Images](/guides/images/)
