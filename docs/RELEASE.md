# Release checklist — v0.2.1

Patch release: **create-wait / Ctrl+C** fix. CLI + guest agent only.

SDKs stay at **0.2.0** (`@cxdy/grain`, `grainvm`) — no npm/PyPI tag unless those packages change.

## Binaries

- [ ] Tag **`v0.2.1`** → GoReleaser publishes macOS/Linux × amd64/arm64 + linux agents
- [ ] Confirm [releases/latest](https://github.com/cxdy/grain/releases/latest) is **v0.2.1**
- [ ] Install script pulls the new binary:

  ```bash
  curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
  grain version   # expect 0.2.1
  ```

## Changelog

- [x] **CHANGELOG** `[0.2.1]` — agent full readiness budget; cancel leaves VM running; `ls` reconciles live error VMs

## Smoke (optional)

- [ ] `grain new` with `grain-ubuntu` stays on **waiting agent** and completes without hanging on SSH
- [ ] Ctrl+C mid-create → `grain ls` shows **running** (not error); `grain sh` works

## After tag

- [ ] GitHub Release notes link CHANGELOG + grainvm.com
- [ ] No SDK re-publish unless intentionally bumping 0.2.1 packages
