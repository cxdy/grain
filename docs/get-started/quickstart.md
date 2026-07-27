---
title: Quick start
description: Install grain, drop in a starter config, and open a shell in a Linux microVM.
---

From zero to a working sandbox in a few minutes. When you finish this page you will have:

1. The **grain** CLI and **QEMU** installed  
2. An optional **starter config** at `~/.grain/config.yaml`  
3. A running microVM you can shell into and tear down  

**Platforms:** macOS and Linux (amd64 / arm64). **Not supported:** Windows or WSL.

Deep dives live elsewhere: [Install]({{ '/get-started/install/' | relative_url }}) · [First sandbox tutorial + demo]({{ '/get-started/first-sandbox/' | relative_url }}) · [Config reference]({{ '/reference/config/' | relative_url }}).

---

## 1. Install

### macOS

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
brew install qemu
grain doctor
```

### Linux (Debian / Ubuntu)

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
sudo apt-get update
sudo apt-get install -y qemu-system qemu-utils
grain doctor
```

### Linux (Fedora)

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
sudo dnf install -y qemu-system-x86 qemu-img
grain doctor
```

`grain doctor` checks QEMU, data dirs, and soft-warns if the guest agent binary is missing (OK for first pull of the golden image).

Other install paths (release binary, `go install`, from source): [Install grain]({{ '/get-started/install/' | relative_url }}).

---

## 2. Starter config (optional)

Defaults work with **no** config file. A starter file is still useful for size defaults and a named **profile**.

```bash
mkdir -p ~/.grain
cat > ~/.grain/config.yaml <<'EOF'
# ~/.grain/config.yaml — starter for local use
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474

# Defaults for `grain new`
image: grain-ubuntu          # after: grain image pull grain-ubuntu
cpus: 2
memory_mb: 2048
disk_gb: 8
ssh_user: ubuntu
ready_timeout: 2m
log_level: info

# Soft caps (stop runaway sandboxes)
max_vms: 8
max_cpus_total: 16
max_memory_mb_total: 32768

# Named profile: grain new --profile work
profiles:
  work:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    image: grain-ubuntu
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}   # host port auto-assigned
EOF
```

| Setting | Meaning |
|---------|---------|
| `api` | Daemon TCP listen (CLI also uses the unix socket by default) |
| `image` | Default base image id for creates |
| `cpus` / `memory_mb` / `disk_gb` | Size defaults for `grain new` |
| `profiles.work` | Reusable create knobs + mount + port forward |

All fields: [Configuration reference]({{ '/reference/config/' | relative_url }}). Remote / team hosts need a token and different bind rules — see [Remote sandbox host]({{ '/guides/remote-host/' | relative_url }}).

---

## 3. First sandbox

```bash
# Start the local daemon (once per session)
grain up

# Download the golden base image once (agent baked in)
grain image pull grain-ubuntu

# Create a microVM (uses defaults / profile)
grain new
# or: grain new --profile work

# Shell in (name optional if only one VM)
grain sh

# One-shot command without an interactive shell
grain x -- uname -a

# List and clean up
grain ls
grain rm
grain down
```

You should see create progress (image → disk → seed → boot → agent), then something like:

```text
created sbox-1  status=running  image=grain-ubuntu  persist=false  ssh=:PORT
next:  grain sh sbox-1
```

| Command | What it does |
|---------|----------------|
| `grain up` / `down` | Start / stop the local daemon |
| `grain image pull` | Download a base image once |
| `grain image ls` | Show local / catalog images |
| `grain new` | Create a microVM |
| `grain sh` | Interactive shell (agent PTY when healthy) |
| `grain x -- …` | One-shot exec in the guest |
| `grain ls` / `rm` | List / delete VMs |

Ephemeral is the default: `rm`, `stop`, or daemon restart removes the VM. Use `grain new -p` for a persistent disk (`stop` / `start` later).

---

## 4. Real workloads (act & k3s)

These are the two features most people try after a plain shell.

### GitHub Actions — `grain act`

From a repo with `.github/workflows/`:

```bash
grain act -- -l              # list workflows / jobs
grain act -- -j test         # run a job in an ephemeral microVM
```

Docker + [act](https://github.com/nektos/act) run **inside** the guest so host Docker stays clean. Full guide: [GitHub Actions with act]({{ '/guides/recipes/act/' | relative_url }}).

### Throwaway k3s lab

```bash
grain new --preset k3s -n lab -p --wait userdata
grain fwd ls lab             # host port → guest 6443
```

Pull kubeconfig and use host `kubectl` — see [k3s recipe]({{ '/guides/recipes/k3s/' | relative_url }}).

---

## 5. Useful next commands

Once `grain up` is running and an image is local:

```bash
# Mount the current directory into the guest
grain new -v "$(pwd):/work"
grain sh
# guest: ls /work

# Publish a host port to a guest port
grain new -P 8080:80
grain fwd ls

# Named profile from the starter config
grain new --profile work

# Keep the disk across stop/start
grain new -p -n lab
grain stop lab
grain start lab

# Guest file copy (agent preferred)
grain cp ./hello.sh lab:/tmp/hello.sh
grain x lab -- cat /tmp/hello.sh

# Docker-in-guest preset
grain new --preset docker
```

More: [Profiles & presets]({{ '/guides/profiles/' | relative_url }}) · [Mounts]({{ '/guides/mounts/' | relative_url }}) · [Networking]({{ '/guides/networking/' | relative_url }}).

---

## 6. If something fails

```bash
grain doctor
grain logs <name>          # guest serial
grain logs --qemu <name>   # hypervisor log
```

Common fixes:

| Symptom | Try |
|---------|-----|
| `grain: command not found` | Ensure install dir is on `PATH` (`/usr/local/bin` or `~/.local/bin`) |
| Doctor fails on QEMU | Install QEMU (step 1); reopen the terminal |
| Image pull fails | Check network; list images with `grain image ls` |
| Create hangs / no shell | Prefer `grain-ubuntu`; see [Troubleshooting]({{ '/guides/troubleshooting/' | relative_url }}) |

---

## 7. Automation (optional)

The daemon exposes HTTP on the unix socket and optional TCP (`api: 127.0.0.1:7474`). Spec: [OpenAPI](https://github.com/cxdy/grain/blob/main/api/openapi.yaml).

```bash
# curl over the local socket
curl --unix-socket ~/.grain/grain.sock http://grain/vms
```

| SDK | Install |
|-----|---------|
| [Go]({{ '/reference/go-sdk/' | relative_url }}) | `go get github.com/cxdy/grain/client` |
| [TypeScript]({{ '/reference/typescript-sdk/' | relative_url }}) | `npm install @cxdy/grain` |
| [Python]({{ '/reference/python-sdk/' | relative_url }}) | `pip install cxdy-grain` |

---

## Next steps

- [GitHub Actions (act)]({{ '/guides/recipes/act/' | relative_url }}) · [k3s lab]({{ '/guides/recipes/k3s/' | relative_url }})  
- [Your first sandbox]({{ '/get-started/first-sandbox/' | relative_url }}) — interactive demo + deeper walkthrough  
- [Core concepts]({{ '/get-started/concepts/' | relative_url }}) — daemon, images, agent, ephemeral vs persistent  
- [Guides]({{ '/guides/' | relative_url }}) — images, agent, mounts, proxy, remote host  
- [CLI reference]({{ '/reference/cli/' | relative_url }}) · [HTTP API]({{ '/reference/api/' | relative_url }})  

