---
title: Agent vs SSH
description: When grain uses the guest agent, when it falls back to SSH, and why both exist.
section: explain
---

## Roles

| Path | Strengths | Weaknesses |
|------|-----------|------------|
| **Guest agent** | Structured API, streaming exec, fs/cp, health probes, PTY shell | Must be present (baked or deployed); Linux guest binary |
| **SSH** | Universal, interactive muscle memory, bootstrap agent deploy | Host-key noise, scp quirks, harder to automate cleanly |

## Default CLI behavior

| Command | Prefers | Override |
|---------|---------|----------|
| `grain x` | Agent streaming exec | `--ssh` / `--agent` |
| `grain sh` | Agent WebSocket PTY | `--ssh` / `--agent` |
| `grain cp` | Agent Put/Get | `--ssh` / `--agent` |
| `grain fs` | Agent only | — |
| Create deploy | SSH used to install agent when missing | Golden images skip deploy when healthy |

## Create wait modes

| Mode | Ready when |
|------|------------|
| `auto` | Agent if image HasAgent, else SSH |
| `ssh` | SSH accepts; agent soft-deployed |
| `agent` | Agent health required (hard fail) |
| `userdata` | Agent + userdata marker |

## Transport

Host → agent:

1. **TCP hostfwd** to guest `:7475` (always available with QEMU user net)  
2. **virtio-vsock** when `agent_transport` allows and `/dev/vhost-vsock` exists (typically Linux)  

## Practical guidance

- **Day-to-day automation:** agent (`x`, `cp`, `fs`, API exec)  
- **Debugging a broken agent:** `grain sh --ssh` and `grain logs`  
- **Images:** prefer `grain-ubuntu` so agent is already present  
- **Security-sensitive tokens:** prefer [egress proxy](/docs/0.2.2/guides/proxy/) over writing secrets into the guest when possible  
