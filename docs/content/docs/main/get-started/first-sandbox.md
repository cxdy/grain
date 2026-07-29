---
title: Your first sandbox
description: Start the grain daemon, pull a base image, create a VM, and open a shell — with an interactive demo.
sandbox_demo: true
section: get-started
---

By the end of this lesson you will understand the real commands to run a Linux microVM and open a shell. Prefer the **interactive demo** first, then run the same steps on your machine.

For the shortest path (install + starter config + cleanup), use [Quick start](/docs/0.2.2/get-started/quickstart/).

## Interactive demo

Simulated terminal — nothing runs on your computer. Type the suggested commands or click **Run step**. After `grain sh`, try guest commands like `uname -a`, then `exit`.

<div class="sandbox-demo-embed">
{{< sandbox-demo >}}
</div>

---

## Do it for real

### 1. Start the daemon

```bash
grain up
```

This starts the local control plane in the background. You should see the unix socket path (default `~/.grain/grain.sock`) and optional TCP API address (default `http://127.0.0.1:7474`).

Check that it is up:

```bash
grain ls
# no vms — create one:  grain new
```

### 2. Pull a base image (once)

Prefer the golden image with the guest agent baked in:

```bash
grain image pull grain-ubuntu
```

If that release asset is not published yet on your arch, use the Ubuntu cloud image:

```bash
grain image pull ubuntu-cloud
```

List what you have:

```bash
grain image ls
```

### 3. Create a sandbox

```bash
grain new
```

With a golden image ready, grain defaults to **agent wait**. You will see progress phases (image → disk → seed → boot → waiting agent) and a final line similar to:

```text
created sbox-1  status=running  image=grain-ubuntu  persist=false  ssh=:PORT
next:  grain sh sbox-1
```

Useful flags later:

| Flag | Meaning |
|------|---------|
| `-p` | Persistent disk |
| `-n NAME` | Explicit name |
| `-P 8080:80` | Publish host port |
| `-v $PWD:/work` | Share a host directory |
| `--wait agent` | Fail create if agent never becomes healthy |

### 4. Open a shell

```bash
grain sh
```

If only one VM exists, the name is optional. grain prefers the **guest agent PTY** when the agent is healthy; use `grain sh --ssh` for classic SSH.

Run a non-interactive command:

```bash
grain x -- uname -a
```

### 5. Clean up

```bash
grain rm          # or: grain stop  (ephemeral VMs are deleted on stop)
grain down        # stop the daemon when you are done for the day
```

## What you learned

- The daemon must be running for VM commands  
- Images are pulled once, then cloned per VM  
- `sh` / `x` talk to the guest agent when possible  
- Ephemeral VMs are disposable by design  

**Next:** [Quick start](/docs/0.2.2/get-started/quickstart/) (config + common flags), [Core concepts](/docs/0.2.2/get-started/concepts/), then a [guide](/docs/0.2.2/guides/) for mounts, proxy, or k3s.
