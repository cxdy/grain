---
title: "Secrets (host store + guest inject)"
description: Store secrets on the host and materialize them into a guest VM without baking them into images.
section: guides
keywords:
  - secrets
  - inject
  - credentials
  - materialize
  - grain secret
---

{{< only-need href="guides/proxy/" >}}
API tokens the guest should never see as files — use egress proxy inject.
{{< /only-need >}}

## Host store

Secrets live under `~/.grain/secrets/<name>/` (directory mode `0700`, data `0600`).

```bash
grain secret set db-pass --value 's3cret'
grain secret set tls-key --from-file ./key.pem --mode 0600
grain secret ls
```

## Inject into a VM

Requires a healthy guest agent:

```bash
grain secret inject sbox-1 db-pass
# default path: /run/grain/secrets/db-pass

grain secret inject sbox-1 db-pass --path /etc/app/secret
```

## API

| Method | Path |
|--------|------|
| `GET/POST` | `/secrets` |
| `GET/PATCH/DELETE` | `/secrets/{name}` |
| `POST` | `/vms/{name}/secrets/{secretName}` |

## Compared to the egress proxy

| Feature | Secrets inject | [Egress proxy](../proxy/) |
|---------|----------------|--------------|
| Where secret lives | File **inside** the guest | Only on the **host**; injected on matching HTTP(S) requests |
| Best for | Config files, certs, app secrets the process must read | API tokens you do not want in the sandbox filesystem |

Inject when guest software must open the material as a file. Use the proxy when the guest must never see the raw credential.
