---
title: Contributing
description: How to build, test, and send changes to grain — full guide on GitHub.
---

grain welcomes patches. Day-to-day contributor setup lives in the repo so it stays next to `justfile` and CI.

## Full guide

**[CONTRIBUTING.md on GitHub](https://github.com/cxdy/grain/blob/main/CONTRIBUTING.md)** covers:

- Prerequisites (Go 1.23+, QEMU, `just test` / `just smoke-api`)
- Mock hypervisor for unit tests; live QEMU when you need it
- `just build && just agent-linux` before agent deploy / `grain act` on non-golden images
- Coding norms and thin SDKs (`client/`, `sdk/ts`, `sdk/python`)
- PR expectations (tests, docs when user-facing)
- Supported platforms: **macOS and Linux only** (not Windows/WSL)

## Quick path

```bash
git clone https://github.com/cxdy/grain.git
cd grain
just test && just build
./bin/grain doctor
```

Docs site source is this `docs/` tree. Product overview: [grainvm.com](https://grainvm.com).

## Community

- [Security policy](https://github.com/cxdy/grain/blob/main/SECURITY.md) — private vulnerability reporting
- [Code of conduct](https://github.com/cxdy/grain/blob/main/CODE_OF_CONDUCT.md)
- [v0.2 release checklist](https://github.com/cxdy/grain/blob/main/docs/RELEASE.md) (maintainers)
