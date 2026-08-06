# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.8.0] - 2026-08-06

Product-facing **minor** release after **v0.7.0**: **Grain Desktop** (app + **release binaries**), recipe library, warm pool / fast create, activity + metrics defaults, and **supported** Firecracker on Linux+KVM. Default hypervisor remains **QEMU**. Cold Ubuntu boot is still guest-bound (~seconds); sub-second “ready” means template/pool claim, not full cold boot.

### Highlights

| Theme | What shipped |
|-------|----------------|
| **Grain Desktop** | Operator GUI **and** GitHub Release assets: `Grain_darwin_<arch>.app.tar.gz`, `grain-desktop_linux_<arch>.tar.gz` (`install.sh --desktop`). Sandboxes, recipes, warm pool Settings, activity, bulk preflight, multi-Run v2 |
| **Recipes** | `~/.grain/recipes` library, official catalog, form builder, deploy preflight, `go-dev` + expanded pack |
| **Fast create** | `grain new --from`, warm pool (`grain pool *` / `--from-pool`), optional `warm_pool.running` |
| **Firecracker** | **vFC-1** agent + **vFC-2** partial TAP publish/fwd; catalog `fc-kernel` / `grain-ubuntu-fc` |
| **Ops / docs** | Activity ring, metrics default on, docs single-tree versioning (#88), README product surface |

**Not in this release:** Apple notarization of Desktop (ad-hoc codesign on macOS assets), FC on macOS, multi-tenant RBAC.

### Added (release packaging)

- **Desktop release binaries** — CI workflow `Release Desktop` builds Wails Desktop on macOS (arm64/amd64) and Linux (amd64/arm64) and uploads assets to the `v*` GitHub Release. `scripts/install.sh --desktop` installs `~/Applications/Grain.app` (macOS) or `grain-desktop` (Linux) from those assets.

### Fixed

- **golangci-lint** — G118: warm-pool background fill/refill uses `context.WithoutCancel` instead of `context.Background` when a request/daemon context is in scope; staticcheck S1008/QF1012 cleanups in activity classify and multi-Run export.
- **Agent listen before clipboard/X11** — TCP bind (and health) no longer waits on Xvfb/clipboard setup (CI `TestHealth` flaked when setup exceeded the wait window).

### Docs

- **Product surface refresh** — CLI/API/config/metrics references document warm pool, `--from` / `--from-pool`, activity, `/info` caps, sandbox metrics defaults; Desktop, lifecycle (create latency), recipes (`go-dev`), quickstart/install, and root README (Desktop, recipes, Firecracker) updated for the last wave of product work.

### Added

- **Docs versioning (issue #88)** — single live content tree `docs/content/docs/main/` only; removed per-release `docs/content/docs/0.x.y/` snapshots. Version switcher lists product SVU tags (`vX.Y.Z`, not fc/golden/sdk tags) with **git commit** links to GitHub; live site stays `/docs/main/`. `scripts/docs-version-bump.sh` updates metadata only.
- **GET /info resource caps** — `max_vms`, `max_cpus_total`, `max_memory_mb_total` (and per-VM caps) so Desktop bulk-start preflight hard-blocks on **remote** hosts, not only local config.
- **Multi-host Run v2** — re-run failed hosts only, copy/export all results (stdout+stderr), clearer FAILED tagging.
- **Official recipe `go-dev`** — Go toolchain bootstrap; catalog entry. GitHub Release `download_count` helper for catalog/stats without extra infra.
- **Desktop warm-path polish** — Settings pool status shows suspended vs running honesty; create prefer-pool copy; promote+fill loop docs.
- **Desktop warm-pool product path** — Settings → Warm pool (template / size / optional running mode) applies config and restarts the local daemon; More → **Promote to golden + fill pool**; New sandbox **prefers pool claim** when ready > 0 with honest empty/unconfigured status. Optional `warm_pool.running: true` keeps members agent-ready (RAM cost); claim is rename-only in that mode.
- **Desktop bulk start preflight** — before multi-start fan-out, estimates capacity against `max_vms` / `max_cpus_total` / `max_memory_mb_total` (from active host `GET /info`, else local config) and **blocks** over-cap batches or **warns** when caps are unknown.
- **Activity source filter** — Activity drawer filter by `desktop` / `cli` / `mcp` / `api` (existing `X-Grain-Client` labels). Pool/create APIs always use the active host connection (remote parity).
- **Warm pool** — config `warm_pool.template` + `warm_pool.size` pre-clones suspended template VMs (disk only). `grain pool status|fill|claim|drain`, `grain new --from-pool -n NAME`, API `GET /pool` / `POST /pool/{fill,claim,drain}` and create `from_pool: true`. Claim renames a ready member and starts with `-loadvm` when snapshotted; async refill + fill on daemon start. Host logs `pool claim timing`.
- **Fast create from suspended template** — `grain new --from TEMPLATE -n NAME` (API `POST /vms` body `{"from":"TEMPLATE","name":"NAME"}`). Clones a stopped/suspended persistent VM and starts it; when the template was `grain suspend`'d with a qcow2 snapshot, QEMU `-loadvm` restores memory and agent wait is short (seconds, not full cold boot). Clone copies suspend markers. Host logs `spawn timing`.
- **Desktop official recipe preview** — `PreviewOfficialRecipe` fetches catalog YAML without installing (sha256 when pinned). Offline: local library copy if installed, else index description only with a clear warning.

### Added

- **Daemon activity feed** — `GET /activity` returns a ring of recent control-plane mutations (create/start/stop/rm/exec/…). Clients send `User-Agent` / `X-Grain-Client` (`cli`, `desktop`, `mcp`, `sdk`). Desktop Activity drawer polls and merges daemon events with local UI notes so CLI/MCP/API actions appear, not only Desktop clicks.
- **Persistent activity log** — activity ring stored at `data_dir/activity.json` (survives `grain down` / restart).
- **Background metrics sampler** — daemon periodically samples guest `/stats` into each VM’s on-disk `metrics.ring` when metrics are enabled (not only when Desktop polls).
- **Desktop fast create** — New sandbox modes: cold boot, **from template** (stopped/suspended), **from warm pool** (status + Fill).
- **Multi-host Run progressive results** — hosts stream in as each `ExecOne` finishes; recent command history.
- **Sandbox list density** — Compact rows, sort by name/status/newest, status group headers.

### Changed

- **Sandbox metrics default on** — `sandbox_metrics_enabled` defaults to **true**; new sandboxes write host-side history under `vms/<name>/metrics.ring`. Opt out with `sandbox_metrics_enabled: false` or per-create `metrics_enabled: false`. History is still readable after disable.
- **Defer disk grow after agent** — when clone disk is larger than the base image, skip cloud-init growpart/resizefs during boot; grow root FS via agent after wait-ready (faster agent-ready path).
- **Official recipe titles** — human-readable catalog titles (ids unchanged).

### Changed

- **Desktop shell input** — xterm keystrokes call `ShellWrite` via live Wails bindings (was a dead `go?.ShellWrite` reference, so the in-app and pop-out shells ignored keyboard input).
- **Desktop sandbox list** — always-scrollable list; search filter by name/image/status; multi-select **Run…** opens a parallel command panel (`name: output` lines via agent `sh -c`).
- **Desktop bulk actions** — confirm dialogs for bulk/single start/stop/remove (Wails-safe, not `window.confirm`); parallel pool with progress bar; multi-Run output uses terminal styling with selectable brand-colored hostnames.
- **Desktop agent badge honesty** — while a golden image is still booting, failed agent `/health` no longer reports **not installed**. UI shows **checking…** until health succeeds, or until 2 minutes after create (then **not responding** / **not installed** as appropriate).
- **Desktop Recipes tab layout** — Import is a File/URL dropdown; official catalog always visible (ID · Summary · Local · Add); row click opens description + YAML preview; denser empty state and detail pane only when a library recipe is open.
- **Create latency instrumentation + leaner golden boots** — daemon logs `create timing` (image/disk/seed/start/wait ms). Agent wait poll 50ms (was 200ms). Agent-ready minimal cloud-init **omits growpart/resizefs** unless the clone disk is larger than the base image (avoids multi-second grow on same-size overlays). Host-side create is typically ~200ms; remaining latency is guest boot/agent (~10s today on grain-ubuntu). Path to sub-second is snapshot/warm-pool (documented in design notes).

### Added

- **Recipes next** — official pack expands (`docker-lab`, `k3s-lab`, `act-lab`, `remote-coding`, `python-dev` + existing); `grain recipe preview`; Desktop **New from form…**, **Save as library recipe**, deploy preflight (image pull CTA, missing mounts, remote-host banner); preset expansion on Desktop deploy. Docs: catalog table, MCP agent sketch, download counts via GitHub Releases only.
- **Recipe library** — `~/.grain/recipes/<name>.yaml` with CLI `grain recipe list|add|search|delete|show|validate`; `grain new --recipe <name>` resolves library names. Official **opt-in** catalog (`recipes/catalog.json` index; bodies on `recipe add <id>`). URL import (http/s). Desktop **Recipes** tab: import file/URL, browse official, YAML edit (valid-only save), Deploy with name override + wait. MCP: `grain_recipe_list|add|search|create` (+ optional `recipe` on `grain_create_vm`). Import never auto-creates VMs. Docs: get-started/recipe, guides/desktop, guides/mcp.
- **Grain Desktop (optional GUI)** — ship-ready operator console (Wails v2, not Electron): **Sandboxes · Images · Recipes · Settings** (Doctor header popup; MCP in Settings); host switcher; right inspector (Overview · Shell · Logs); activity drawer + toasts; multi-select bulk actions; pull progress; Doctor checks; MCP snippets; Advanced YAML with strict unknown-key validation; disk grow via `qemu-img`; **More → Export as recipe…**. Build: `just desktop-build` → `./bin/grain-desktop` / `desktop/build/bin/Grain.app`. Install: `scripts/install.sh --desktop`. Docs: guides/desktop, `desktop/README.md`.
- **`grain check-config [file]`** — parse and validate `config.yaml` (field enums, non-loopback `api_token`, unknown top-level keys). Used by Desktop before applying config edits.

### Security

- **Guest put-tar symlink escape defense** — `grain-agent` put-tar rejects absolute symlink targets and any `..` path component in linknames; regular-file extract uses `O_NOFOLLOW` (Unix) and refuses to write through an existing final-component symlink so a crafted tar cannot escape the extract root or overwrite via alias.
- **Image pull fail-closed digests** — production catalog pulls refuse install when neither a pinned `SHA256` nor a companion `.sha256` sidecar is available (`grain-ubuntu` sidecar required; no silent skip). `ubuntu-cloud` digests refreshed to current noble minimal SHA256SUMS; `alpine-cloud` pins SHA-256 of the published qcow2 (Alpine ships `.sha512`/`.asc` only). Spec field `AllowUnverified` is for tests/dev only.
- **Remote API transport guidance** — CLI prints a one-time stderr warning when dialing a non-loopback cleartext `http://` API URL (Bearer tokens are sniffable); silence with `GRAIN_INSECURE_HTTP=1`. Daemon non-loopback bind warning mentions cleartext HTTP. Docs (`SECURITY.md`, remote-lab, config) recommend SSH tunnel to `127.0.0.1` or HTTPS reverse proxy. `https://` API URLs use the default TLS client.


### Docs

- **Versioned README / top-level links** — user-facing `grainvm.com` paths point at `/docs/<ver>/…` (no more 404s on unversioned `/get-started/…`).
- **Docs version tracks product version** — homepage demo and chrome use `docsVersion` (no hardcoded `grain 0.2.2`); version switcher shows the git **commit** for the selected release.
- **Tag/commit-backed docs versioning** — each `docsVersions` entry records the release-tag commit; `scripts/docs-version-bump.sh` writes commit SHAs so historical versions can be rebuilt from tags without forever-growing full trees as the only model.
- **Firecracker official support** — guides/matrix/parity/config/concepts/architecture no longer label FC as experimental; support policy is **vFC-1 agent + vFC-2 partial net** on Linux+KVM. UDP mounts/overlay remain QEMU-only and are documented as such.
- **Hypervisor feature matrix (QEMU vs Firecracker)** — new explain page comparing capabilities today with production-track target phases (**vFC-1** agent path over FC vsock UDS `CONNECT`, **vFC-2** net/mounts). Linked from Firecracker guide, product surface, and concepts; sidebar entry under Understand. Zero VMM code.
- **Single-tenant / single-operator model** — document multi-user RBAC as an intentional non-goal: one `data_dir` owner (0700), unix socket 0600, shared `api_token` (not per-user roles); shared hosts use separate OS users/`data_dir` or separate hosts. `SECURITY.md`, explain/security, remote-host, remote-lab; config comments near `DataDir` / `APIToken`.


### Added

- **Firecracker vFC-2 partial net** — single-tenant TAP + create-time `-P`/`--publish` and live `grain fwd` via host **TCP proxy** (socat/python3), guest eth0 over agent after vsock; SSH and agent host ports also proxied when allocated. UDP publish / mounts / overlay remain QEMU-only (hard errors on FC). Needs Linux CAP_NET_ADMIN + `/dev/net/tun`. Smoke: `scripts/smoke-fc-net.sh` (guest HTTP + host `curl` proof).
- **Firecracker catalog pull (`fc-latest`)** — `grain image pull fc-kernel` / `grain-ubuntu-fc` downloads from the `fc-latest` release (sidecar digests, fail-closed). Bake workflow publishes **amd64 and arm64** assets. `scripts/smoke-fc.sh` one-shot create→agent→destroy on Linux+KVM.
- **Firecracker bake (`scripts/bake-fc.sh --all`)** — on Linux: fetch Firecracker CI `vmlinux` + Ubuntu squashfs, convert to raw ext4, inject **static** `grain-agent` (systemd, vsock :7475). Outputs `dist/fc/vmlinux-<arch>` + `grain-ubuntu-fc-<arch>.raw` (+ `.sha256`).
- **Firecracker kernel/rootfs import + doctor** — `grain image import <vmlinux> --id fc-kernel` installs to `data_dir/kernels/vmlinux`; `import … --id grain-ubuntu-fc` stores **raw** `disk.raw` (not qcow2). Doctor **hard-fails** missing FC kernel and distinguishes default-path missing vs BYO `kernel_path` misconfig; soft notes when the default image is QEMU-oriented. Start errors use the same wording.
- **Firecracker catalog ID scaffolding** — reserved `grain-ubuntu-fc` (raw rootfs + agent) and `fc-kernel` (vmlinux) catalog entries, **LocalOnly** until bake publishes digests/URLs. Explicit IDs avoid dual-use of QEMU `grain-ubuntu` qcow2. Docs: [Firecracker on Linux](https://grainvm.com/docs/main/guides/firecracker/).
- **`grain sync push|pull --watch`** — re-run sync on a poll interval (default `--interval 2s`) until Ctrl+C. Conflicts and apply errors print and continue; usage errors abort; interrupt exits 0. Incompatible with `--dry-run`.
- **`grain clone SRC DST` / `grain new --clone SRC`** — offline clone of a **stopped persistent** VM: copy root disk + meta under a new name (left stopped; SSH/agent and hostfwd host ports allocated on next start). API: `POST /vms/{name}/clone` with body `{"name":"dst"}`. Refuses running/paused and ephemeral VMs. Limitations: qcow2 overlays keep their backing chain; guest hostname may still match the source; live SSH forwards are not copied.
- **`grain fwd tunnel [name]`** — print ready-to-run `ssh -N -L HOSTPORT:127.0.0.1:HOSTPORT` lines for a VM's published SLIRP and live host ports (daemon host loopback). Flags: `--host`, `--user`, `--json`; default host from `GRAIN_SSH_HOST` or `USER@HOST` placeholder. See [Remote lab](https://grainvm.com/guides/remote-lab/).
- **Builtin profile `remote-coding`** — durable remote lab defaults (`persistent`, 4 CPU / 8192 MiB / 32 GiB, `grain-ubuntu`) without editing config; user `profiles:` with the same name override. CLI root help lists `sync`, `agent deploy`, and the profile.
- **`grain sync push | pull`** — unidirectional incremental host↔guest directory sync via the guest agent (local dial + remote `GRAIN_API` proxy). Host-side baselines under `data_dir/sync/`; `--delete` / `--dry-run` / `--force` / ignore flags; exit `2` on conflicts with zero applies. MCP: `grain_sync_push` / `grain_sync_pull`.
- **`grain agent deploy [name]`** — SCP/install or refresh `grain-agent` in a running guest over SSH. Local CLI SCPs directly; remote CLI (`GRAIN_API`) calls `POST /vms/{name}/agent/deploy` so deploy runs on the daemon host (agent binary must exist there). Docs cover reverse `cp`, remote tunnel + `-p` labs, and agent refresh.
- **Sandbox recipes** — portable YAML (`apiVersion: grain/v1`, `kind: Sandbox`) for create options + ordered bootstrap steps that implement the readiness protocol. CLI: `grain new --recipe file.yaml`, `grain recipe validate|show`. Examples: `examples/recipes/`. Docs: [Sandbox recipes](https://grainvm.com/docs/main/get-started/recipe/).
- **Host clipboard via OSC 52 on `grain sh`** — intercept OSC 52 (and tmux DCS-wrapped) sequences from the guest PTY and copy to the local clipboard (`pbcopy` / `wl-copy` / `xclip` / `xsel`) so tools like Grok Build can copy on highlight inside a sandbox. Disable with `GRAIN_OSC52_CLIPBOARD=0`.
- **`grain sh` forwards host terminal env** — `TERM` / `TERM_PROGRAM` / `COLORTERM` (and locale) into the guest PTY so TUIs can negotiate keyboard protocols the same as local sessions (Shift+Enter newlines in Grok Build, etc.).
- **Guest readiness protocol** — custom images/bootstrap write `/var/lib/grain/readiness/*`; agent `GET /health` includes `readiness` and `GET /readiness`; create wait mode `bootstrap` polls until `state=ready` (fails on `failed`/timeout, VM left running). CLI: `grain status`, create progress surfaces guest messages. Docs: [Readiness protocol](https://grainvm.com/docs/0.3.0/explain/readiness/). Helper: `scripts/grain-ready-report.sh`.

### Changed

- **Firecracker supported operator path** — Linux+KVM guide, doctor, catalog pull, vsock agent, TAP + TCP proxy publish/fwd; cross-linked from concepts, product surface, config, troubleshooting, networking, and architecture. Jailer/overlay/mounts/UDP remain deferred or QEMU-only.

### Docs

- **Firecracker support policy** — **FC agent production (vFC-1)** plus **FC net partial (vFC-2)** for TAP publish/fwd; overlay/mounts/UDP stay QEMU-only. Matrix/parity/FC guide + CLI help updated. Bench: `scripts/bench-fc.sh`; smoke: `scripts/smoke-fc.sh` / `smoke-fc-net.sh`.

### Fixed

- **CLI duplicated error lines** — root Cobra sets `SilenceErrors` so failures print once as `error: …` from `main` (no extra `Error: …` from cobra). Fixes irritating double messages on `grain status` / `grain down` and other RunE failures.
- **Image paste into guest TUIs (Grok Build, etc.) on macOS host** — host `ReadClipboard` still prefers PNG/TIFF→PNG (compiled AppKit helper under `~/.grain/bin` for speed). Guest `pbpaste`/`xclip`/`wl-paste` shims advertise **TARGETS** / MIME types and tolerate `-Prefer`. When **Xvfb** is installed in the guest, the agent owns X11 **CLIPBOARD** on `DISPLAY=:7` and serves host images on SelectionRequest so **arboard-native** apps (not only CLI shims) can paste screenshots over `grain sh` (local or remote API). Large screenshots use ICCCM **INCR** chunking (direct ChangeProperty only under the X max-request-length; failures no longer send a false SelectionNotify). Redeploy guest agent after upgrade (`grain agent deploy`).
- **Firecracker create-time `-P` host→guest** — OUTPUT DNAT of `127.0.0.1` never delivers packets onto the TAP. Create-time publishes, SSH, and agent host ports use the **same host TCP proxy** as live `grain fwd add` after guest eth0 is configured. Teardown removes MASQUERADE and TAP FORWARD rules. Smoke starts a guest HTTP listener and `curl`s both create-time `-P` and live `fwd`.
- **Firecracker doctor / guide pull-first** — missing `fc-kernel` / `grain-ubuntu-fc` recommend `grain image pull …` (BYO import remains fallback); soft notes no longer say “not imported” / “not published yet”. Guide operator checklist + known-limitations table align with vFC-1 agent production (net still QEMU-only); boot sample p50/p95 match AWS `m7i-flex.large` post-merge bench.
- **Install / `grain update` quieter when already set up** — `scripts/install.sh` skips the MCP enable prompt when `~/.grain/config.yaml` already exists, and drops the long “Next steps” footer (install progress + short “grain X installed” / PATH line only).
- **Paste screenshots into `grain sh`** — host clipboard paste prefers **images** (macOS: PNG/TIFF/JPEG via AppKit, converting screenshot TIFF→PNG; Linux: `wl-paste`/`xclip` image MIME types) before plain text. Guest `pbpaste`/xclip shims fetch via the shell control channel. Longer paste timeout for large images.
- **Firecracker agent wait retries dial** — create/start `wait=agent` re-dials Firecracker UDS + CONNECT until the readiness budget expires (guest agent appears after boot; a single early CONNECT EOF no longer fails immediately). SSH agent deploy is skipped when `SSHPort` is 0.
- **Firecracker agent wait errors** — when create/start waits for the guest agent over FC vsock UDS (no SSH deploy fallback), failures name the UDS path and point at `grain doctor` / baked agent / Firecracker guide instead of a bare timeout.
- **Firecracker agent dial (UDS + CONNECT)** — host-side `agent.Dial` speaks Firecracker’s vsock protocol (`connect(unix:…/fc-vsock.sock)` then `CONNECT 7475\n`) so create-wait, manager, daemon API proxy, and local CLI can reach the guest agent without TCP hostfwd or host AF_VSOCK. `TargetForInstance` derives the UDS from `disk_path` when `AgentPort=0` and `AgentCID` is set (FC Start pattern).
- **Firecracker doctor** — when `hypervisor: firecracker`, `grain doctor` hard-fails if `/dev/kvm` is missing or not RDWR-accessible (plus a soft nested-virt CPU flag hint).
- **Firecracker start errors** — if Firecracker dies right after opening its API socket (typical missing-KVM path), create returns `firecracker exited immediately` with the log tail and a KVM hint, instead of a misleading later `vsock … unreachable` agent wait error.
- **MCP in the main binary** — `grain up --mcp` / config `mcp.enabled` + `mcp.listen` (default `127.0.0.1:7476/mcp` Streamable HTTP); `grain mcp` for stdio IDE hosts. Guide: [MCP server](https://grainvm.com/guides/mcp/).
- **Install script MCP prompt** — asks whether to enable MCP by default in `~/.grain/config.yaml` (`GRAIN_ENABLE_MCP=1|0` for non-interactive); declining prints `grain up --mcp` and config snippets.
- **Expanded MCP tools** — streaming `grain_exec` (timeout, progress); write/read file + tar; fs readdir/stat/mkdir/rm; agent_health, logs, stats; workspace_sandbox helper; live port forwards; image list/pull; `grain_act` (GitHub Actions via act); `grain_k3s` lab; create defaults `grain-ubuntu` + `wait=agent`; idempotent delete.

- **`grain update`** — check GitHub Releases for a newer CLI and re-run the install script (`--check` report-only with exit 1 when outdated; `--force` reinstalls even when current).
- **Upgrade notices** — most commands may print a one-line stderr hint when a newer release is known (24h cache under `~/.grain/cache/`). Disable with `check_updates: false`, `GRAIN_CHECK_UPDATES=0`, or `GRAIN_NO_UPDATE_CHECK=1`.

### Removed

- **`grain tray`** — menu bar / system tray helper. It required a CGO-enabled build (not available in portable release archives) or a separate binary; status and lifecycle stay on the CLI and API.

## [0.6.3] - 2026-08-06

### Added

- **Firecracker vFC-2 partial net** — single-tenant TAP + create-time `-P`/`--publish` (iptables DNAT to guest IP), live `grain fwd add` via host TCP proxy (socat/python3), guest eth0 configured over agent after vsock. Overlay/mounts remain QEMU-only. Needs Linux CAP_NET_ADMIN + `/dev/net/tun`. Smoke: `scripts/smoke-fc-net.sh`.

### Fixed

- **macOS screenshot TIFF → PNG** — full-resolution convert via `NSImage.cgImage` (previous path could return a tiny icon-sized image).
- **`grain agent deploy` binary search** — prefer `{data_dir}/agent/` from `grain update` over a stale `./bin/` in a git checkout.

## [0.6.2] - 2026-08-06

### Fixed

- **Screenshot paste over `grain sh` (Cmd/Ctrl+V)** — shell WebSocket read limit raised above the 32KiB default so base64 clipboard replies (real screenshots) are not rejected. Guest tools that read the host clipboard on paste no longer fail on large images. **Requires updated CLI and guest agent** (`grain update`, then redeploy/refresh agent in existing VMs).

## [0.2.2] - 2026-07-27

### Added

- **`grain uninstall`** — stop the local daemon, remove the CLI binary, and optionally purge the data directory with `--purge` (`-y` skips the prompt). Default keeps VMs and images; always drops the installer agent cache and runtime socket/pid files.

### Fixed

- **`grain up`** — detects a healthy existing daemon (`already up`) instead of spawning a second process that races a leftover foreground instance. Dead pid / orphan socket are cleaned before start; background start waits for healthz or child exit. Setsid so shell Ctrl+C does not signal the daemon.
- **`grain down`** — cleans stale runtime files and SIGKILLs a stuck daemon when needed.
- **Coverage comment workflow** — runs only for fork PRs (same-repo PRs already post coverage from `ci.yml`).

### Changed

- Broader unit-test coverage and consolidated `*_test.go` names (CI Cobertura threshold unchanged).

## [0.2.1] - 2026-07-27

### Fixed

- **`grain new` create wait** — for agent-ready images (`grain-ubuntu` / HasAgent), wait the full readiness budget for the guest agent instead of falling back to SSH after ~45s. SSH deploy is still used when needed, but agent health is raced so a late-up agent finishes create without hanging on SSH.
- **Ctrl+C during create** — no longer marks a live VM as `status=error`. The guest stays **running** so `grain sh` / `grain ls` work. `grain ls` also reconciles stale `error`/`creating` to `running` when the hypervisor process is still alive.

## [0.2.0] - 2026-07-27

### Added

- **`grain tray`** — macOS menu bar / Linux system tray status helper.
- **Guest arch** — `grain new --arch arm64|amd64` (and config `guest_arch`). Cross-arch on Apple Silicon uses QEMU TCG for x86_64 guests.
- **Virtio GPU** — `grain new --gpu virtio` / config `gpu: virtio` adds `virtio-gpu-pci`.
- **Overlay network** — `grain new --network overlay` / config `network: overlay` shares an L2 multicast segment between VMs (plus SLIRP for hostfwd).
- Guides: tray, multi-arch, GPU, overlay networking. Product surface page cleaned up (no competitor comparison / deferred-feature table).
- Dev tooling: mise (`.tool-versions`), pre-commit, golangci-lint, markdownlint-cli2, commitizen; `just init` / `just lint` / `just pre-commit`.
- CI coverage: `just coverage` writes Cobertura `coverage.xml`; PR comment posts a compact summary with the full per-file table under a collapsible details block (75% line minimum; cmd/tray excluded).
- Broad unit-test expansion across client, API, manager, hypervisor, agent, CLI helpers, and related packages.
- Interactive **OpenAPI explorer** on the docs site (`/reference/openapi/`).
- **Field Manual** docs site redesign (layout, navigation, and visual system).

### Changed

- **Python SDK** PyPI name is **`grainvm`** (`pip install grainvm`; import still `import grain`). Previous name `cxdy-grain` is not used for new publishes.
- TypeScript SDK **`@cxdy/grain`** and Python SDK **`grainvm`** published as **0.2.0** (aligned with the CLI tag).
- Docs site visual refresh (typography and cool ink/mint/steel palette); OpenAPI explorer readable in light mode.

### Fixed

- Release builds no longer fail on the tray dependency when `CGO_ENABLED=0` (tray is a CGO-only build path).
- OpenAPI YAML descriptions quoted so Swagger UI can parse the published spec.

## [0.1.4] - 2026-07-27

### Fixed

- **`grain act`** no longer dies on first job run waiting for act’s interactive Large/Medium/Micro image prompt. The act preset and `grain act` seed a non-interactive `actrc` (medium-class `catthehacker/ubuntu` images).

## [0.1.3] - 2026-07-27

### Fixed

- **Daemon start** fails fast if the TCP API address is already bound (no half-up process that only holds the unix socket).

## [0.1.2] - 2026-07-27

### Fixed

- **Install script** next-step links pointed at a non-existent `docs/recipes` path; they now open grainvm.com (docs, act, k3s).
- **QEMU discovery** — doctor and the QEMU runtime resolve `qemu-system-*` / `qemu-img` from Homebrew locations (`/opt/homebrew/bin`, `/usr/local/bin`) when those dirs are not on `PATH`.

### Changed

- Installer “next steps” mention `grain act` and the k3s preset.
- GitHub **issue templates** (bug / feature) and **CODEOWNERS**.

## [0.1.1] - 2026-07-27

### Added

- Published SDKs: npm `@cxdy/grain`, PyPI `cxdy-grain`; Go client via `github.com/cxdy/grain/client`.
- Trusted-publishing workflows for npm and PyPI.
- Product docs: quick start, act/k3s homepage surfaces, multi-scenario demo.

### Changed

- GoReleaser archives (`.tar.gz`) as primary install assets; install script supports them.

## [0.1.0] - 2026-07-27

First public release: local Linux microVM control plane for macOS and Linux.

### Added

- **Community docs** — [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md); site page [get-started/contributing](https://grainvm.com/get-started/contributing/); maintainer [docs/RELEASE.md](docs/RELEASE.md) (v0.1.0 checklist).
- **`grain act`** — run [nektos/act](https://github.com/nektos/act) inside an ephemeral microVM (`--preset act`: Docker Engine + act); mounts the project at `/work`, waits for docker/act, streams the run, deletes the sandbox unless `--keep`. Recipe: [guides/recipes/act](https://grainvm.com/guides/recipes/act/).
- **Python client SDK** — `sdk/python` (`cxdy-grain`, `import grain`); stdlib-only TCP/Unix socket client with create/stream, exec, lifecycle, forwards, and guest fs/cp. Docs: [reference/python-sdk](https://grainvm.com/reference/python-sdk/).
- **Guest agent (`grain-agent`)** — in-guest HTTP server for health, streaming/buffered exec, interactive shell (PTY), file copy, filesystem ops, stats, and secret materialization; deploy over SSH when missing; optional vsock transport with TCP hostfwd fallback.
- **Create wait modes** — `auto` (default: agent when image HasAgent, else ssh), `ssh`, `agent`, `userdata`.
- **Golden image `grain-ubuntu`** — bake scripts, CI bake workflow, pull from GitHub Release tag `golden-latest` with companion `.sha256` sidecars (arm64 + amd64); minimal cloud-init seed for agent-ready clones; auto default when local Ready.
- **Multi-distro catalog** — `ubuntu-cloud` (Ubuntu 24.04 minimal) and `alpine-cloud` (Alpine generic UEFI + cloud-init; SSH user `alpine`).
- **Host egress proxy** — default-deny allowlist (`proxy up/down/allow/deny/ls/client`) with optional Authorization inject from host secrets; `new --proxy` for guest `HTTPS_PROXY`.
- **Secrets store** — host-side `secret ls|set|rm|inject` under `~/.grain/secrets`; agent materialize into the guest.
- **Guest stats** — `grain stats` / agent `GET /stats` (uptime, mem, load).
- **Suspend / restore** — stop QEMU and free RAM for persistent VMs; restore from disk or savevm snapshot when available.
- **Pause / resume** — QMP freeze/unfreeze guest vCPUs without tearing down the process.
- **Live port forwards** — `fwd ls|add|rm` plus create-time `--publish`.
- **Virtio-9p mounts** — `-v HOST:GUEST` (and virtiofs on Linux when virtiofsd is available).
- **Profiles & presets** — named create profiles in config; embedded `docker`, `k3s`, and `act` userdata presets.
- **OpenAPI + Go client SDK** — `api/openapi.yaml`, optional Bearer `api_token` / `GRAIN_TOKEN`, public `github.com/cxdy/grain/client`.
- **TypeScript client SDK** — `sdk/ts` (`@cxdy/grain`) thin fetch client for Node automation.
- **Experimental Firecracker** backend on Linux (raw rootfs, vsock agent, known limits documented).
- **Boot benchmark** — `scripts/bench-create.sh` times N creates and prints p50/p95/avg.
- Install script, recipes (coding-agent, k3s, docker-socket, ci-ephemeral, act), grainvm.com Diátaxis site, and feature docs (agent, images, proxy, firecracker, mounts, networking, profiles).
- **Remote sandbox host guide** — run grain as a systemd service for a team box (API token, caps, SSH tunnels, SDKs); example unit under `deploy/systemd/`.
- **Remote CLI** — `GRAIN_API` / `--api` / config `api_url` dial a remote daemon over HTTP; agent shell/exec/cp/fs proxy through the API; non-loopback API bind requires `api_token` (daemon refuses insecure open binds).

### Changed

- Default image selection prefers local **`grain-ubuntu`** when Ready (`image: auto`).
- Create readiness auto-selects **agent wait** for golden / HasAgent images.
- Release binaries can ship the guest agent for multi-arch deploys.
- **WaitAgent** short-probes then SSH-deploys instead of spending the full create timeout probing a missing agent (fixes `grain act` / non-golden `--wait agent` hangs).

### Fixed

- QEMU **OVMF** for amd64 cloud images; CI bake installs OVMF + ISO tools.
- Agent transport **auto** requires a *writable* `/dev/vhost-vsock` (fixes GitHub Actions golden bake Permission denied).
- Golden bake prep commands run entirely in-guest via `sh -c`.

### Explicitly deferred (out of scope for v0.1)

- macOS menu-bar tray app, Rosetta x86_64 guests, GPU/PCI passthrough
- Full HA multi-node cluster networking (single-node k3s preset only)
- Production-hardened Firecracker (CNI/TAP/hostfwd, jailer, catalog FC images) — experimental operator path is documented in [Firecracker on Linux](https://grainvm.com/docs/main/guides/firecracker/); full production track remains deferred
- Measured sub-second marketing claims without hardware bench (use `scripts/bench-create.sh`)
- Windows host and **WSL / WSL2** (macOS + Linux only; use remote API/SDK from Windows if needed)

### Notes

- Platforms: **macOS and Linux** only (amd64 / arm64). Golden images: `golden-latest` on GitHub Releases.
- Install: `curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash` (or release assets for `v0.1.0`).

[Unreleased]: https://github.com/cxdy/grain/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/cxdy/grain/releases/tag/v0.8.0
[0.7.0]: https://github.com/cxdy/grain/releases/tag/v0.7.0
[0.2.2]: https://github.com/cxdy/grain/releases/tag/v0.2.2
[0.2.1]: https://github.com/cxdy/grain/releases/tag/v0.2.1
[0.2.0]: https://github.com/cxdy/grain/releases/tag/v0.2.0
[0.1.4]: https://github.com/cxdy/grain/releases/tag/v0.1.4
[0.1.3]: https://github.com/cxdy/grain/releases/tag/v0.1.3
[0.1.2]: https://github.com/cxdy/grain/releases/tag/v0.1.2
[0.1.1]: https://github.com/cxdy/grain/releases/tag/v0.1.1
[0.1.0]: https://github.com/cxdy/grain/releases/tag/v0.1.0
