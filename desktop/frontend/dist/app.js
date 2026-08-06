/**
 * Grain Desktop — review-pass operator console
 */
(function () {
  "use strict";

  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const go = window.go?.main?.App;

  const EVENTS_KEY = "grain-desktop-activity-v2";
  const THEME_KEY = "grain-desktop-theme";

  const state = {
    selected: null,
    sandboxes: [],
    selectedSet: new Set(),
    hostProbes: [],
    term: null,
    fit: null,
    termOnly: null,
    fitOnly: null,
    shellEventsBound: false,
    configEditing: false,
    activeTab: "overview",
    settingsSeg: "general",
    mcpAgent: "cursor",
    mcpSnippets: {},
    pollTimer: null,
    metricsTimer: null,
    confirm: null,
    expandedEvent: null,
    currentView: "sandboxes",
    hostTestedOK: false,
  };

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  async function call(name, ...args) {
    if (!go || typeof go[name] !== "function") throw new Error(`binding ${name} unavailable`);
    return go[name](...args);
  }

  function openModal(id) {
    const m = $(`#${id}`);
    if (m) m.hidden = false;
  }
  function closeModal(id) {
    const m = $(`#${id}`);
    if (m) m.hidden = true;
  }
  function confirmDialog(msg) {
    return new Promise((resolve) => {
      $("#confirm-msg").textContent = msg;
      openModal("modal-confirm");
      state.confirm = resolve;
    });
  }

  /* ── activity ── */
  function loadEvents() {
    try {
      return JSON.parse(localStorage.getItem(EVENTS_KEY) || "[]");
    } catch (_) {
      return [];
    }
  }
  function saveEvents(list) {
    try {
      localStorage.setItem(EVENTS_KEY, JSON.stringify(list.slice(0, 200)));
    } catch (_) {}
  }

  function formatActivityTime(iso) {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "—";
    const pad = (n) => String(n).padStart(2, "0");
    let h = d.getHours();
    const ampm = h >= 12 ? "PM" : "AM";
    h = h % 12;
    if (h === 0) h = 12;
    return `${pad(d.getMonth() + 1)}/${pad(d.getDate())}/${d.getFullYear()} ${h}:${pad(d.getMinutes())}:${pad(d.getSeconds())} ${ampm}`;
  }

  function pushEvent(opts) {
    const list = loadEvents();
    const ev = {
      id: "e" + Date.now() + Math.random().toString(36).slice(2, 7),
      t: new Date().toISOString(),
      action: opts.action || "notify",
      target: opts.target || "",
      status: opts.status || (opts.ok === false ? "error" : "success"),
      duration_ms: opts.duration_ms != null ? opts.duration_ms : null,
      summary: opts.summary || opts.detail || "",
      detail: opts.detail || "",
      ok: opts.status ? opts.status === "success" : opts.ok !== false,
    };
    list.unshift(ev);
    saveEvents(list);
    renderActivity();
    updateActivityBadge();
    return ev;
  }

  function updateEvent(id, patch) {
    const list = loadEvents();
    const i = list.findIndex((e) => e.id === id);
    if (i < 0) return;
    list[i] = { ...list[i], ...patch };
    saveEvents(list);
    renderActivity();
    updateActivityBadge();
  }

  function toast(msg, isError, extra) {
    const el = $("#toast");
    if (!el) return;
    el.hidden = false;
    el.textContent = msg;
    el.classList.toggle("error", !!isError);
    const ev = pushEvent({
      action: extra?.action || "notify",
      target: extra?.target || "",
      status: isError ? "error" : extra?.status || "success",
      duration_ms: extra?.duration_ms,
      summary: extra?.summary || msg,
      detail: extra?.detail || msg,
      ok: !isError,
    });
    el.dataset.eventId = ev.id;
    clearTimeout(toast._t);
    toast._t = setTimeout(() => {
      el.hidden = true;
    }, 5500);
  }

  function updateActivityBadge() {
    const list = loadEvents();
    const n = list.filter((e) => e.status === "error").length;
    const b = $("#activity-badge");
    if (!b) return;
    if (n > 0) {
      b.hidden = false;
      b.textContent = n > 9 ? "9+" : String(n);
    } else b.hidden = true;
  }

  function renderActivity() {
    const root = $("#activity-list");
    if (!root) return;
    const list = loadEvents();
    if (!list.length) {
      root.innerHTML = '<p class="muted" style="padding:1rem;text-align:center">No activity yet.</p>';
      return;
    }
    root.innerHTML = list
      .map((e) => {
        const open = state.expandedEvent === e.id ? "open" : "";
        const hl = state.expandedEvent === e.id ? "highlight" : "";
        const st = e.status || (e.ok ? "success" : "error");
        const stClass =
          st === "success" ? "act-success" : st === "error" ? "act-error" : st === "warning" ? "act-warning" : "act-running";
        const target = e.target ? ` — "${escapeHtml(e.target)}"` : "";
        const dur =
          st === "running"
            ? " — …"
            : e.duration_ms != null
              ? ` — ${e.duration_ms}ms`
              : "";
        const line = `${escapeHtml(formatActivityTime(e.t))} — ${escapeHtml(e.action)}${target} — <span class="${stClass}">${escapeHtml(st)}</span>${dur}`;
        const body = escapeHtml(e.summary || e.detail || "");
        return `<div class="activity-row ${open} ${hl}" data-eid="${escapeHtml(e.id)}">
          <div class="activity-row-head"><div class="activity-line">${line}</div></div>
          <div class="activity-row-body">${body}${e.detail && e.detail !== e.summary ? "\n\n" + escapeHtml(e.detail) : ""}</div>
        </div>`;
      })
      .join("");
    $$(".activity-row-head").forEach((h) => {
      h.addEventListener("click", () => {
        const id = h.parentElement.dataset.eid;
        state.expandedEvent = state.expandedEvent === id ? null : id;
        renderActivity();
      });
    });
  }

  function openActivity(focusId) {
    const d = $("#activity-drawer");
    if (d) d.hidden = false;
    if (focusId) state.expandedEvent = focusId;
    renderActivity();
  }
  function closeActivity() {
    const d = $("#activity-drawer");
    if (d) d.hidden = true;
  }

  async function act(action, fn, meta) {
    const t0 = performance.now();
    try {
      const r = await fn();
      const ms = Math.round(performance.now() - t0);
      pushEvent({
        action,
        target: meta?.target || "",
        status: "success",
        duration_ms: ms,
        summary: meta?.summary || `${action} ok in ${ms}ms`,
        detail: meta?.detail || "",
      });
      return r;
    } catch (e) {
      const ms = Math.round(performance.now() - t0);
      const msg = String(e).replace(/^Error:\s*/i, "");
      pushEvent({
        action,
        target: meta?.target || "",
        status: "error",
        duration_ms: ms,
        summary: msg,
        detail: msg,
      });
      toast(msg, true, { action, target: meta?.target });
      throw e;
    }
  }

  /* ── theme ── */
  function osTheme() {
    if (window.matchMedia?.("(prefers-color-scheme: light)").matches) return "light";
    return "dark";
  }
  function readThemePref() {
    try {
      const v = localStorage.getItem(THEME_KEY);
      if (v === "light" || v === "dark" || v === "system") return v;
    } catch (_) {}
    return "system";
  }
  function applyTheme(theme, opts) {
    const persist = !!(opts && opts.persist);
    let pref = theme;
    if (pref !== "light" && pref !== "dark" && pref !== "system") pref = "system";
    const t = pref === "system" ? osTheme() : pref;
    document.documentElement.setAttribute("data-theme", t);
    const icon = $("#theme-icon");
    if (icon) icon.textContent = t === "light" ? "☾" : "☀";
    const link = $("#hljs-theme");
    if (link)
      link.href = t === "light" ? "./vendor/highlight-github.min.css" : "./vendor/highlight-github-dark.min.css";
    if (persist) {
      try {
        localStorage.setItem(THEME_KEY, pref);
      } catch (_) {}
    }
    const ta = $("#config-raw");
    if (ta) highlightConfig(ta.value);
  }
  function toggleTheme() {
    const cur = document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
    applyTheme(cur === "light" ? "dark" : "light", { persist: true });
  }

  function setHealth(hs) {
    const dot = $("#health-dot");
    const label = $("#health-label");
    if (!dot || !label) return;
    // Only ever reflect the *active* connection — never another host's probe error.
    dot.classList.remove("ok", "bad");
    if (hs?.healthy) {
      dot.classList.add("ok");
      const ver = hs.version ? ` · ${hs.version}` : "";
      label.textContent = `${hs.connection || "host"}${ver}`;
      label.title = "";
    } else {
      dot.classList.add("bad");
      const msg = hs?.message || "unhealthy";
      // Keep pill short; full error on hover
      const short =
        msg.length > 64 ? msg.slice(0, 60).replace(/\s+\S*$/, "") + "…" : msg;
      label.textContent = short;
      label.title = msg;
    }
  }

  /* ── host menu ── */
  async function loadHostMenu() {
    try {
      const probes = (await call("ProbeHosts")) || [];
      state.hostProbes = probes;
      const active = await call("GetActiveConnection");
      state.activeHost = active;
      const avail = probes.filter((p) => p.reachable);
      const unavail = probes.filter((p) => !p.reachable);
      const activeP = probes.find((p) => p.name === active);
      const btnLabel = $("#host-btn-label");
      if (btnLabel) {
        if (activeP?.reachable && activeP.version) {
          btnLabel.textContent = `${active} · ${activeP.version}`;
          btnLabel.title = `${active} · ${activeP.version}`;
        } else if (activeP?.reachable) {
          btnLabel.textContent = active;
          btnLabel.title = active;
        } else if (activeP && !activeP.reachable) {
          btnLabel.textContent = `${active} · offline`;
          btnLabel.title = activeP.error || "unreachable";
        } else {
          btnLabel.textContent = active || "Host";
          btnLabel.title = "";
        }
      }
      const av = $("#host-available");
      const un = $("#host-unavailable");
      const unLab = $("#host-unavail-label");
      if (av) {
        av.innerHTML =
          avail.map((p) => hostItemHTML(p, active)).join("") ||
          `<div class="host-item unavail" disabled>No reachable hosts</div>`;
      }
      if (un && unLab) {
        if (unavail.length) {
          unLab.hidden = false;
          un.innerHTML = unavail
            .map((p) => {
              const err = p.error || "unreachable";
              const isActive = p.name === active;
              // Active-but-down still listed under Unavailable (not selectable for switch)
              return `<button type="button" class="host-item unavail ${isActive ? "active" : ""}" disabled title="${escapeHtml(err)}">
                  <span>${escapeHtml(p.name)}${isActive ? " (current)" : ""}</span>
                  <span class="host-err">${escapeHtml(err.slice(0, 56))}</span>
                </button>`;
            })
            .join("");
        } else {
          unLab.hidden = true;
          un.innerHTML = "";
        }
      }
      $$("#host-available .host-item:not(:disabled)").forEach((b) => {
        b.addEventListener("click", async () => {
          closeHostMenu();
          const name = b.dataset.name;
          if (!name || name === active) return;
          try {
            await act("switch host", () => call("SetActiveConnection", name), { target: name });
            await refreshAll();
          } catch (_) {}
        });
      });
    } catch (e) {
      console.warn(e);
    }
  }

  function hostItemHTML(p, active) {
    const check = p.name === active ? '<span class="check">✓</span>' : "";
    const ver = p.version ? ` · ${escapeHtml(p.version)}` : "";
    return `<button type="button" class="host-item ${p.name === active ? "active" : ""}" data-name="${escapeHtml(p.name)}">
      <span>${escapeHtml(p.name)}${ver}</span>${check}
    </button>`;
  }

  function closeHostMenu() {
    const pop = $("#host-popover");
    const btn = $("#host-btn");
    if (pop) pop.hidden = true;
    if (btn) btn.setAttribute("aria-expanded", "false");
  }
  function toggleHostMenu() {
    const pop = $("#host-popover");
    const btn = $("#host-btn");
    if (!pop) return;
    closeDefaultConnMenu();
    const open = pop.hidden;
    pop.hidden = !open;
    btn?.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) loadHostMenu();
  }

  /* Settings → Default connection (same host-menu chrome as header) */
  function closeDefaultConnMenu() {
    const pop = $("#set-default-conn-popover");
    const btn = $("#set-default-conn-btn");
    if (pop) pop.hidden = true;
    if (btn) btn.setAttribute("aria-expanded", "false");
  }
  function toggleDefaultConnMenu() {
    const pop = $("#set-default-conn-popover");
    const btn = $("#set-default-conn-btn");
    if (!pop) return;
    closeHostMenu();
    const open = pop.hidden;
    pop.hidden = !open;
    btn?.setAttribute("aria-expanded", open ? "true" : "false");
  }
  function setDefaultConnValue(name) {
    const hidden = $("#set-default-conn");
    const label = $("#set-default-conn-label");
    if (hidden) hidden.value = name || "local";
    if (label) {
      label.textContent = name || "local";
      label.title = name || "local";
    }
  }
  function renderDefaultConnMenu(conns, selected) {
    const list = $("#set-default-conn-list");
    const names =
      conns && conns.length
        ? conns.map((c) => c.name).filter(Boolean)
        : ["local"];
    let sel = selected || "local";
    if (!names.includes(sel)) sel = names[0] || "local";
    setDefaultConnValue(sel);
    if (!list) return;
    list.innerHTML = names
      .map((name) => {
        const active = name === sel;
        const check = active ? '<span class="check">✓</span>' : "";
        return `<button type="button" class="host-item ${active ? "active" : ""}" data-name="${escapeHtml(name)}" role="option" aria-selected="${active}">
          <span>${escapeHtml(name)}</span>${check}
        </button>`;
      })
      .join("");
    $$("#set-default-conn-list .host-item").forEach((b) => {
      b.addEventListener("click", () => {
        setDefaultConnValue(b.dataset.name);
        renderDefaultConnMenu(
          names.map((n) => ({ name: n })),
          b.dataset.name
        );
        closeDefaultConnMenu();
      });
    });
  }

  /* ── sandboxes ── */
  function statusBadge(st) {
    const s = (st || "").toLowerCase();
    return `<span class="badge-status ${s}">${escapeHtml(st || "—")}</span>`;
  }
  function agentCell(vm) {
    if (vm.agent_ok === true) {
      const ver = vm.agent_version || "ok";
      return `<span class="badge-agent ok" title="guest agent ${escapeHtml(ver)}">${escapeHtml(ver)}</span>`;
    }
    if ((vm.status || "").toLowerCase() === "running") {
      return `<span class="badge-agent no error agent-tip" title="grain agent deploy ${escapeHtml(vm.name)} to install guest-agent">not installed</span>`;
    }
    return `<span class="badge-agent no">—</span>`;
  }

  function updateBulkBar() {
    const n = (state.sandboxes || []).length;
    const multi = n >= 2;
    // Require ≥2 existing AND ≥2 selected — never show with 0–1 sandboxes
    const showBulk = multi && state.selectedSet.size >= 2;
    const bulk = $("#bulk-bar");
    const th = $("#th-check");
    if (th) {
      th.hidden = !multi;
      if (!multi) th.setAttribute("hidden", "");
      else th.removeAttribute("hidden");
    }
    if (bulk) {
      bulk.hidden = !showBulk;
      if (!showBulk) {
        bulk.setAttribute("hidden", "");
        bulk.style.display = "none";
      } else {
        bulk.removeAttribute("hidden");
        bulk.style.display = "";
      }
    }
    if (!multi) state.selectedSet.clear();
  }

  function renderTable() {
    const tb = $("#vm-tbody");
    if (!tb) return;
    const list = state.sandboxes || [];
    const multi = list.length >= 2;
    if (!multi) state.selectedSet.clear();
    updateBulkBar();

    if (!list.length) {
      updateBulkBar();
      tb.innerHTML = `<tr><td colspan="6" class="empty-cell">No sandboxes yet.<br/><br/>
        <button type="button" class="btn btn-primary" id="empty-new">New sandbox</button></td></tr>`;
      $("#empty-new")?.addEventListener("click", () => openCreate());
      return;
    }
    tb.innerHTML = list
      .map((vm) => {
        const sel = state.selected === vm.name ? "selected" : "";
        const checked = state.selectedSet.has(vm.name) ? "checked" : "";
        const check = multi
          ? `<td><input type="checkbox" class="row-check" data-name="${escapeHtml(vm.name)}" ${checked} /></td>`
          : "";
        return `<tr data-name="${escapeHtml(vm.name)}" class="${sel}">
          ${check}
          <td><strong class="selectable">${escapeHtml(vm.name)}</strong></td>
          <td>${statusBadge(vm.status)}</td>
          <td>${agentCell(vm)}</td>
          <td class="muted selectable">${escapeHtml(vm.image || "—")}</td>
          <td class="muted">${vm.cpus != null && vm.cpus !== "" ? vm.cpus + " vCPUs" : "—"} / ${vm.memory_mb || "—"} MiB</td>
        </tr>`;
      })
      .join("");

    $$("#vm-tbody tr[data-name]").forEach((tr) => {
      tr.addEventListener("click", (e) => {
        if (e.target.classList.contains("row-check")) return;
        selectVM(tr.dataset.name);
      });
    });
    $$(".row-check").forEach((c) => {
      c.addEventListener("click", (e) => e.stopPropagation());
      c.addEventListener("change", () => {
        if (c.checked) state.selectedSet.add(c.dataset.name);
        else state.selectedSet.delete(c.dataset.name);
        updateBulkBar();
        const ca = $("#check-all");
        if (ca) ca.checked = state.selectedSet.size === list.length;
      });
    });
  }

  function isRunning(status) {
    return (status || "").toLowerCase() === "running";
  }
  function isStopped(status) {
    const s = (status || "").toLowerCase();
    return s === "stopped" || s === "suspended" || s === "";
  }

  /** Start only when not running; Stop only when running. Never both. */
  function updateActionButtons(status) {
    const running = isRunning(status);
    const start = $("#btn-start");
    const stop = $("#btn-stop");
    // Explicit show/hide — do not rely on CSS alone (btn display:inline-flex
    // used to override the HTML hidden attribute).
    if (start) {
      start.hidden = running;
      start.style.display = running ? "none" : "";
    }
    if (stop) {
      stop.hidden = !running;
      stop.style.display = running ? "" : "none";
    }
  }

  function imageHasAgent(vm) {
    if (!vm) return false;
    if (vm.has_agent_image === true) return true;
    if (vm.has_agent_image === false) return false;
    const img = (vm.image || "").toLowerCase();
    if (!img) return false;
    if (img === "ubuntu-cloud" || img === "alpine-cloud" || img === "fc-kernel") return false;
    return img === "grain-ubuntu" || img === "grain-ubuntu-fc" || img.startsWith("grain-ubuntu");
  }

  function formatWhen(iso) {
    if (!iso) return "—";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return String(iso);
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
      second: "2-digit",
    });
  }

  function agentLabel(vm) {
    if (!imageHasAgent(vm)) return "n/a (image has no guest agent)";
    if (vm.agent_ok === true) return vm.agent_version ? `ok · ${vm.agent_version}` : "ok";
    if (vm.agent_ok === false) return "not responding (use Install / update agent)";
    if (isRunning(vm.status)) return "checking…";
    return "— (start sandbox to probe)";
  }

  function fillMeta(vm, el) {
    if (!el) return;
    el.innerHTML = `
      <dt>Status</dt><dd>${escapeHtml(vm.status || "—")}</dd>
      <dt>Image</dt><dd>${escapeHtml(vm.image || "—")}</dd>
      <dt>vCPUs</dt><dd>${vm.cpus != null ? vm.cpus : "—"}</dd>
      <dt>Memory</dt><dd>${vm.memory_mb != null ? vm.memory_mb + " MiB" : "—"}</dd>
      <dt>Disk</dt><dd>${vm.disk_gb != null && vm.disk_gb > 0 ? vm.disk_gb + " GiB" : "—"}</dd>
      <dt>Persistent</dt><dd>${vm.persistent ? "yes" : "no"}</dd>
      <dt>SSH port</dt><dd>${vm.ssh_port || "—"}</dd>
      <dt>Agent</dt><dd>${escapeHtml(agentLabel(vm))}</dd>
      <dt>Metrics</dt><dd>${
        !imageHasAgent(vm) ? "n/a" : vm.metrics_enabled ? "on (host ring)" : "off"
      }</dd>
      <dt>Error</dt><dd class="selectable">${escapeHtml(vm.error || "—")}</dd>`;
  }

  function setKV(el, rows) {
    if (!el) return;
    el.innerHTML = rows
      .map(
        ([k, v]) =>
          `<dt>${escapeHtml(k)}</dt><dd class="selectable">${v == null || v === "" ? "—" : escapeHtml(String(v))}</dd>`
      )
      .join("");
  }

  function fillInspector(vm) {
    if (!vm) return;
    const el = $("#inspector-summary");
    if (el) {
      const bits = [
        vm.image || "—",
        vm.cpus != null ? `${vm.cpus} vCPUs` : null,
        vm.memory_mb != null ? `${vm.memory_mb} MiB` : null,
        vm.disk_gb ? `${vm.disk_gb} GiB disk` : null,
      ].filter(Boolean);
      el.textContent = bits.join(" · ");
    }
    setKV($("#inspector-status"), [
      ["State", vm.status || "—"],
      ["Agent", agentLabel(vm)],
      ["Checked", vm.agent_checked_at ? formatWhen(vm.agent_checked_at) : isRunning(vm.status) ? "—" : "—"],
      ["Metrics", !imageHasAgent(vm) ? "n/a (no guest agent)" : vm.metrics_enabled ? "on · host ring" : "off"],
      ["Error", vm.error || "—"],
    ]);
    setKV($("#inspector-resources"), [
      ["vCPUs", vm.cpus != null ? String(vm.cpus) : "—"],
      ["Memory", vm.memory_mb != null ? `${vm.memory_mb} MiB` : "—"],
      ["Disk", vm.disk_gb != null && vm.disk_gb > 0 ? `${vm.disk_gb} GiB` : "—"],
      ["PID", vm.pid ? String(vm.pid) : "—"],
    ]);
    setKV($("#inspector-connectivity"), [
      ["IP", vm.ip || "—"],
      ["SSH", vm.ssh_port ? `localhost:${vm.ssh_port}` : "—"],
      ["Agent port", vm.agent_port ? String(vm.agent_port) : "—"],
      ["Network", vm.network || "—"],
    ]);
    setKV($("#inspector-config"), [
      ["Image", vm.image || "—"],
      ["Persistent", vm.persistent ? "yes" : "no"],
      ["Arch", vm.arch || "—"],
      ["GPU", vm.gpu || "—"],
      ["Created", formatWhen(vm.created_at)],
    ]);
    fillMeta(vm, $("#inspector-meta"));
    fillMeta(vm, $("#detail-meta"));

    // More → Install/update agent only for agent-capable images
    const agentItem = $("#more-agent-item");
    if (agentItem) agentItem.hidden = !imageHasAgent(vm);
  }

  function fillInspectorSummary(vm) {
    fillInspector(vm);
  }

  function fmtBytesShort(n) {
    if (n == null || n === 0) return "0";
    if (n > 1e9) return (n / 1e9).toFixed(1) + "G";
    if (n > 1e6) return (n / 1e6).toFixed(0) + "M";
    if (n > 1e3) return (n / 1e3).toFixed(0) + "K";
    return String(n);
  }

  /* ── Grafana-style metrics charts (shared range + hover + brush) ── */
  const RANGE_MS = {
    "1m": 60e3,
    "5m": 5 * 60e3,
    "15m": 15 * 60e3,
    "30m": 30 * 60e3,
    "1h": 3600e3,
    "3h": 3 * 3600e3,
    "6h": 6 * 3600e3,
    "12h": 12 * 3600e3,
    "24h": 24 * 3600e3,
    all: null,
  };

  const metricsUI = {
    allPoints: [],
    series: {},
    // view window: absolute ms range (null,null = follow preset)
    viewFrom: null,
    viewTo: null,
    preset: "1m",
    hoverT: null,
    brush: null, // { startX, curX, canvas }
    bound: false,
    pad: { l: 8, r: 8, t: 6, b: 6 },
  };

  function formatTs(ms) {
    const d = new Date(ms);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
      hour: "numeric",
      minute: "2-digit",
      second: "2-digit",
      hour12: true,
    });
  }

  function formatRangeLabel(from, to, preset) {
    if (preset && preset !== "custom" && RANGE_MS[preset] !== undefined && preset !== "all") {
      return `Last ${preset}`;
    }
    if (preset === "all") return "All samples";
    if (from != null && to != null) {
      return `${formatTs(from)} — ${formatTs(to)}`;
    }
    return "—";
  }

  function effectiveWindow() {
    const pts = metricsUI.allPoints;
    if (!pts.length) return { from: 0, to: 0, pts: [] };
    const dataFrom = pts[0].t_ms;
    const dataTo = pts[pts.length - 1].t_ms;
    let from = metricsUI.viewFrom;
    let to = metricsUI.viewTo;
    if (from == null || to == null) {
      if (metricsUI.preset === "all" || !RANGE_MS[metricsUI.preset]) {
        from = dataFrom;
        to = dataTo;
      } else {
        to = dataTo;
        from = to - RANGE_MS[metricsUI.preset];
        if (from < dataFrom) from = dataFrom;
      }
    }
    if (from > to) {
      const t = from;
      from = to;
      to = t;
    }
    const filtered = pts.filter((p) => p.t_ms >= from && p.t_ms <= to);
    return { from, to, pts: filtered };
  }

  function seriesValues(key, pts) {
    if (key === "net") {
      // Throughput from cumulative counters (bytes/sec between samples).
      const rates = [];
      for (let i = 0; i < pts.length; i++) {
        if (i === 0) {
          rates.push(0);
          continue;
        }
        const dt = (pts[i].t_ms - pts[i - 1].t_ms) / 1000;
        if (dt <= 0) {
          rates.push(0);
          continue;
        }
        const drx = Math.max(0, (pts[i].net_rx_bytes || 0) - (pts[i - 1].net_rx_bytes || 0));
        const dtx = Math.max(0, (pts[i].net_tx_bytes || 0) - (pts[i - 1].net_tx_bytes || 0));
        rates.push((drx + dtx) / dt);
      }
      return rates;
    }
    return pts.map((p) => {
      if (key === "cpu") return p.load1 || 0;
      if (key === "mem") {
        const t = p.mem_total_bytes || 0;
        const a = p.mem_available_bytes || 0;
        return t > 0 ? ((t - a) / t) * 100 : 0;
      }
      if (key === "disk") {
        const t = p.disk_total_bytes || 0;
        const f = p.disk_free_bytes || 0;
        return t > 0 ? ((t - f) / t) * 100 : 0;
      }
      return 0;
    });
  }

  function fmtRate(bps) {
    if (bps == null || !Number.isFinite(bps) || bps <= 0) return "0 B/s";
    if (bps >= 1e9) return (bps / 1e9).toFixed(2) + " GB/s";
    if (bps >= 1e6) return (bps / 1e6).toFixed(2) + " MB/s";
    if (bps >= 1e3) return (bps / 1e3).toFixed(1) + " KB/s";
    return Math.round(bps) + " B/s";
  }

  function netRateAt(pts, idx) {
    if (!pts || idx <= 0 || idx >= pts.length) return { rx: 0, tx: 0, total: 0 };
    const dt = (pts[idx].t_ms - pts[idx - 1].t_ms) / 1000;
    if (dt <= 0) return { rx: 0, tx: 0, total: 0 };
    const rx = Math.max(0, (pts[idx].net_rx_bytes || 0) - (pts[idx - 1].net_rx_bytes || 0)) / dt;
    const tx = Math.max(0, (pts[idx].net_tx_bytes || 0) - (pts[idx - 1].net_tx_bytes || 0)) / dt;
    return { rx, tx, total: rx + tx };
  }

  function seriesFormat(key, p, pts, idx) {
    if (!p) return "—";
    if (key === "cpu") return `load ${(p.load1 || 0).toFixed(2)}`;
    if (key === "mem") {
      const t = p.mem_total_bytes || 0;
      const a = p.mem_available_bytes || 0;
      const u = t > 0 ? t - a : 0;
      return t > 0 ? `${fmtBytesShort(u)} / ${fmtBytesShort(t)} (${((u / t) * 100).toFixed(0)}%)` : "—";
    }
    if (key === "disk") {
      const t = p.disk_total_bytes || 0;
      const f = p.disk_free_bytes || 0;
      const u = t > 0 ? t - f : 0;
      return t > 0 ? `${fmtBytesShort(u)} / ${fmtBytesShort(t)} (${((u / t) * 100).toFixed(0)}%)` : "—";
    }
    if (key === "net") {
      const r = netRateAt(pts, idx);
      const cumRx = p.net_rx_bytes || 0;
      const cumTx = p.net_tx_bytes || 0;
      if (cumRx === 0 && cumTx === 0) {
        return "↓0 ↑0 · agent may lack net counters — Install / update agent";
      }
      return `↓${fmtRate(r.rx)} ↑${fmtRate(r.tx)} · Σ ↓${fmtBytesShort(cumRx)} ↑${fmtBytesShort(cumTx)}`;
    }
    return "—";
  }

  function chartLayout(canvas, forceResize) {
    const wrap = canvas.parentElement;
    const w = Math.max(1, (wrap && wrap.clientWidth) || canvas.clientWidth || 640);
    const h = 96;
    const dpr = window.devicePixelRatio || 1;
    const pad = metricsUI.pad;
    const need =
      forceResize ||
      canvas._layoutW !== w ||
      canvas._layoutH !== h ||
      canvas._layoutDpr !== dpr;
    if (need) {
      canvas._layoutW = w;
      canvas._layoutH = h;
      canvas._layoutDpr = dpr;
      canvas.width = Math.floor(w * dpr);
      canvas.height = Math.floor(h * dpr);
      canvas.style.width = w + "px";
      canvas.style.height = h + "px";
    }
    const ctx = canvas.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    return { ctx, w, h, pad, plotW: w - pad.l - pad.r, plotH: h - pad.t - pad.b };
  }

  function xForTime(t, from, to, layout) {
    if (to <= from) return layout.pad.l;
    return layout.pad.l + ((t - from) / (to - from)) * layout.plotW;
  }

  function timeForX(x, from, to, layout) {
    const rel = (x - layout.pad.l) / Math.max(layout.plotW, 1);
    return from + Math.min(1, Math.max(0, rel)) * (to - from);
  }

  /** Map pointer X (CSS px) on any chart to a shared 0–1 fraction of the plot. */
  function fracForX(x, layout) {
    return Math.min(1, Math.max(0, (x - layout.pad.l) / Math.max(layout.plotW, 1)));
  }

  function nearestPoint(pts, t) {
    if (!pts.length) return null;
    let best = pts[0];
    let bestD = Math.abs(pts[0].t_ms - t);
    for (const p of pts) {
      const d = Math.abs(p.t_ms - t);
      if (d < bestD) {
        best = p;
        bestD = d;
      }
    }
    return best;
  }

  function drawChart(canvasId, key, color) {
    const c = $(canvasId);
    if (!c) return;
    const layout = chartLayout(c, false);
    const { ctx, w, h, pad } = layout;
    ctx.clearRect(0, 0, w, h);
    const win = effectiveWindow();
    const pts = win.pts;
    const values = seriesValues(key, pts);
    c._metricsKey = key;
    c._metricsColor = color;

    if (values.length < 1) {
      ctx.fillStyle = "rgba(128,128,128,0.4)";
      ctx.font = "12px IBM Plex Sans, sans-serif";
      ctx.fillText("No samples in this range", 10, h / 2);
      return;
    }

    let min = Math.min(...values);
    let max = Math.max(...values);
    if (min === max) {
      min -= 1;
      max += 1;
    }
    // line
    ctx.strokeStyle = color || "#3ddea8";
    ctx.lineWidth = 1.75;
    ctx.beginPath();
    pts.forEach((p, i) => {
      const x = xForTime(p.t_ms, win.from, win.to, layout);
      const y = pad.t + (1 - (values[i] - min) / (max - min)) * layout.plotH;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();

    // brush selection preview (shared time fraction → all charts stay aligned)
    if (metricsUI.brush && metricsUI.brush.f0 != null) {
      const f0 = Math.min(metricsUI.brush.f0, metricsUI.brush.f1);
      const f1 = Math.max(metricsUI.brush.f0, metricsUI.brush.f1);
      const x0 = pad.l + f0 * layout.plotW;
      const x1 = pad.l + f1 * layout.plotW;
      ctx.fillStyle = "rgba(61, 222, 168, 0.14)";
      ctx.fillRect(x0, pad.t, Math.max(1, x1 - x0), layout.plotH);
      ctx.strokeStyle = "rgba(61, 222, 168, 0.6)";
      ctx.lineWidth = 1;
      ctx.strokeRect(x0, pad.t, Math.max(1, x1 - x0), layout.plotH);
    }

    // hover crosshair (synced across all charts via metricsUI.hoverT)
    if (metricsUI.hoverT != null && pts.length) {
      const np = nearestPoint(pts, metricsUI.hoverT);
      if (np) {
        const x = xForTime(np.t_ms, win.from, win.to, layout);
        const vi = pts.indexOf(np);
        const y = pad.t + (1 - (values[vi] - min) / (max - min)) * layout.plotH;
        ctx.strokeStyle = "rgba(236, 234, 228, 0.4)";
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 3]);
        ctx.beginPath();
        ctx.moveTo(x, pad.t);
        ctx.lineTo(x, h - pad.b);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.fillStyle = color || "#3ddea8";
        ctx.beginPath();
        ctx.arc(x, y, 3.5, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = "rgba(0,0,0,0.25)";
        ctx.lineWidth = 1;
        ctx.stroke();
      }
    }
  }

  let _redrawRaf = 0;
  function redrawAllCharts() {
    if (_redrawRaf) cancelAnimationFrame(_redrawRaf);
    _redrawRaf = requestAnimationFrame(() => {
      _redrawRaf = 0;
      drawChart("#chart-cpu", "cpu", "#3ddea8");
      drawChart("#chart-mem", "mem", "#6cb6ff");
      drawChart("#chart-disk", "disk", "#e0b050");
      drawChart("#chart-net", "net", "#c084fc");
      updateRangeChrome();
      updateLiveValues();
    });
  }

  function updateRangeChrome() {
    const win = effectiveWindow();
    const label = $("#range-label");
    const preset = metricsUI.viewFrom != null ? "custom" : metricsUI.preset;
    if (label) label.textContent = formatRangeLabel(win.from, win.to, preset);
    $$(".range-btn").forEach((b) => {
      b.classList.toggle("active", metricsUI.viewFrom == null && b.dataset.range === metricsUI.preset);
    });
    const reset = $("#range-reset");
    if (reset) reset.hidden = metricsUI.viewFrom == null;
  }

  function updateLiveValues() {
    const win = effectiveWindow();
    const pts = win.pts;
    const tipT = metricsUI.hoverT;
    let idx = pts.length - 1;
    let p = pts[idx];
    if (tipT != null && pts.length) {
      p = nearestPoint(pts, tipT);
      idx = pts.indexOf(p);
    }
    $("#m-cpu-val").textContent = p ? (p.load1 || 0).toFixed(2) : "—";
    $("#m-mem-val").textContent = seriesFormat("mem", p, pts, idx);
    $("#m-disk-val").textContent = seriesFormat("disk", p, pts, idx);
    $("#m-net-val").textContent = seriesFormat("net", p, pts, idx);
  }

  function showTooltip(clientX, clientY, html) {
    const tip = $("#chart-tooltip");
    if (!tip) return;
    tip.hidden = false;
    tip.innerHTML = html;
    const pad = 14;
    let x = clientX + pad;
    let y = clientY + pad;
    const tw = tip.offsetWidth || 180;
    const th = tip.offsetHeight || 48;
    if (x + tw > window.innerWidth - 8) x = clientX - tw - pad;
    if (y + th > window.innerHeight - 8) y = clientY - th - pad;
    if (x < 8) x = 8;
    if (y < 8) y = 8;
    tip.style.left = x + "px";
    tip.style.top = y + "px";
  }
  function hideTooltip() {
    const tip = $("#chart-tooltip");
    if (tip) tip.hidden = true;
  }

  function clearCustomRange() {
    metricsUI.viewFrom = null;
    metricsUI.viewTo = null;
    if (metricsUI.preset === "custom") metricsUI.preset = "1m";
    metricsUI.hoverT = null;
    hideTooltip();
    redrawAllCharts();
  }

  function bindChartInteractions() {
    if (metricsUI.bound) return;
    metricsUI.bound = true;
    const ids = ["#chart-cpu", "#chart-mem", "#chart-disk", "#chart-net"];

    ids.forEach((sel) => {
      const c = $(sel);
      if (!c) return;
      c.addEventListener("mousemove", (e) => {
        if (metricsUI.brush) return; // window handler owns brush drag
        const layout = chartLayout(c, false);
        const rect = c.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const win = effectiveWindow();
        if (!win.pts.length) return;
        const t = timeForX(x, win.from, win.to, layout);
        metricsUI.hoverT = t;
        const np = nearestPoint(win.pts, t);
        const key = c._metricsKey || "cpu";
        if (np) {
          const idx = win.pts.indexOf(np);
          showTooltip(
            e.clientX,
            e.clientY,
            `<div class="tip-ts">${escapeHtml(formatTs(np.t_ms))}</div>` +
              `<div class="tip-val">${escapeHtml(seriesFormat(key, np, win.pts, idx))}</div>`
          );
        }
        redrawAllCharts();
      });
      c.addEventListener("mouseleave", () => {
        if (!metricsUI.brush) {
          metricsUI.hoverT = null;
          hideTooltip();
          redrawAllCharts();
        }
      });
      c.addEventListener("mousedown", (e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        const layout = chartLayout(c, false);
        const rect = c.getBoundingClientRect();
        const x = e.clientX - rect.left;
        const f = fracForX(x, layout);
        metricsUI.brush = { f0: f, f1: f, canvas: c };
        hideTooltip();
        redrawAllCharts();
      });
      // Double-click any chart to clear a brush zoom (Grafana-like zoom out)
      c.addEventListener("dblclick", (e) => {
        e.preventDefault();
        if (metricsUI.viewFrom != null) clearCustomRange();
      });
    });

    window.addEventListener("mousemove", (e) => {
      if (!metricsUI.brush) return;
      const c = metricsUI.brush.canvas;
      const layout = chartLayout(c, false);
      const rect = c.getBoundingClientRect();
      const x = Math.min(Math.max(e.clientX - rect.left, 0), rect.width);
      metricsUI.brush.f1 = fracForX(x, layout);
      // Live range preview in toolbar while dragging
      const win = effectiveWindow();
      if (win.pts.length) {
        const f0 = Math.min(metricsUI.brush.f0, metricsUI.brush.f1);
        const f1 = Math.max(metricsUI.brush.f0, metricsUI.brush.f1);
        const from = win.from + f0 * (win.to - win.from);
        const to = win.from + f1 * (win.to - win.from);
        const label = $("#range-label");
        if (label) label.textContent = formatRangeLabel(from, to, "custom");
      }
      redrawAllCharts();
    });
    window.addEventListener("mouseup", () => {
      if (!metricsUI.brush) return;
      const c = metricsUI.brush.canvas;
      const layout = chartLayout(c, false);
      const win = effectiveWindow();
      const f0 = Math.min(metricsUI.brush.f0, metricsUI.brush.f1);
      const f1 = Math.max(metricsUI.brush.f0, metricsUI.brush.f1);
      metricsUI.brush = null;
      // require ~6px of drag relative to plot width
      if (f1 - f0 < 0.01 || !win.pts.length || win.to <= win.from) {
        redrawAllCharts();
        return;
      }
      let from = win.from + f0 * (win.to - win.from);
      let to = win.from + f1 * (win.to - win.from);
      if (to - from < 1000) {
        const mid = (from + to) / 2;
        from = mid - 500;
        to = mid + 500;
      }
      metricsUI.viewFrom = from;
      metricsUI.viewTo = to;
      metricsUI.preset = "custom";
      metricsUI.hoverT = null;
      hideTooltip();
      redrawAllCharts();
    });

    $("#range-presets")?.addEventListener("click", (e) => {
      const btn = e.target.closest("[data-range]");
      if (!btn) return;
      metricsUI.preset = btn.dataset.range;
      metricsUI.viewFrom = null;
      metricsUI.viewTo = null;
      metricsUI.hoverT = null;
      hideTooltip();
      redrawAllCharts();
    });
    $("#range-reset")?.addEventListener("click", () => clearCustomRange());

    // Keep chart width in sync with panel resizes
    window.addEventListener("resize", () => {
      ["#chart-cpu", "#chart-mem", "#chart-disk", "#chart-net"].forEach((sel) => {
        const el = $(sel);
        if (el) chartLayout(el, true);
      });
      redrawAllCharts();
    });
  }

  async function loadMetrics(name) {
    const charts = $("#metrics-charts");
    const disabled = $("#metrics-disabled");
    const hint = $("#metrics-hint");
    if (!name) {
      if (hint) {
        hint.hidden = false;
        hint.textContent = "Select a sandbox to view metrics.";
      }
      if (charts) charts.hidden = true;
      if (disabled) disabled.hidden = true;
      return;
    }
    const vm =
      state.sandboxes.find((s) => s.name === name) ||
      (state._selectedVM && state._selectedVM.name === name ? state._selectedVM : null);
    // Cloud images without guest agent cannot provide stats — don't nag with 404/enable.
    if (vm && !imageHasAgent(vm)) {
      if (charts) charts.hidden = true;
      if (disabled) disabled.hidden = true;
      if (hint) {
        hint.hidden = false;
        hint.innerHTML = `<span class="muted">Metrics require a guest agent image
          (e.g. <code>grain-ubuntu</code> / <code>grain-ubuntu-fc</code>).
          <strong>${escapeHtml(vm.image || name)}</strong> has no guest agent, so Overview metrics are unavailable.</span>`;
      }
      return;
    }
    try {
      const h = await call("GetSandboxMetrics", name);
      if (hint) hint.hidden = true;
      if (!h.enabled) {
        if (charts) charts.hidden = true;
        if (disabled) disabled.hidden = false;
        const dis = $("#metrics-disabled");
        if (dis) {
          dis.innerHTML = `<p class="muted">Metrics are off for <strong>${escapeHtml(name)}</strong>.</p>
            <p class="hint">Samples are stored on the Grain <strong>host</strong>
            (<code>data_dir/vms/${escapeHtml(name)}/metrics.ring</code>) so remote Desktop still works.</p>
            <button type="button" class="btn btn-primary btn-sm" id="btn-enable-metrics">Enable metrics</button>`;
          $("#btn-enable-metrics")?.addEventListener("click", async () => {
            try {
              await act("enable metrics", () => call("EnableSandboxMetrics", name, true), {
                target: name,
              });
              toast(`Metrics enabled for ${name}`);
              await loadMetrics(name);
              await selectVM(name);
            } catch (_) {}
          });
        }
        return;
      }
      if (disabled) disabled.hidden = true;
      if (charts) charts.hidden = false;
      bindChartInteractions();
      const pts = (h.points || [])
        .map((p) => ({
          t_ms: p.t_ms || p.TimeMS || 0,
          load1: p.load1 ?? 0,
          mem_total_bytes: p.mem_total_bytes ?? 0,
          mem_available_bytes: p.mem_available_bytes ?? 0,
          disk_total_bytes: p.disk_total_bytes ?? 0,
          disk_free_bytes: p.disk_free_bytes ?? 0,
          net_rx_bytes: p.net_rx_bytes ?? 0,
          net_tx_bytes: p.net_tx_bytes ?? 0,
        }))
        .filter((p) => p.t_ms > 0)
        .sort((a, b) => a.t_ms - b.t_ms);
      metricsUI.allPoints = pts;
      redrawAllCharts();
      const meta = $("#metrics-meta");
      if (meta) {
        const anyNet = pts.some((p) => (p.net_rx_bytes || 0) + (p.net_tx_bytes || 0) > 0);
        meta.textContent = `${pts.length} samples in ring · interval ${h.interval || "—"} · host data_dir/vms/${name}/metrics.ring`;
        if (!anyNet && pts.length > 0) {
          meta.textContent +=
            " · network counters are zero — Install / update agent if traffic is expected";
        }
      }
    } catch (e) {
      const msg = String(e).replace(/^Error:\s*/i, "");
      if (charts) charts.hidden = true;
      if (disabled) disabled.hidden = true;
      if (hint) {
        hint.hidden = false;
        if (/404|not found/i.test(msg)) {
          // Only for agent images — cloud images already bailed above.
          if (vm && !imageHasAgent(vm)) {
            hint.innerHTML = `<span class="muted">Metrics require a guest agent image.</span>`;
          } else {
            hint.innerHTML = `<span class="hint bad">Metrics API not found (HTTP 404).</span>
              <span class="hint"> The Grain <strong>daemon</strong> on this host is older than the Desktop client.
              Rebuild and restart it: <code>just build && grain down && grain up</code>, then reopen Overview.
              Metrics are stored on that host, not in the Desktop app.</span>`;
          }
        } else {
          hint.textContent = msg;
        }
      }
    }
  }

  function startMetricsPoll(name) {
    stopMetricsPoll();
    if (!name) return;
    loadMetrics(name);
    state.metricsTimer = setInterval(() => {
      if (state.selected === name && state.activeTab === "overview") loadMetrics(name);
    }, 5000);
  }
  function stopMetricsPoll() {
    if (state.metricsTimer) {
      clearInterval(state.metricsTimer);
      state.metricsTimer = null;
    }
    hideTooltip();
  }

  function showInspector(show) {
    const insp = $("#inspector");
    if (!insp) return;
    if (state.currentView !== "sandboxes") {
      insp.hidden = true;
      return;
    }
    insp.hidden = false;
    const empty = $("#inspector-empty");
    const body = $("#inspector-body");
    const ws = $("#workspace");
    if (show && state.selected) {
      if (empty) empty.hidden = true;
      if (body) body.hidden = false;
      if (ws) ws.hidden = false;
    } else {
      if (empty) empty.hidden = false;
      if (body) body.hidden = true;
      if (ws) ws.hidden = true;
    }
  }

  async function selectVM(name) {
    if (!name) return;
    const switched = state.selected !== name;
    state.selected = name;
    renderTable();
    showInspector(true);
    $("#detail-name").textContent = name;
    try {
      const vm = await call("GetSandbox", name);
      const fromList = state.sandboxes.find((s) => s.name === name);
      if (fromList) {
        if (vm.agent_ok == null && fromList.agent_ok != null) vm.agent_ok = fromList.agent_ok;
        if (!vm.agent_version && fromList.agent_version) vm.agent_version = fromList.agent_version;
        if (!vm.agent_checked_at && fromList.agent_checked_at) vm.agent_checked_at = fromList.agent_checked_at;
        if (!vm.disk_gb && fromList.disk_gb) vm.disk_gb = fromList.disk_gb;
        if (vm.has_agent_image == null && fromList.has_agent_image != null)
          vm.has_agent_image = fromList.has_agent_image;
        if (!vm.network && fromList.network) vm.network = fromList.network;
        if (!vm.created_at && fromList.created_at) vm.created_at = fromList.created_at;
      }
      state._selectedVM = vm;
      updateActionButtons(vm.status);
      $("#detail-status-pill").innerHTML = statusBadge(vm.status);
      fillInspector(vm);
      if (switched && state.activeTab === "shell" && state.term) {
        try {
          state.term.reset();
        } catch (_) {}
      }
      if (state.activeTab === "overview") startMetricsPoll(name);
      if (state.activeTab === "shell") {
        stopMetricsPoll();
        const term = ensureTerm();
        if (term) await attachShell(name, term);
      } else if (state.activeTab === "logs") {
        stopMetricsPoll();
        await loadLogs();
      }
    } catch (e) {
      toast(String(e), true, { action: "get", target: name });
    }
  }

  function switchTab(name) {
    state.activeTab = name;
    $$(".ws-tab").forEach((t) => t.classList.toggle("active", t.dataset.tab === name));
    ["overview", "shell", "logs"].forEach((t) => {
      const p = $(`#tab-${t}`);
      if (p) p.hidden = t !== name;
    });
    if (name === "overview" && state.selected) {
      startMetricsPoll(state.selected);
    } else {
      stopMetricsPoll();
    }
    if (name === "shell" && state.selected) {
      const term = ensureTerm();
      requestAnimationFrame(() => {
        try {
          state.fit?.fit();
        } catch (_) {}
      });
      if (term) attachShell(state.selected, term);
    }
    if (name === "logs" && state.selected) loadLogs();
  }

  async function refreshList() {
    // Health is only for the active host — never paint another host's dial error here.
    try {
      const hs = await call("Health");
      setHealth(hs);
    } catch (e) {
      setHealth({ healthy: false, message: String(e).replace(/^Error:\s*/i, "") });
    }
    try {
      state.sandboxes = (await call("ListSandboxes")) || [];
      const names = new Set(state.sandboxes.map((s) => s.name));
      for (const n of [...state.selectedSet]) if (!names.has(n)) state.selectedSet.delete(n);
      if (state.sandboxes.length < 2) state.selectedSet.clear();
      renderTable();
      if (state.selected && names.has(state.selected)) {
        const vm = state.sandboxes.find((s) => s.name === state.selected);
        if (vm) {
          // Keep richer fields from last GetSandbox when list is thinner.
          if (state._selectedVM && state._selectedVM.name === vm.name) {
            const prev = state._selectedVM;
            if (vm.agent_ok == null && prev.agent_ok != null) vm.agent_ok = prev.agent_ok;
            if (!vm.agent_version && prev.agent_version) vm.agent_version = prev.agent_version;
            if (!vm.created_at && prev.created_at) vm.created_at = prev.created_at;
            if (!vm.network && prev.network) vm.network = prev.network;
            if (!vm.arch && prev.arch) vm.arch = prev.arch;
            if (!vm.gpu && prev.gpu) vm.gpu = prev.gpu;
            if (!vm.ip && prev.ip) vm.ip = prev.ip;
            if (!vm.agent_port && prev.agent_port) vm.agent_port = prev.agent_port;
          }
          state._selectedVM = { ...state._selectedVM, ...vm };
          updateActionButtons(vm.status);
          $("#detail-status-pill").innerHTML = statusBadge(vm.status);
          fillInspector(state._selectedVM);
        }
      } else if (state.selected && !names.has(state.selected)) {
        state.selected = null;
        showInspector(false);
      } else showInspector(!!state.selected);
    } catch (e) {
      // List failure is not health of another host — empty list + keep last health pill
      state.sandboxes = [];
      state.selectedSet.clear();
      renderTable();
      console.warn("ListSandboxes:", e);
    }
  }

  async function refreshAll() {
    await refreshList();
    await loadHostMenu();
  }

  /* ── images ── */
  async function loadImagesPage() {
    const tb = $("#images-tbody");
    if (!tb) return;
    try {
      const imgs = (await call("ListImages")) || [];
      tb.innerHTML =
        imgs
          .map((img) => {
            const st = img.ready
              ? '<span class="badge-agent ok">ready</span>'
              : '<span class="badge-agent no">missing</span>';
            const ver = img.ready ? escapeHtml(img.version || "installed") : "—";
            let btn = img.ready ? '<span class="muted">on disk</span>' : '<span class="muted">—</span>';
            if (img.pullable && !img.ready)
              btn = `<button type="button" class="btn btn-primary btn-sm" data-pull="${escapeHtml(img.id)}">Pull</button>`;
            return `<tr>
              <td><code class="selectable">${escapeHtml(img.id)}</code></td>
              <td>${st}</td>
              <td class="muted selectable">${ver}</td>
              <td>${img.has_agent ? "yes" : "—"}</td>
              <td class="muted selectable">${escapeHtml(img.description || "")}</td>
              <td>${btn}</td>
            </tr>`;
          })
          .join("") || '<tr><td colspan="6" class="empty-cell">No images</td></tr>';
      $$("[data-pull]").forEach((b) => b.addEventListener("click", () => pullImage(b)));
    } catch (e) {
      tb.innerHTML = `<tr><td colspan="6" class="empty-cell" style="color:var(--danger)">${escapeHtml(String(e))}</td></tr>`;
    }
  }

  function fmtBytes(n) {
    if (n > 1e9) return (n / 1e9).toFixed(2) + "GB";
    if (n > 1e6) return (n / 1e6).toFixed(0) + "MB";
    if (n > 1e3) return (n / 1e3).toFixed(0) + "KB";
    return n + "B";
  }

  async function pullImage(btn) {
    const id = btn.dataset.pull;
    btn.disabled = true;
    btn.textContent = "Pulling…";
    let lastToast = 0;
    const onProg = (p) => {
      if (p?.id && p.id !== id) return;
      const pct = p?.percent ?? 0;
      const w = p?.written || 0;
      const t = p?.total || 0;
      if (btn.isConnected) btn.textContent = t ? `Pulling (${pct}%)` : "Pulling…";
      const now = Date.now();
      if (now - lastToast > 900) {
        lastToast = now;
        toast(`Pulling ${id} — ${fmtBytes(w)}${t ? "/" + fmtBytes(t) : ""} (${pct}%)…`, false, {
          action: "pull progress",
          target: id,
          status: "running",
        });
      }
    };
    if (window.runtime?.EventsOn) window.runtime.EventsOn("pull:progress", onProg);
    try {
      await act("pull", () => call("PullImage", id), { target: id, summary: `pulled ${id}` });
      toast(`Pulled ${id}`);
      await loadImagesPage();
    } catch (_) {
      if (btn.isConnected) {
        btn.disabled = false;
        btn.textContent = "Pull";
      }
    } finally {
      try {
        window.runtime?.EventsOff?.("pull:progress");
      } catch (_) {}
    }
  }

  /* ── settings ── */
  function switchSettingsSeg(name) {
    state.settingsSeg = name;
    $$(".seg").forEach((s) => s.classList.toggle("active", s.dataset.seg === name));
    ["general", "connections", "mcp", "advanced"].forEach((s) => {
      const el = $(`#seg-${s}`);
      if (el) el.hidden = s !== name;
    });
    if (name === "mcp") loadMCP();
    if (name === "connections" || name === "general") loadSettings();
    if (name === "advanced" && !state.configEditing) loadSettings();
  }

  async function loadSettings() {
    try {
      const sum = await call("GetConfigSummary");
      const hint = $("#settings-path-hint");
      if (hint)
        hint.innerHTML = `Config: <span class="selectable mono">${escapeHtml(sum.path || "—")}</span> · dial <span class="selectable mono">${escapeHtml(sum.dial_hint || "—")}</span>`;
      const conns = sum.connections || [];
      const def =
        sum.desktop?.default_connection ||
        (conns[0] && conns[0].name) ||
        "local";
      renderDefaultConnMenu(conns, def);
      const startLocal = $("#set-start-local");
      if (startLocal) {
        const v = sum.desktop?.start_local_daemon;
        startLocal.checked = v === undefined || v === null ? true : !!v;
      }
      if ($("#set-data-dir")) $("#set-data-dir").value = sum.data_dir || "";
      if ($("#set-api")) $("#set-api").value = sum.api || "";
      if ($("#set-api-url")) $("#set-api-url").value = sum.api_url || "";
      const ts = $("#token-status");
      if (ts) ts.textContent = sum.has_token ? "Token: set" : "Token: not set";

      const cl = $("#connections-list");
      if (cl) {
        if (!conns.length) cl.innerHTML = '<p class="hint">No connections.</p>';
        else {
          const probes = state.hostProbes || [];
          cl.innerHTML = conns
            .map((c) => {
              const pr = probes.find((p) => p.name === c.name);
              const reach = pr
                ? pr.reachable
                  ? '<span class="act-success">reachable</span>'
                  : '<span class="act-error">unreachable</span>'
                : "";
              const detail = c.local ? "local" : escapeHtml(c.api || "remote");
              const err = pr && !pr.reachable && pr.error ? pr.error : "";
              const actions = c.local
                ? '<span class="muted">built-in</span>'
                : `<div class="btn-row">
                    <button type="button" class="btn btn-ghost btn-sm" data-edit-host="${escapeHtml(c.name)}">Edit</button>
                    <button type="button" class="btn btn-danger-ghost btn-sm" data-del-host="${escapeHtml(c.name)}">Remove</button>
                  </div>`;
              return `<div class="conn-row ${err ? "clickable" : ""}" data-conn="${escapeHtml(c.name)}">
                <div style="flex:1;min-width:0">
                  <div class="conn-name selectable">${escapeHtml(c.name)} ${reach}</div>
                  <div class="conn-meta selectable">${detail}</div>
                  ${err ? `<div class="conn-error" hidden data-conn-err="${escapeHtml(c.name)}">${escapeHtml(err)}</div>` : ""}
                </div>
                ${actions}
              </div>`;
            })
            .join("");
          $$(".conn-row.clickable").forEach((row) => {
            row.addEventListener("click", (e) => {
              if (e.target.closest("button")) return;
              const errEl = row.querySelector("[data-conn-err]");
              if (errEl) errEl.hidden = !errEl.hidden;
            });
          });
          $$("[data-del-host]").forEach((b) => {
            b.addEventListener("click", async (e) => {
              e.stopPropagation();
              if (!confirm(`Remove host “${b.dataset.delHost}”?`)) return;
              try {
                await act("delete host", () => call("DeleteHost", b.dataset.delHost), {
                  target: b.dataset.delHost,
                });
                toast(`Removed ${b.dataset.delHost}`);
                await loadHostMenu();
                await loadSettings();
              } catch (_) {}
            });
          });
          $$("[data-edit-host]").forEach((b) => {
            b.addEventListener("click", (e) => {
              e.stopPropagation();
              openHostModal(b.dataset.editHost, conns);
            });
          });
        }
      }

      if (!state.configEditing) {
        const raw = await call("GetConfigRaw");
        const ta = $("#config-raw");
        if (ta) ta.value = raw;
        updateLineNumbers(raw);
        highlightConfig(raw);
      }
    } catch (e) {
      toast(String(e), true, { action: "settings" });
    }
  }

  async function onSettingsFormSave(ev) {
    ev.preventDefault();
    const err = $("#settings-form-error");
    if (err) err.hidden = true;
    try {
      await act("settings save", () =>
        call("SaveSettingsForm", {
          default_connection: $("#set-default-conn")?.value || "local",
          start_local_daemon: !!$("#set-start-local")?.checked,
          data_dir: $("#set-data-dir")?.value || "",
          api: $("#set-api")?.value || "",
          api_url: $("#set-api-url")?.value || "",
        })
      );
      toast("Preferences saved");
      await refreshAll();
      await loadSettings();
    } catch (e) {
      if (err) {
        err.hidden = false;
        err.textContent = String(e).replace(/^Error:\s*/i, "");
      }
    }
  }

  /* ── MCP ── */
  async function loadMCP() {
    try {
      const st = await call("GetMCPStatus");
      $("#mcp-status-text").textContent = st.message || "—";
      const pill = $("#mcp-pill");
      pill.textContent = st.listening ? "listening" : st.enabled ? "enabled" : "disabled";
      pill.className = "status-pill " + (st.listening ? "ok" : st.enabled ? "warn" : "bad");
      $("#mcp-enabled").checked = !!st.enabled;
      $("#mcp-listen-input").value = st.listen || "127.0.0.1:7476";
      state.mcpSnippets = {
        cursor: st.cursor_snippet || "",
        claude: st.claude_snippet || "",
        generic: st.generic_snippet || "",
      };
      $("#mcp-ensure").disabled = !st.local;
      $("#mcp-hint").textContent = st.local
        ? "Local: Ensure running starts grain up --mcp."
        : "Remote host — cannot start MCP from Desktop.";
      renderMCPSnippet();
    } catch (e) {
      $("#mcp-status-text").textContent = String(e);
    }
  }

  function renderMCPSnippet() {
    const code = $("#mcp-snippet");
    const text = state.mcpSnippets[state.mcpAgent] || "";
    if (!code) return;
    if (window.hljs) {
      try {
        const lang = state.mcpAgent === "generic" ? "yaml" : "json";
        code.innerHTML = hljs.highlight(text, { language: lang }).value;
        return;
      } catch (_) {}
    }
    code.textContent = text;
  }

  /* ── doctor ── */
  async function runDoctor() {
    const root = $("#doctor-list");
    root.innerHTML = '<p class="muted">Running checks…</p>';
    $("#doctor-repair-out").hidden = true;
    try {
      const checks = (await act("doctor", () => call("RunDoctor"))) || [];
      if (!checks.length) {
        root.innerHTML = '<p class="muted">No checks returned.</p>';
        return;
      }
      root.innerHTML = checks
        .map((c) => {
          const icon = c.ok ? '<span class="ok">✓</span>' : '<span class="bad">✗</span>';
          const fix = c.fix ? `<div class="doctor-fix selectable">${escapeHtml(c.fix)}</div>` : "";
          const repairable =
            c.command &&
            (c.command === "grain up" || c.command === "grain up --mcp" || c.command.startsWith("grain up"));
          const cmd = c.command
            ? repairable
              ? `<button type="button" class="btn btn-ghost btn-sm" data-repair="${escapeHtml(c.command)}">Repair</button>`
              : `<button type="button" class="btn btn-ghost btn-sm" data-copy-cmd="${escapeHtml(c.command)}">Copy</button>`
            : "";
          return `<div class="doctor-row">
            ${icon}
            <strong>${escapeHtml(c.name)}</strong>
            <div><div class="selectable">${escapeHtml(c.message || "")}</div>${fix}</div>
            <div>${cmd}</div>
          </div>`;
        })
        .join("");
      $$("[data-repair]").forEach((b) => {
        b.addEventListener("click", async () => {
          const out = $("#doctor-repair-out");
          out.hidden = false;
          out.textContent = `Running ${b.dataset.repair}…\n`;
          try {
            const res = await act("doctor repair", () => call("DoctorRepair", b.dataset.repair), {
              summary: `repair ${b.dataset.repair}`,
            });
            out.textContent = (res.output || "ok") + "\n";
            toast(res.ok ? "Repair finished" : "Repair failed", !res.ok);
            await runDoctor();
          } catch (e) {
            out.textContent += String(e) + "\n";
          }
        });
      });
      $$("[data-copy-cmd]").forEach((b) => {
        b.addEventListener("click", async () => {
          try {
            await navigator.clipboard.writeText(b.dataset.copyCmd);
            toast("Copied command");
          } catch (_) {
            toast(b.dataset.copyCmd);
          }
        });
      });
    } catch (e) {
      root.innerHTML = `<p class="hint bad selectable">${escapeHtml(String(e))}</p>`;
    }
  }

  /* ── views ── */
  function switchView(name) {
    state.currentView = name;
    $$(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.view === name));
    $$(".view").forEach((v) => {
      v.hidden = v.id !== `view-${name}`;
    });
    const btnNew = $("#btn-new");
    if (btnNew) btnNew.hidden = name !== "sandboxes";
    showInspector(name === "sandboxes" && !!state.selected);
    if (name === "settings") {
      setConfigEditMode(false);
      switchSettingsSeg(state.settingsSeg || "general");
      loadSettings();
    }
    if (name === "images") loadImagesPage();
    if (name === "recipes") loadRecipesPage();
    if (name === "sandboxes") showInspector(!!state.selected);
  }

  /* ── recipes library ── */
  async function loadRecipesPage() {
    const tbody = $("#recipes-tbody");
    const empty = $("#recipes-empty");
    if (!tbody) return;
    try {
      const list = (await call("ListLibraryRecipes")) || [];
      state.recipes = list;
      tbody.innerHTML = "";
      if (!list.length) {
        if (empty) empty.hidden = false;
        $("#recipes-detail").hidden = true;
        return;
      }
      if (empty) empty.hidden = true;
      for (const r of list) {
        const tr = document.createElement("tr");
        tr.dataset.id = r.id;
        tr.innerHTML = `<td class="selectable">${escapeHtml(r.id)}</td>
          <td>${escapeHtml(r.image || "—")}</td>
          <td>${r.has_bootstrap ? "yes" : "no"}</td>
          <td class="selectable">${escapeHtml(r.description || r.name || "")}</td>
          <td class="no-select"><button type="button" class="btn btn-ghost btn-sm" data-recipe-open="${escapeHtml(r.id)}">Open</button></td>`;
        tr.addEventListener("click", (e) => {
          if (e.target.closest("button")) return;
          openRecipe(r.id);
        });
        tbody.appendChild(tr);
      }
      $$("[data-recipe-open]").forEach((b) =>
        b.addEventListener("click", (e) => {
          e.stopPropagation();
          openRecipe(b.dataset.recipeOpen);
        })
      );
      if (state.selectedRecipe) openRecipe(state.selectedRecipe);
    } catch (e) {
      toast(String(e), true);
    }
  }

  async function openRecipe(id) {
    state.selectedRecipe = id;
    const detail = $("#recipes-detail");
    if (!detail) return;
    detail.hidden = false;
    $("#recipe-detail-title").textContent = id;
    $("#recipe-yaml-err").hidden = true;
    try {
      const yaml = await call("GetLibraryRecipeYAML", id);
      $("#recipe-yaml").value = yaml || "";
      const meta = (state.recipes || []).find((r) => r.id === id);
      $("#recipe-detail-meta").textContent = meta
        ? `${meta.image || "—"} · ${meta.cpus || "?"} vCPU · ${meta.memory_mb || "?"} MiB${meta.has_bootstrap ? " · bootstrap" : ""}`
        : "";
    } catch (e) {
      toast(String(e), true);
    }
  }

  async function saveSelectedRecipe() {
    const id = state.selectedRecipe;
    if (!id) return;
    const yaml = $("#recipe-yaml")?.value || "";
    const errEl = $("#recipe-yaml-err");
    try {
      await act("save recipe", () => call("SaveLibraryRecipeYAML", id, yaml), { target: id });
      if (errEl) errEl.hidden = true;
      toast(`Saved ${id}`);
      await loadRecipesPage();
      openRecipe(id);
    } catch (e) {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = String(e);
      }
      toast(String(e), true);
    }
  }

  async function deleteSelectedRecipe() {
    const id = state.selectedRecipe;
    if (!id) return;
    if (!confirm(`Delete library recipe ${id}? (Sandboxes are not removed.)`)) return;
    try {
      await act("delete recipe", () => call("DeleteLibraryRecipe", id), { target: id });
      state.selectedRecipe = null;
      $("#recipes-detail").hidden = true;
      toast(`Deleted ${id}`);
      await loadRecipesPage();
    } catch (e) {
      toast(String(e), true);
    }
  }

  async function importRecipeFile() {
    try {
      const ent = await act("import recipe", () => call("PickAndImportRecipe"), {});
      toast(`Added ${ent?.id || "recipe"}`);
      state.selectedRecipe = ent?.id;
      await loadRecipesPage();
      if (ent?.id) openRecipe(ent.id);
    } catch (e) {
      if (String(e).includes("cancelled")) return;
      toast(String(e), true);
    }
  }

  function resetRecipeURLModal() {
    state.recipeURLPreview = null;
    const prev = $("#recipe-url-preview");
    if (prev) prev.hidden = true;
    const btn = $("#recipe-url-confirm-btn");
    if (btn) btn.disabled = true;
    const err = $("#recipe-url-preview-err");
    if (err) {
      err.hidden = true;
      err.textContent = "";
    }
    const kv = $("#recipe-url-preview-kv");
    if (kv) kv.innerHTML = "";
    const warn = $("#recipe-url-preview-warn");
    if (warn) warn.innerHTML = "";
  }

  function renderRecipeURLPreview(p) {
    const panel = $("#recipe-url-preview");
    const kv = $("#recipe-url-preview-kv");
    const warn = $("#recipe-url-preview-warn");
    const btn = $("#recipe-url-confirm-btn");
    const err = $("#recipe-url-preview-err");
    if (!panel || !kv) return;
    panel.hidden = false;
    if (err) err.hidden = true;
    const rows = [
      ["ID", p.suggested_id || "—"],
      ["Name", p.name || "—"],
      ["Description", p.description || "—"],
      ["Image", p.image || "—"],
      ["Resources", `${p.cpus || "—"} vCPU / ${p.memory_mb || "—"} MiB` + (p.disk_gb ? ` / ${p.disk_gb} GiB` : "")],
      ["Persistent", p.persistent ? "yes" : "no"],
      ["Bootstrap", p.has_bootstrap ? (p.bootstrap_steps || []).join(", ") || "yes" : "no"],
      ["Mounts", (p.mounts || []).length ? (p.mounts || []).join("; ") : "—"],
      ["Forwards", (p.forwards || []).length ? (p.forwards || []).join("; ") : "—"],
    ];
    setKV(kv, rows);
    if (warn) {
      warn.innerHTML = (p.warnings || []).map((w) => `<li>${escapeHtml(w)}</li>`).join("");
    }
    if (btn) btn.disabled = !p.yaml;
    state.recipeURLPreview = p;
  }

  async function previewRecipeURL(url) {
    resetRecipeURLModal();
    const p = await act("preview recipe URL", () => call("PreviewRecipeURL", url), { target: url });
    renderRecipeURLPreview(p);
    return p;
  }

  async function confirmRecipeURLPreview() {
    const p = state.recipeURLPreview;
    if (!p?.yaml) throw new Error("Preview the URL first");
    const ent = await act(
      "add recipe to library",
      () => call("ConfirmRecipeYAML", p.yaml, p.suggested_id || "", false),
      { target: p.suggested_id }
    );
    toast(`Added ${ent?.id || p.suggested_id || "recipe"}`);
    state.selectedRecipe = ent?.id;
    state.recipeURLPreview = null;
    closeModal("modal-recipe-url");
    $("#recipe-url-input").value = "";
    resetRecipeURLModal();
    await loadRecipesPage();
    if (ent?.id) openRecipe(ent.id);
  }

  async function loadOfficialCatalog() {
    const panel = $("#recipes-catalog");
    const tbody = $("#catalog-tbody");
    const err = $("#catalog-err");
    if (!panel || !tbody) return;
    panel.hidden = false;
    if (err) err.hidden = true;
    tbody.innerHTML = `<tr><td colspan="4" class="muted">Loading catalog…</td></tr>`;
    try {
      const list = (await call("SearchOfficialRecipes")) || [];
      tbody.innerHTML = "";
      if (!list.length) {
        tbody.innerHTML = `<tr><td colspan="4" class="muted">Catalog empty or unavailable.</td></tr>`;
        return;
      }
      for (const r of list) {
        const tr = document.createElement("tr");
        tr.innerHTML = `<td class="selectable">${escapeHtml(r.id)}</td>
          <td>${escapeHtml(r.title || r.description || "")}</td>
          <td>${r.in_library ? "yes" : "no"}</td>
          <td class="no-select"><button type="button" class="btn btn-ghost btn-sm" data-catalog-add="${escapeHtml(r.id)}" ${r.in_library ? "disabled" : ""}>${r.in_library ? "Installed" : "Add"}</button></td>`;
        tbody.appendChild(tr);
      }
      $$("[data-catalog-add]").forEach((b) =>
        b.addEventListener("click", async () => {
          try {
            const ent = await act("add official recipe", () => call("AddOfficialRecipe", b.dataset.catalogAdd, false), {
              target: b.dataset.catalogAdd,
            });
            toast(`Added ${ent?.id || b.dataset.catalogAdd}`);
            await loadRecipesPage();
            await loadOfficialCatalog();
            if (ent?.id) openRecipe(ent.id);
          } catch (e) {
            toast(String(e), true);
          }
        })
      );
    } catch (e) {
      tbody.innerHTML = "";
      if (err) {
        err.hidden = false;
        err.textContent = String(e);
      }
    }
  }

  function openDeployRecipeModal() {
    const id = state.selectedRecipe;
    if (!id) return;
    const meta = (state.recipes || []).find((r) => r.id === id);
    $("#recipe-deploy-id").textContent = `Recipe: ${id}`;
    $("#recipe-deploy-name").value = meta?.name || id;
    const warn = $("#recipe-deploy-warn");
    if (meta?.has_bootstrap && warn) {
      warn.hidden = false;
      warn.textContent = "This recipe runs bootstrap scripts inside the guest until ready.";
    } else if (warn) {
      warn.hidden = true;
    }
    openModal("modal-recipe-deploy");
  }

  async function deploySelectedRecipe(name, waitReady) {
    const id = state.selectedRecipe;
    if (!id) return;
    const opts = { recipe: id, name: name || id };
    if (waitReady) {
      opts.wait = ""; // recipe default / wait until ready
    } else {
      opts.wait = "ssh";
    }
    const sb = await act("deploy recipe", () => call("DeployRecipe", opts), {
      target: name || id,
      summary: `created sandbox from recipe ${id}`,
    });
    toast(`Created ${sb?.name || name}`);
    closeModal("modal-recipe-deploy");
    switchView("sandboxes");
    await refreshList();
    if (sb?.name) await selectVM(sb.name);
  }

  /* ── shell ── */
  function ensureTerm(host = "#xterm", fitKey = "fit", termKey = "term") {
    if (state[termKey]) return state[termKey];
    const el = $(host);
    if (!el || !window.Terminal) return null;
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: "IBM Plex Mono, monospace",
      fontSize: 13,
      theme: { background: "#0a0d0b", foreground: "#eceae4", cursor: "#3ddea8" },
    });
    term.open(el);
    if (window.FitAddon) {
      try {
        const FitAddon = window.FitAddon.FitAddon || window.FitAddon;
        const fit = new FitAddon();
        term.loadAddon(fit);
        fit.fit();
        state[fitKey] = fit;
      } catch (_) {}
    }
    term.onData((data) => {
      if (typeof go?.ShellWrite === "function") go.ShellWrite(data).catch(() => {});
    });
    state[termKey] = term;
    return term;
  }

  function bindShellEventsOnce() {
    if (state.shellEventsBound || !window.runtime?.EventsOn) return;
    state.shellEventsBound = true;
    window.runtime.EventsOn("shell:data", (payload) => {
      const data = typeof payload === "string" ? payload : payload?.data;
      if (data == null) return;
      if (state.term) state.term.write(String(data));
      if (state.termOnly) state.termOnly.write(String(data));
    });
    window.runtime.EventsOn("shell:close", (payload) => {
      const msg = typeof payload === "string" ? payload : payload?.error || "closed";
      const normal =
        /StatusNormalClosure|shell exited|logout|context canceled|close frame/i.test(String(msg));
      // Pop-out shell window: close the window on normal exit
      if (state.termOnly && normal && window.runtime?.Quit) {
        try {
          window.runtime.Quit();
        } catch (_) {}
        return;
      }
      if (state.termOnly) {
        const line = normal
          ? `\r\n\x1b[90m// session ended\x1b[0m\r\n`
          : `\r\n\x1b[90m// session closed ${msg}\x1b[0m\r\n`;
        state.termOnly.write(line);
        return;
      }
      // In-app shell: normal exit → Overview; errors keep a short note then Overview
      if (state.term) {
        if (!normal) {
          state.term.write(`\r\n\x1b[90m// session closed ${msg}\x1b[0m\r\n`);
        }
        switchTab("overview");
      }
    });
  }

  async function attachShell(vm, term) {
    if (!vm || !term) return;
    bindShellEventsOnce();
    try {
      await call("ShellClose");
    } catch (_) {}
    term.reset();
    try {
      await call("ShellAttach", vm, term.cols || 80, term.rows || 24);
      term.writeln(`\x1b[32mconnected\x1b[0m \x1b[90m${vm}\x1b[0m`);
    } catch (e) {
      term.writeln(`\x1b[31m${String(e)}\x1b[0m`);
    }
  }

  async function openShellWindow() {
    if (!state.selected) return;
    try {
      await act("shell window", () => call("OpenShellWindow", state.selected), {
        target: state.selected,
      });
      toast(`Shell window: ${state.selected}`);
    } catch (_) {}
  }

  const logFind = {
    text: "",
    query: "",
    matches: [], // character offsets into plain text — we use element indices of marks
    idx: -1,
  };

  function setLogText(text) {
    logFind.text = text == null ? "" : String(text);
    renderLogView();
  }

  function renderLogView() {
    const view = $("#log-view");
    if (!view) return;
    const text = logFind.text;
    const q = (logFind.query || "").trim();
    logFind.matches = [];
    logFind.idx = -1;

    if (!q) {
      view.textContent = text;
      updateLogFindCount();
      return;
    }

    // Case-insensitive highlight; escape HTML carefully.
    const lower = text.toLowerCase();
    const needle = q.toLowerCase();
    let html = "";
    let i = 0;
    let matchN = 0;
    while (i < text.length) {
      const j = lower.indexOf(needle, i);
      if (j < 0) {
        html += escapeHtml(text.slice(i));
        break;
      }
      html += escapeHtml(text.slice(i, j));
      html += `<mark class="log-hit" data-hit="${matchN}">${escapeHtml(text.slice(j, j + needle.length))}</mark>`;
      logFind.matches.push(matchN);
      matchN++;
      i = j + needle.length;
    }
    view.innerHTML = html || "";
    if (logFind.matches.length) {
      logFind.idx = 0;
      focusLogMatch(0, false);
    }
    updateLogFindCount();
  }

  function updateLogFindCount() {
    const el = $("#log-find-count");
    if (!el) return;
    const n = logFind.matches.length;
    const q = (logFind.query || "").trim();
    if (!q) {
      el.textContent = "";
      return;
    }
    if (!n) {
      el.textContent = "0/0";
      return;
    }
    el.textContent = `${logFind.idx + 1}/${n}`;
  }

  function focusLogMatch(index, scroll) {
    const view = $("#log-view");
    if (!view) return;
    const marks = $$("mark.log-hit", view);
    marks.forEach((m) => m.classList.remove("current"));
    if (!marks.length) return;
    const i = ((index % marks.length) + marks.length) % marks.length;
    logFind.idx = i;
    const m = marks[i];
    m.classList.add("current");
    if (scroll !== false) {
      m.scrollIntoView({ block: "center", behavior: "smooth" });
    }
    updateLogFindCount();
  }

  function logFindNext(dir) {
    if (!logFind.matches.length) return;
    focusLogMatch(logFind.idx + (dir < 0 ? -1 : 1), true);
  }

  function openLogFind(prefill) {
    const bar = $("#log-find-bar");
    const input = $("#log-find");
    if (!bar || !input) return;
    bar.hidden = false;
    if (prefill != null && prefill !== "") input.value = prefill;
    input.focus();
    input.select();
    logFind.query = input.value;
    renderLogView();
  }

  function closeLogFind() {
    const bar = $("#log-find-bar");
    const input = $("#log-find");
    if (bar) bar.hidden = true;
    if (input) input.value = "";
    logFind.query = "";
    renderLogView();
    $("#log-view")?.focus();
  }

  async function copyLogSelection() {
    const sel = window.getSelection();
    let text = sel && String(sel) ? String(sel) : "";
    // Prefer selection when it intersects the log view
    const view = $("#log-view");
    if (text && view && sel && sel.rangeCount) {
      try {
        const range = sel.getRangeAt(0);
        if (!view.contains(range.commonAncestorContainer)) text = "";
      } catch (_) {
        text = "";
      }
    }
    if (!text) text = logFind.text || view?.textContent || "";
    if (!text) {
      toast("Nothing to copy", true);
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      toast(text === logFind.text || text === (view?.textContent || "") ? "Logs copied" : "Copied selection");
    } catch (e) {
      // Fallback for restricted clipboard
      try {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.left = "-9999px";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        ta.remove();
        toast("Copied");
      } catch (_) {
        toast("Copy failed", true);
      }
    }
  }

  async function loadLogs() {
    if (!state.selected) return;
    const view = $("#log-view");
    if (!view) return;
    setLogText("Loading…");
    try {
      const res = await call("ReadLogs", state.selected, $("#log-source")?.value || "serial");
      const body = res.missing
        ? `No log at ${res.path || "?"}`
        : (res.truncated ? "…\n" : "") + (res.content || "");
      setLogText(body);
      // Keep find bar query if open
      if (!$("#log-find-bar")?.hidden) {
        logFind.query = $("#log-find")?.value || "";
        renderLogView();
      }
    } catch (e) {
      setLogText(String(e));
    }
  }

  /* ── config editor ── */
  function updateLineNumbers(text) {
    const n = Math.max(String(text || "").split("\n").length, 1);
    let s = "";
    for (let i = 1; i <= n; i++) s += i + "\n";
    const el = $("#config-lines");
    if (el) el.textContent = s;
  }
  function highlightConfig(text) {
    const code = $("#config-hl code");
    if (!code) return;
    if (window.hljs) {
      try {
        code.innerHTML = hljs.highlight(text || "", { language: "yaml" }).value;
        return;
      } catch (_) {}
    }
    code.textContent = text || "";
  }
  function setConfigEditMode(on) {
    state.configEditing = on;
    const ta = $("#config-raw");
    const btn = $("#btn-config-edit");
    if (!ta || !btn) return;
    ta.readOnly = !on;
    btn.textContent = on ? "Save" : "Edit";
    if (!on) {
      const err = $("#config-error");
      if (err) err.hidden = true;
    }
  }
  async function onConfigEditSave() {
    if (!state.configEditing) {
      setConfigEditMode(true);
      $("#config-raw")?.focus();
      return;
    }
    const content = $("#config-raw").value;
    const err = $("#config-error");
    if (err) err.hidden = true;
    try {
      const res = await act("config save", () => call("SaveConfig", content));
      toast(res.message || "Saved");
      setConfigEditMode(false);
      await refreshAll();
      await loadSettings();
    } catch (e) {
      if (err) {
        err.hidden = false;
        err.textContent = String(e).replace(/^Error:\s*/i, "");
      }
    }
  }

  /* ── create ── */
  async function fillImageSelect() {
    const sel = $("#create-image");
    if (!sel) return;
    let ready = [],
      all = [];
    try {
      ready = (await call("ReadyImageIDs")) || [];
      all = (await call("ListImages")) || [];
    } catch (_) {}
    const seen = new Set(ready);
    let opts = ready.map((id) => `<option value="${escapeHtml(id)}">${escapeHtml(id)} (ready)</option>`).join("");
    for (const img of all) {
      if (seen.has(img.id)) continue;
      if (img.pullable)
        opts += `<option value="${escapeHtml(img.id)}">${escapeHtml(img.id)} (not pulled)</option>`;
    }
    opts += `<option value="__custom__">Custom…</option>`;
    sel.innerHTML = opts || `<option value="grain-ubuntu">grain-ubuntu</option>`;
    const custom = $("#create-image-custom");
    sel.onchange = () => {
      custom.hidden = sel.value !== "__custom__";
    };
  }

  async function openCreate() {
    await fillImageSelect();
    try {
      const d = await call("ConfigDefaults");
      const f = $("#create-form");
      if (d.cpus) f.cpus.value = d.cpus;
      if (d.memory_mb) f.memory_mb.value = d.memory_mb;
      if (d.disk_gb) f.disk_gb.value = d.disk_gb;
    } catch (_) {}
    $("#create-status").textContent = "";
    $("#create-submit").disabled = false;
    openModal("modal-create");
  }

  async function onCreate(ev) {
    ev.preventDefault();
    const fd = new FormData(ev.target);
    let image = fd.get("image") || "";
    if (image === "__custom__") image = (fd.get("image_custom") || "").toString().trim();
    const opts = {
      name: (fd.get("name") || "").toString().trim(),
      image,
      cpus: Number(fd.get("cpus") || 0),
      memory_mb: Number(fd.get("memory_mb") || 0),
      disk_gb: Number(fd.get("disk_gb") || 0),
      persistent: !!fd.get("persistent"),
      metrics_enabled: !!fd.get("metrics_enabled"),
      wait: fd.get("wait") || "auto",
      timeout: fd.get("timeout") || "",
      arch: fd.get("arch") || "",
      gpu: fd.get("gpu") || "",
      network: fd.get("network") || "",
      publish: fd.get("publish") || "",
      mounts: fd.get("mounts") || "",
      userdata: fd.get("userdata") || "",
    };
    try {
      if (opts.name) await call("ValidateName", opts.name);
    } catch (e) {
      const msg = String(e).replace(/^Error:\s*/i, "");
      $("#create-status").textContent = msg;
      $("#name-hint").textContent = msg;
      $("#name-hint").classList.add("bad");
      return;
    }
    // Close modal immediately — long create must not jail the UI
    closeModal("modal-create");
    const displayName = opts.name || "sandbox";
    toast(`Creating “${displayName}”…`, false, {
      action: "create",
      target: displayName,
      status: "running",
      summary: `Creating sandbox ${displayName}…`,
    });
    const running = pushEvent({
      action: "create",
      target: displayName,
      status: "running",
      summary: `Creating sandbox ${displayName} from Grain Desktop…`,
    });
    const t0 = performance.now();
    try {
      const sb = await call("CreateSandbox", opts);
      const ms = Math.round(performance.now() - t0);
      updateEvent(running.id, {
        status: "success",
        target: sb.name,
        duration_ms: ms,
        summary: `${sb.name} sandbox created from Grain Desktop in ${ms}ms`,
        ok: true,
      });
      toast(`Created ${sb.name} (${ms}ms)`, false, {
        action: "create",
        target: sb.name,
        duration_ms: ms,
      });
      switchView("sandboxes");
      await refreshList();
      await selectVM(sb.name);
      switchTab("overview");
    } catch (e) {
      const ms = Math.round(performance.now() - t0);
      const msg = String(e).replace(/^Error:\s*/i, "");
      updateEvent(running.id, {
        status: "error",
        duration_ms: ms,
        summary: msg,
        detail: msg,
        ok: false,
      });
      toast(msg, true, { action: "create", target: displayName });
    }
  }

  async function onNameHint() {
    const hint = $("#name-hint");
    const name = $("#create-form")?.name?.value?.trim() || "";
    if (!hint) return;
    if (!name) {
      hint.textContent = "Lowercase letters, digits, hyphens; start with a letter. Empty = auto.";
      hint.classList.remove("bad");
      return;
    }
    try {
      await call("ValidateName", name);
      hint.textContent = "Name looks valid.";
      hint.classList.remove("bad");
    } catch (e) {
      hint.textContent = String(e).replace(/^Error:\s*/i, "");
      hint.classList.add("bad");
    }
  }

  /* ── host modal ── */
  function openHostModal(editName, conns) {
    state.hostTestedOK = false;
    $("#host-save").disabled = true;
    $("#host-test-result").hidden = true;
    $("#host-edit-original").value = editName || "";
    $("#host-modal-title").textContent = editName ? "Edit host" : "Add host";
    const c = (conns || []).find((x) => x.name === editName);
    $("#host-name").value = c?.name || "";
    $("#host-name").readOnly = !!editName;
    $("#host-api").value = c?.api || "";
    $("#host-token").value = "";
    $("#host-mcp").checked = !!(c?.notes && String(c.notes).startsWith("mcp:"));
    $("#host-mcp-fields").hidden = !$("#host-mcp").checked;
    if ($("#host-mcp").checked && c?.notes) {
      $("#host-mcp-listen").value = String(c.notes).replace(/^mcp:/, "");
    } else $("#host-mcp-listen").value = "";
    openModal("modal-host");
  }

  /* ── sandbox config ── */
  async function openSandboxEdit(name) {
    try {
      const meta = await call("GetSandboxMeta", name);
      const f = $("#sbox-form");
      $("#sbox-edit-name").textContent = name;
      f.cpus.value = meta.cpus || "";
      f.memory_mb.value = meta.memory_mb || "";
      f.disk_gb.value = meta.disk_gb || "";
      f.image.value = meta.image || "";
      f.persistent.checked = !!meta.persistent;
      openModal("modal-sbox");
    } catch (e) {
      toast(String(e), true, { action: "sandbox config" });
    }
  }

  async function onSboxSave(ev) {
    ev.preventDefault();
    const name = $("#sbox-edit-name").textContent;
    const fd = new FormData(ev.target);
    try {
      const res = await act(
        "save meta",
        () =>
          call("SaveSandboxMeta", name, {
            cpus: Number(fd.get("cpus") || 0),
            memory_mb: Number(fd.get("memory_mb") || 0),
            disk_gb: Number(fd.get("disk_gb") || 0),
            image: fd.get("image") || "",
            persistent: !!fd.get("persistent"),
          }),
        { target: name }
      );
      closeModal("modal-sbox");
      if (res.disk_resized) toast(res.message || "Disk resized");
      if (res.needs_restart) {
        const yes = await confirmDialog(`Saved config for ${name}. Restart now?`);
        if (yes) {
          try {
            await act(
              "restart",
              async () => {
                await call("StopSandbox", name);
                await call("StartSandbox", name);
              },
              { target: name, summary: `${name} sandbox restarted from Grain Desktop` }
            );
            toast(`Restarted ${name}`);
          } catch (_) {}
        }
      } else if (!res.disk_resized) toast(res.message || "Saved");
      await refreshList();
      if (state.selected === name) await selectVM(name);
    } catch (_) {}
  }

  async function bulk(kind) {
    const names = [...state.selectedSet];
    if (names.length < 2) return;
    if (kind === "rm" && !confirm(`Remove ${names.length} sandboxes?`)) return;
    for (const name of names) {
      try {
        if (kind === "start") await act("start", () => call("StartSandbox", name), { target: name });
        if (kind === "stop") await act("stop", () => call("StopSandbox", name), { target: name });
        if (kind === "rm") await act("remove", () => call("RemoveSandbox", name), { target: name });
      } catch (_) {}
    }
    state.selectedSet.clear();
    await refreshList();
  }

  /* ── wire ── */
  function openDocs(e) {
    if (e) e.preventDefault();
    // Prefer bound method (always works in Wails), then runtime helper, then window.open
    if (typeof go?.OpenDocs === "function") {
      go.OpenDocs().catch(() => {});
      return;
    }
    try {
      if (window.runtime?.BrowserOpenURL) {
        window.runtime.BrowserOpenURL("https://grainvm.com");
        return;
      }
    } catch (_) {}
    window.open("https://grainvm.com", "_blank", "noopener,noreferrer");
  }

  function wire() {
    $("#btn-theme")?.addEventListener("click", toggleTheme);
    $("#btn-new")?.addEventListener("click", openCreate);
    // Wails webview: plain <a target=_blank> often no-ops — use BrowserOpenURL
    $(".docs-link")?.addEventListener("click", openDocs);
    $("#btn-doctor")?.addEventListener("click", () => {
      openModal("modal-doctor");
      runDoctor();
    });
    $("#btn-doctor-run")?.addEventListener("click", runDoctor);
    $("#btn-activity")?.addEventListener("click", () => openActivity());
    $("#btn-activity-close")?.addEventListener("click", closeActivity);
    $("#btn-activity-clear")?.addEventListener("click", () => {
      localStorage.removeItem(EVENTS_KEY);
      renderActivity();
      updateActivityBadge();
    });
    $("#activity-drawer")?.addEventListener("click", (e) => {
      if (e.target.id === "activity-drawer") closeActivity();
    });
    $("#toast")?.addEventListener("click", () => openActivity($("#toast").dataset.eventId));

    $("#host-btn")?.addEventListener("click", (e) => {
      e.stopPropagation();
      toggleHostMenu();
    });
    $("#set-default-conn-btn")?.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      toggleDefaultConnMenu();
    });
    document.addEventListener("click", (e) => {
      if (!$("#host-menu")?.contains(e.target)) closeHostMenu();
      if (!$("#set-default-conn-menu")?.contains(e.target)) closeDefaultConnMenu();
    });
    $("#host-add")?.addEventListener("click", () => {
      closeHostMenu();
      openHostModal(null, []);
    });

    $$(".nav-item").forEach((b) => b.addEventListener("click", () => switchView(b.dataset.view)));
    $$(".ws-tab").forEach((t) => t.addEventListener("click", () => switchTab(t.dataset.tab)));

    $("#btn-recipe-import-file")?.addEventListener("click", () => importRecipeFile());
    $("#btn-recipe-import-url")?.addEventListener("click", () => {
      resetRecipeURLModal();
      openModal("modal-recipe-url");
    });
    $("#btn-recipe-browse")?.addEventListener("click", () => loadOfficialCatalog());
    $("#btn-recipe-refresh")?.addEventListener("click", () => loadRecipesPage());
    $("#btn-recipe-catalog-close")?.addEventListener("click", () => {
      const p = $("#recipes-catalog");
      if (p) p.hidden = true;
    });
    $("#btn-recipe-save")?.addEventListener("click", () => saveSelectedRecipe());
    $("#btn-recipe-delete")?.addEventListener("click", () => deleteSelectedRecipe());
    $("#btn-recipe-deploy")?.addEventListener("click", () => openDeployRecipeModal());
    $("#recipe-url-form")?.addEventListener("submit", async (e) => {
      e.preventDefault();
      const url = $("#recipe-url-input")?.value?.trim();
      if (!url) return;
      try {
        await previewRecipeURL(url);
      } catch (err) {
        const errEl = $("#recipe-url-preview-err");
        const panel = $("#recipe-url-preview");
        if (panel) panel.hidden = false;
        if (errEl) {
          errEl.hidden = false;
          errEl.textContent = String(err);
        }
        toast(String(err), true);
      }
    });
    $("#recipe-url-confirm-btn")?.addEventListener("click", async (e) => {
      e.preventDefault();
      try {
        await confirmRecipeURLPreview();
      } catch (err) {
        toast(String(err), true);
      }
    });
    $("#recipe-deploy-form")?.addEventListener("submit", async (e) => {
      e.preventDefault();
      const name = $("#recipe-deploy-name")?.value?.trim();
      const waitReady = $("#recipe-deploy-wait")?.checked !== false;
      try {
        await deploySelectedRecipe(name, waitReady);
      } catch (err) {
        toast(String(err), true);
      }
    });
    $$(".seg").forEach((s) => s.addEventListener("click", () => switchSettingsSeg(s.dataset.seg)));
    $$(".agent-tab").forEach((t) =>
      t.addEventListener("click", () => {
        state.mcpAgent = t.dataset.agent;
        $$(".agent-tab").forEach((x) => x.classList.toggle("active", x.dataset.agent === state.mcpAgent));
        $("#mcp-agent-label").textContent =
          state.mcpAgent === "cursor" ? "Cursor" : state.mcpAgent === "claude" ? "Claude" : "Generic";
        renderMCPSnippet();
      })
    );

    $$("[data-close]").forEach((b) => b.addEventListener("click", () => closeModal(b.dataset.close)));
    $$(".modal-backdrop").forEach((bd) => {
      bd.addEventListener("click", (e) => {
        if (e.target === bd) bd.hidden = true;
      });
    });
    $("#confirm-yes")?.addEventListener("click", () => {
      closeModal("modal-confirm");
      state.confirm?.(true);
      state.confirm = null;
    });
    $("#confirm-later")?.addEventListener("click", () => {
      closeModal("modal-confirm");
      state.confirm?.(false);
      state.confirm = null;
    });

    const moreMenu = $("#more-menu");
    $("#btn-more")?.addEventListener("click", (e) => {
      e.stopPropagation();
      moreMenu?.classList.toggle("show");
    });
    document.addEventListener("click", () => moreMenu?.classList.remove("show"));

    $("#detail-actions")?.addEventListener("click", async (e) => {
      const btn = e.target.closest("[data-act]");
      if (!btn) return;
      e.stopPropagation();
      moreMenu?.classList.remove("show");
      const actName = btn.dataset.act;
      const name = state.selected;
      if (!name) return;
      try {
        if (actName === "shell") {
          switchTab("shell");
          return;
        }
        if (actName === "window") return openShellWindow();
        if (actName === "term") {
          await act("terminal", () => call("OpenSystemTerminal", name), { target: name });
          toast("Opened Terminal");
          return;
        }
        if (actName === "logs") return switchTab("logs");
        if (actName === "edit") return openSandboxEdit(name);
        if (actName === "export-recipe") {
          const res = await act("export recipe", () => call("ExportSandboxRecipe", name), {
            target: name,
            summary: `exported recipe for ${name}`,
          });
          if (res?.cancelled) {
            toast("Export cancelled");
            return;
          }
          if (res?.path) {
            toast(`Saved recipe: ${res.path}`);
          } else if (res?.yaml) {
            toast("Recipe generated (no save path)");
          } else {
            toast(`Exported recipe for ${name}`);
          }
          return;
        }
        if (actName === "start") {
          await act("start", () => call("StartSandbox", name), {
            target: name,
            summary: `${name} sandbox started from Grain Desktop`,
          });
          toast(`Started ${name}`);
          await refreshList();
          await selectVM(name);
          return;
        }
        if (actName === "stop") {
          await act("stop", () => call("StopSandbox", name), {
            target: name,
            summary: `${name} sandbox stopped from Grain Desktop`,
          });
          toast(`Stopped ${name}`);
          await refreshList();
          await selectVM(name);
          return;
        }
        if (actName === "deploy-agent") {
          const res = await act("deploy agent", () => call("DeployAgent", name), {
            target: name,
            summary: `deployed guest agent on ${name}`,
          });
          toast(res?.message || `Agent deployed on ${name}`);
          await refreshList();
          await selectVM(name);
          return;
        }
        if (actName === "rm") {
          if (!confirm(`Remove ${name}?`)) return;
          await act("remove", () => call("RemoveSandbox", name), { target: name });
          state.selected = null;
          state._selectedVM = null;
          showInspector(false);
          await refreshList();
        }
      } catch (_) {}
    });

    $("#btn-shell-pop")?.addEventListener("click", openShellWindow);
    $("#btn-shell-reconnect")?.addEventListener("click", async () => {
      if (state.selected && state.term) await attachShell(state.selected, state.term);
    });
    $("#bulk-start")?.addEventListener("click", () => bulk("start"));
    $("#bulk-stop")?.addEventListener("click", () => bulk("stop"));
    $("#bulk-rm")?.addEventListener("click", () => bulk("rm"));
    $("#check-all")?.addEventListener("change", (e) => {
      state.selectedSet = new Set();
      if (e.target.checked) state.sandboxes.forEach((s) => state.selectedSet.add(s.name));
      renderTable();
    });

    $("#btn-reload-logs")?.addEventListener("click", loadLogs);
    $("#log-source")?.addEventListener("change", loadLogs);
    $("#btn-log-copy")?.addEventListener("click", () => copyLogSelection());
    $("#log-find")?.addEventListener("input", (e) => {
      logFind.query = e.target.value || "";
      renderLogView();
    });
    $("#log-find")?.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeLogFind();
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        logFindNext(e.shiftKey ? -1 : 1);
      }
    });
    $("#log-find-next")?.addEventListener("click", () => logFindNext(1));
    $("#log-find-prev")?.addEventListener("click", () => logFindNext(-1));
    $("#log-find-close")?.addEventListener("click", closeLogFind);

    // Cmd/Ctrl+F find in logs; Cmd/Ctrl+C copy selection from log view (backup to Edit menu)
    document.addEventListener("keydown", (e) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod) return;
      const key = (e.key || "").toLowerCase();
      if (key === "f" && state.activeTab === "logs" && state.selected) {
        e.preventDefault();
        const sel = window.getSelection()?.toString();
        openLogFind(sel && sel.length < 120 ? sel : undefined);
        return;
      }
      if (key === "g" && state.activeTab === "logs" && !$("#log-find-bar")?.hidden) {
        e.preventDefault();
        logFindNext(e.shiftKey ? -1 : 1);
        return;
      }
      if (key === "c") {
        const view = $("#log-view");
        const sel = window.getSelection();
        if (!view || !sel || !sel.rangeCount) return;
        try {
          if (!view.contains(sel.getRangeAt(0).commonAncestorContainer)) return;
        } catch (_) {
          return;
        }
        if (!String(sel)) return;
        e.preventDefault();
        copyLogSelection();
      }
    });
    $("#settings-form")?.addEventListener("submit", onSettingsFormSave);
    $("#btn-add-host")?.addEventListener("click", () => openHostModal(null, []));
    $("#host-mcp")?.addEventListener("change", (e) => {
      $("#host-mcp-fields").hidden = !e.target.checked;
    });
    $("#host-test")?.addEventListener("click", async () => {
      const api = $("#host-api").value;
      const token = $("#host-token").value;
      const resEl = $("#host-test-result");
      resEl.hidden = false;
      resEl.textContent = "Testing…";
      resEl.classList.remove("bad");
      try {
        const p = await call("TestHostConnection", api, token);
        if (p.reachable) {
          resEl.textContent = `OK${p.version ? " · " + p.version : ""}`;
          resEl.classList.remove("bad");
          state.hostTestedOK = true;
          $("#host-save").disabled = false;
        } else {
          resEl.textContent = p.error || "unreachable";
          resEl.classList.add("bad");
          state.hostTestedOK = false;
          $("#host-save").disabled = true;
        }
      } catch (e) {
        resEl.textContent = String(e).replace(/^Error:\s*/i, "");
        resEl.classList.add("bad");
        state.hostTestedOK = false;
        $("#host-save").disabled = true;
      }
    });
    $("#host-form")?.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      if (!state.hostTestedOK) {
        toast("Test connection successfully before saving", true);
        return;
      }
      const fd = new FormData(ev.target);
      try {
        await act("add host", () =>
          call("AddHost", {
            name: fd.get("name"),
            api: fd.get("api"),
            token: fd.get("token") || "",
            mcp_enabled: !!fd.get("mcp_enabled"),
            mcp_listen: fd.get("mcp_listen") || "",
          })
        );
        closeModal("modal-host");
        toast("Host saved");
        await loadHostMenu();
        await loadSettings();
      } catch (_) {}
    });
    // re-test required if fields change
    ["host-api", "host-token", "host-name"].forEach((id) => {
      $(`#${id}`)?.addEventListener("input", () => {
        state.hostTestedOK = false;
        $("#host-save").disabled = true;
      });
    });

    $("#mcp-save")?.addEventListener("click", async () => {
      try {
        await act("mcp save", () =>
          call("SetMCPEnabled", $("#mcp-enabled").checked, $("#mcp-listen-input").value)
        );
        toast("MCP config saved");
        await loadMCP();
      } catch (_) {}
    });
    $("#mcp-ensure")?.addEventListener("click", async () => {
      try {
        const st = await act("mcp ensure", () => call("EnsureMCPLocal"));
        toast(st.message || "done");
        await loadMCP();
      } catch (_) {}
    });
    $("#mcp-copy")?.addEventListener("click", async () => {
      const text = state.mcpSnippets[state.mcpAgent] || "";
      try {
        await navigator.clipboard.writeText(text);
        toast("Copied");
      } catch (_) {
        toast("Copy failed", true);
      }
    });

    $("#btn-token-gen")?.addEventListener("click", async () => {
      if (!confirm("Generate a new API token? This replaces any existing token in config.")) return;
      try {
        const res = await act("token generate", () => call("GenerateAPIToken"));
        const el = $("#token-once");
        el.hidden = false;
        el.textContent = res.token || "";
        toast(res.message || "Token generated");
        await loadSettings();
      } catch (_) {}
    });
    $("#btn-token-revoke")?.addEventListener("click", async () => {
      if (!confirm("Revoke API token in config?")) return;
      try {
        const res = await act("token revoke", () => call("RevokeAPIToken"));
        $("#token-once").hidden = true;
        toast(res.message || "Revoked");
        await loadSettings();
      } catch (_) {}
    });

    $("#btn-reload-cfg")?.addEventListener("click", async () => {
      await call("ReloadConfig");
      setConfigEditMode(false);
      await loadSettings();
      toast("Config reloaded");
    });
    $("#btn-config-edit")?.addEventListener("click", onConfigEditSave);
    const ta = $("#config-raw");
    ta?.addEventListener("input", () => {
      if (!state.configEditing) return;
      updateLineNumbers(ta.value);
      highlightConfig(ta.value);
    });
    ta?.addEventListener("keydown", (e) => {
      if (e.key === "Tab" && state.configEditing) {
        e.preventDefault();
        const start = ta.selectionStart;
        const end = ta.selectionEnd;
        ta.value = ta.value.slice(0, start) + "  " + ta.value.slice(end);
        ta.selectionStart = ta.selectionEnd = start + 2;
        updateLineNumbers(ta.value);
        highlightConfig(ta.value);
      }
    });
    $("#config-pane")?.addEventListener("scroll", () => {
      const lines = $("#config-lines");
      if (lines) lines.scrollTop = $("#config-pane").scrollTop;
    });

    $("#create-form")?.addEventListener("submit", onCreate);
    $("#create-form")?.name?.addEventListener("input", onNameHint);
    $("#sbox-form")?.addEventListener("submit", onSboxSave);

    $("#shell-only-reconnect")?.addEventListener("click", async () => {
      const vm = await call("ShellOnlyVM");
      if (vm && state.termOnly) await attachShell(vm, state.termOnly);
    });
    window.addEventListener("resize", () => {
      try {
        state.fit?.fit();
        state.fitOnly?.fit();
      } catch (_) {}
    });
  }

  async function boot() {
    let shellVM = "";
    try {
      shellVM = (await call("ShellOnlyVM")) || "";
    } catch (_) {}
    if (shellVM) {
      $("#splash").hidden = true;
      $("#app").hidden = true;
      $("#shell-only").hidden = false;
      $("#shell-only-title").textContent = shellVM;
      try {
        await call("EnsureReady");
        const term = ensureTerm("#xterm-only", "fitOnly", "termOnly");
        await attachShell(shellVM, term);
      } catch (e) {
        toast(String(e), true);
      }
      return;
    }

    $("#splash").hidden = false;
    $("#app").hidden = true;
    try {
      const splash = await call("EnsureReady");
      $("#splash-msg").textContent = splash.message || "Ready";
      if (splash.health) setHealth(splash.health);
      if (splash.error) toast(splash.error, true, { action: "ensure ready" });
      await loadHostMenu();
      await refreshList();
      showInspector(false);
      $("#btn-new").hidden = false;
      $("#splash").hidden = true;
      $("#app").hidden = false;
      state.pollTimer = setInterval(async () => {
        // Refresh active host health + list first; host menu probes secondaries only
        await refreshList();
        await loadHostMenu();
      }, 5000);
    } catch (e) {
      $("#splash").hidden = true;
      $("#app").hidden = false;
      try {
        await loadHostMenu();
        await refreshList();
      } catch (_) {}
      toast(String(e), true, { action: "boot" });
      state.pollTimer = setInterval(refreshList, 4000);
    }
  }

  document.addEventListener("DOMContentLoaded", () => {
    applyTheme(readThemePref(), { persist: false });
    if (window.matchMedia) {
      window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
        if (readThemePref() === "system") applyTheme("system", { persist: false });
      });
    }
    updateActivityBadge();
    wire();
    boot();
  });
})();
