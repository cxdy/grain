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
    const open = pop.hidden;
    pop.hidden = !open;
    btn?.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) loadHostMenu();
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

  function updateActionButtons(status) {
    const running = (status || "").toLowerCase() === "running";
    const start = $("#btn-start");
    const stop = $("#btn-stop");
    if (start) start.hidden = running;
    if (stop) stop.hidden = !running;
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
      <dt>Agent</dt><dd>${
        vm.agent_ok === true
          ? escapeHtml(vm.agent_version || "ok")
          : vm.agent_ok === false
            ? "not installed"
            : "—"
      }</dd>
      <dt>Metrics</dt><dd>${vm.metrics_enabled ? "on (host ring)" : "off"}</dd>
      <dt>Error</dt><dd class="selectable">${escapeHtml(vm.error || "—")}</dd>`;
  }

  function fillInspectorSummary(vm) {
    const el = $("#inspector-summary");
    if (!el || !vm) return;
    const agent =
      vm.agent_ok === true
        ? vm.agent_version || "ok"
        : vm.agent_ok === false
          ? "agent not installed"
          : "agent —";
    el.textContent = `${vm.image || "—"} · ${vm.cpus || "—"} vCPUs · ${vm.memory_mb || "—"} MiB · ${agent}${vm.metrics_enabled ? " · metrics on" : ""}`;
  }

  function fmtBytesShort(n) {
    if (n == null || n === 0) return "0";
    if (n > 1e9) return (n / 1e9).toFixed(1) + "G";
    if (n > 1e6) return (n / 1e6).toFixed(0) + "M";
    if (n > 1e3) return (n / 1e3).toFixed(0) + "K";
    return String(n);
  }

  function drawSpark(canvasId, values, color) {
    const c = $(canvasId);
    if (!c) return;
    const dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth || 640;
    const h = 72;
    c.width = Math.floor(w * dpr);
    c.height = Math.floor(h * dpr);
    const ctx = c.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    if (!values || values.length < 2) {
      ctx.fillStyle = "rgba(128,128,128,0.35)";
      ctx.font = "12px sans-serif";
      ctx.fillText("No samples yet — wait for collection interval", 8, h / 2);
      return;
    }
    let min = Math.min(...values);
    let max = Math.max(...values);
    if (min === max) {
      min -= 1;
      max += 1;
    }
    const pad = 4;
    ctx.strokeStyle = color || "#3ddea8";
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    values.forEach((v, i) => {
      const x = pad + (i / (values.length - 1)) * (w - pad * 2);
      const y = h - pad - ((v - min) / (max - min)) * (h - pad * 2);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();
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
      const pts = h.points || [];
      const loads = pts.map((p) => p.load1 || 0);
      const memUsed = pts.map((p) => {
        const t = p.mem_total_bytes || 0;
        const a = p.mem_available_bytes || 0;
        return t > 0 ? ((t - a) / t) * 100 : 0;
      });
      const diskUsed = pts.map((p) => {
        const t = p.disk_total_bytes || 0;
        const f = p.disk_free_bytes || 0;
        return t > 0 ? ((t - f) / t) * 100 : 0;
      });
      const net = pts.map((p) => (p.net_rx_bytes || 0) + (p.net_tx_bytes || 0));
      drawSpark("#chart-cpu", loads, "#3ddea8");
      drawSpark("#chart-mem", memUsed, "#6cb6ff");
      drawSpark("#chart-disk", diskUsed, "#e0b050");
      drawSpark("#chart-net", net, "#c084fc");
      const last = pts[pts.length - 1];
      if (last) {
        const mt = last.mem_total_bytes || 0;
        const ma = last.mem_available_bytes || 0;
        const mu = mt > 0 ? mt - ma : 0;
        const dt = last.disk_total_bytes || 0;
        const df = last.disk_free_bytes || 0;
        const du = dt > 0 ? dt - df : 0;
        $("#m-cpu-val").textContent = (last.load1 || 0).toFixed(2);
        $("#m-mem-val").textContent =
          mt > 0 ? `${fmtBytesShort(mu)} / ${fmtBytesShort(mt)} (${((mu / mt) * 100).toFixed(0)}%)` : "—";
        $("#m-disk-val").textContent =
          dt > 0 ? `${fmtBytesShort(du)} / ${fmtBytesShort(dt)} (${((du / dt) * 100).toFixed(0)}%)` : "—";
        $("#m-net-val").textContent = `↓${fmtBytesShort(last.net_rx_bytes || 0)} ↑${fmtBytesShort(last.net_tx_bytes || 0)}`;
      } else {
        $("#m-cpu-val").textContent = "—";
        $("#m-mem-val").textContent = "—";
        $("#m-disk-val").textContent = "—";
        $("#m-net-val").textContent = "—";
      }
      const meta = $("#metrics-meta");
      if (meta) {
        meta.textContent = `${pts.length} samples · interval ${h.interval || "—"} · stored on Grain host (data_dir/vms/${name}/metrics.ring)`;
      }
    } catch (e) {
      const msg = String(e).replace(/^Error:\s*/i, "");
      if (charts) charts.hidden = true;
      if (disabled) disabled.hidden = true;
      if (hint) {
        hint.hidden = false;
        // 404 = daemon too old (no metrics route) — most common after Desktop-only rebuild
        if (/404|not found/i.test(msg)) {
          hint.innerHTML = `<span class="hint bad">Metrics API not found (HTTP 404).</span>
            <span class="hint"> The Grain <strong>daemon</strong> on this host is older than the Desktop client.
            Rebuild and restart it: <code>just build && grain down && grain up</code>, then reopen Overview.
            Metrics are stored on that host, not in the Desktop app.</span>`;
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
        if (!vm.disk_gb && fromList.disk_gb) vm.disk_gb = fromList.disk_gb;
      }
      updateActionButtons(vm.status);
      $("#detail-status-pill").innerHTML = statusBadge(vm.status);
      fillMeta(vm, $("#detail-meta"));
      fillInspectorSummary(vm);
      fillMeta(vm, $("#inspector-meta"));
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
          updateActionButtons(vm.status);
          $("#detail-status-pill").innerHTML = statusBadge(vm.status);
          if (state.activeTab === "overview") {
            fillMeta(vm, $("#detail-meta"));
            fillInspectorSummary(vm);
            fillMeta(vm, $("#inspector-meta"));
          }
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
      const defSel = $("#set-default-conn");
      if (defSel) {
        const def = sum.desktop?.default_connection || "local";
        defSel.innerHTML =
          conns
            .map(
              (c) =>
                `<option value="${escapeHtml(c.name)}" ${c.name === def ? "selected" : ""}>${escapeHtml(c.name)}</option>`
            )
            .join("") || `<option value="local">local</option>`;
      }
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
    if (name === "sandboxes") showInspector(!!state.selected);
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

  async function loadLogs() {
    if (!state.selected) return;
    const view = $("#log-view");
    if (!view) return;
    view.textContent = "Loading…";
    try {
      const res = await call("ReadLogs", state.selected, $("#log-source")?.value || "serial");
      view.textContent = res.missing
        ? `No log at ${res.path || "?"}`
        : (res.truncated ? "…\n" : "") + (res.content || "");
    } catch (e) {
      view.textContent = String(e);
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
    document.addEventListener("click", (e) => {
      if (!$("#host-menu")?.contains(e.target)) closeHostMenu();
    });
    $("#host-add")?.addEventListener("click", () => {
      closeHostMenu();
      openHostModal(null, []);
    });

    $$(".nav-item").forEach((b) => b.addEventListener("click", () => switchView(b.dataset.view)));
    $$(".ws-tab").forEach((t) => t.addEventListener("click", () => switchTab(t.dataset.tab)));
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
        if (actName === "rm") {
          if (!confirm(`Remove ${name}?`)) return;
          await act("remove", () => call("RemoveSandbox", name), { target: name });
          state.selected = null;
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
