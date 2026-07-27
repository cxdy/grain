# Release checklist — v0.2.2

Patch release: **`grain up` stale-daemon** fix, **`grain uninstall`**, coverage-comment CI gate. CLI + guest agent only.

SDKs stay at **0.2.0** (`@cxdy/grain`, `grainvm`) — no npm/PyPI tag unless those packages change.

## Binaries

- [ ] Tag **`v0.2.2`** → GoReleaser publishes macOS/Linux × amd64/arm64 + linux agents
- [ ] Confirm [releases/latest](https://github.com/cxdy/grain/releases/latest) is **v0.2.2**
- [ ] Install script pulls the new binary:

  ```bash
  curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
  grain version   # expect 0.2.2
  ```

## Changelog

- [x] **CHANGELOG** `[0.2.2]` — up/down daemon hygiene; uninstall; fork-only coverage comment

## Smoke (optional)

- [ ] `grain up` twice → second reports already up (no second process)
- [ ] Stale pid/socket after kill → `grain up` cleans and starts cleanly
- [ ] `grain uninstall` removes binary; `--purge` removes data dir after confirm

## After tag

- [ ] GitHub Release notes link CHANGELOG + grainvm.com
- [ ] No SDK re-publish unless intentionally bumping packages
