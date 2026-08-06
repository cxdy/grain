/**
 * Grain Desktop — ship-ready operator console
 * Modular IIFE (no bundler); binds to Wails go.main.App
 */
(function () {
  "use strict";

  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const go = window.go?.main?.App;

  const EVENTS_KEY = "grain-desktop-activity";
  const THEME_KEY = "grain-desktop-theme";

  const state = {
    selected: null,
    sandboxes: [],
    selectedSet: new Set(),
    term: null,
    fit: null,
    termOnly: null,
    fitOnly: null,
    shellEventsBound: false,
    configEditing: false,
    activeTab: "overview",
    pollTimer: null,
    confirm: null,
    expandedEvent: null,
    currentView: "sandboxes",
  };

  /* ── helpers ── */
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

  /* ── activity + toasts ── */
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
  function pushEvent(action, ok, detail, extra) {
    const list = loadEvents();
    const ev = {
      id: "e" + Date.now() + Math.random().toString(36).slice(2, 7),
      t: new Date().toISOString(),
      action,
      ok: !!ok,
      detail: String(detail || "").slice(0, 4000),
      extra: extra || null,
    };
    list.unshift(ev);
    saveEvents(list);
    renderActivity();
    updateActivityBadge();
    return ev;
  }

  function toast(msg, isError, action, extra) {
    const el = $("#toast");
    if (!el) return;
    el.hidden = false;
    el.textContent = msg;
    el.classList.toggle("error", !!isError);
    const ev = pushEvent(action || "notify", !isError, msg, extra);
    el.dataset.eventId = ev.id;
    clearTimeout(toast._t);
    toast._t = setTimeout(() => {
      el.hidden = true;
    }, 5500);
  }

  function updateActivityBadge() {
    const list = loadEvents();
    const n = list.filter((e) => !e.ok).length;
    const b = $("#activity-badge");
    if (!b) return;
    if (n > 0) {
      b.hidden = false;
      b.textContent = n > 9 ? "9+" : String(n);
    } else {
      b.hidden = true;
    }
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
        const res = e.ok
          ? '<span class="badge-agent ok">ok</span>'
          : '<span class="badge-agent no">error</span>';
        const ms = e.extra?.duration_ms != null ? ` · ${e.extra.duration_ms}ms` : "";
        let body = escapeHtml(e.detail);
        if (e.extra?.log) body += "\n\n" + escapeHtml(e.extra.log);
        if (e.extra) {
          const copy = { ...e.extra };
          delete copy.log;
          if (Object.keys(copy).length) body += "\n\n" + escapeHtml(JSON.stringify(copy, null, 2));
        }
        return `<div class="activity-row ${open} ${hl}" data-eid="${escapeHtml(e.id)}">
          <div class="activity-row-head">
            <span><span class="muted">${escapeHtml(new Date(e.t).toLocaleString())}${ms}</span><br/>${escapeHtml(e.action)}</span>
            ${res}
          </div>
          <div class="activity-row-body">${body}</div>
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

  async function act(action, fn) {
    const t0 = performance.now();
    try {
      const r = await fn();
      const ms = Math.round(performance.now() - t0);
      pushEvent(action, true, "ok", { duration_ms: ms });
      return r;
    } catch (e) {
      const ms = Math.round(performance.now() - t0);
      const msg = String(e).replace(/^Error:\s*/i, "");
      pushEvent(action, false, msg, { duration_ms: ms });
      toast(msg, true, action);
      throw e;
    }
  }

  /* ── theme ──
   * THEME_KEY values: "light" | "dark" | "system" (or absent = follow OS).
   * applyTheme(theme, { persist }) — only persist when the user toggles.
   */
  function osTheme() {
    if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) return "light";
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
    document.documentElement.setAttribute("data-theme-pref", pref);
    const icon = $("#theme-icon");
    if (icon) icon.textContent = t === "light" ? "☾" : "☀";
    const link = $("#hljs-theme");
    if (link) {
      link.href =
        t === "light" ? "./vendor/highlight-github.min.css" : "./vendor/highlight-github-dark.min.css";
    }
    if (persist) {
      try {
        localStorage.setItem(THEME_KEY, pref);
      } catch (_) {}
    }
    const ta = $("#config-raw");
    if (ta) highlightConfig(ta.value);
  }

  function toggleTheme() {
    // User override: flip current rendered theme and persist light/dark
    const cur = document.documentElement.getAttribute("data-theme") === "light" ? "light" : "dark";
    applyTheme(cur === "light" ? "dark" : "light", { persist: true });
  }

  function setHealth(hs) {
    const dot = $("#health-dot");
    const label = $("#health-label");
    if (!dot) return;
    dot.classList.remove("ok", "bad");
    if (hs?.healthy) {
      dot.classList.add("ok");
      label.textContent = hs.local ? `local · ${hs.connection}` : `remote · ${hs.connection}`;
    } else {
      dot.classList.add("bad");
      label.textContent = hs?.message || "unhealthy";
    }
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
      return `<span class="badge-agent no agent-tip" title="grain agent deploy ${escapeHtml(vm.name)} to install guest-agent">not installed</span>`;
    }
    return `<span class="badge-agent no">—</span>`;
  }

  function showInspector(show) {
    const insp = $("#inspector");
    if (!insp) return;
    // On sandboxes view always show inspector column
    if (state.currentView === "sandboxes") {
      insp.hidden = false;
    } else {
      insp.hidden = true;
      return;
    }
    const empty = $("#inspector-empty");
    const body = $("#inspector-body");
    if (show && state.selected) {
      if (empty) empty.hidden = true;
      if (body) body.hidden = false;
    } else {
      if (empty) empty.hidden = false;
      if (body) body.hidden = true;
    }
  }

  function renderTable() {
    const tb = $("#vm-tbody");
    if (!tb) return;
    const list = state.sandboxes || [];
    const multi = list.length > 1;
    const th = $("#th-check");
    if (th) th.hidden = !multi;
    const bulk = $("#bulk-bar");
    if (bulk) bulk.hidden = !multi || state.selectedSet.size === 0;

    if (!list.length) {
      tb.innerHTML = `<tr><td colspan="6" class="empty-cell">
        No sandboxes yet.<br/><br/>
        <button type="button" class="btn btn-primary" id="empty-new">New sandbox</button>
      </td></tr>`;
      $("#empty-new")?.addEventListener("click", () => $("#btn-new")?.click());
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
          <td><strong>${escapeHtml(vm.name)}</strong></td>
          <td>${statusBadge(vm.status)}</td>
          <td>${agentCell(vm)}</td>
          <td class="muted">${escapeHtml(vm.image || "—")}</td>
          <td class="muted">${vm.cpus || "—"} / ${vm.memory_mb || "—"} MiB</td>
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
        if (bulk) bulk.hidden = state.selectedSet.size === 0;
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

  function fillDetailMeta(vm) {
    $("#detail-meta").innerHTML = `
      <dt>Status</dt><dd>${escapeHtml(vm.status || "—")}</dd>
      <dt>Image</dt><dd>${escapeHtml(vm.image || "—")}</dd>
      <dt>CPUs</dt><dd>${vm.cpus ?? "—"}</dd>
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
      <dt>Error</dt><dd>${escapeHtml(vm.error || "—")}</dd>`;
  }

  async function selectVM(name) {
    if (!name) return;
    const switched = state.selected !== name;
    state.selected = name;
    renderTable();
    showInspector(true);
    $("#detail-name").textContent = name;

    if (switched) {
      if (state.term && state.activeTab === "shell") {
        try {
          state.term.reset();
        } catch (_) {}
      }
      if (state.activeTab === "logs") {
        const lv = $("#log-view");
        if (lv) lv.textContent = "";
      }
    }

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
      fillDetailMeta(vm);

      if (state.activeTab === "shell") {
        const term = ensureTerm();
        if (term) await attachShell(name, term);
      } else if (state.activeTab === "logs") {
        await loadLogs();
      }
    } catch (e) {
      toast(String(e), true, "get " + name);
    }
  }

  function switchTab(name) {
    state.activeTab = name;
    $$(".insp-tab").forEach((t) => t.classList.toggle("active", t.dataset.tab === name));
    ["overview", "shell", "logs"].forEach((t) => {
      const p = $(`#tab-${t}`);
      if (p) p.hidden = t !== name;
    });
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
    try {
      const hs = await call("Health");
      setHealth(hs);
      state.sandboxes = (await call("ListSandboxes")) || [];
      const names = new Set(state.sandboxes.map((s) => s.name));
      for (const n of [...state.selectedSet]) {
        if (!names.has(n)) state.selectedSet.delete(n);
      }
      renderTable();
      if (state.selected && names.has(state.selected)) {
        const vm = state.sandboxes.find((s) => s.name === state.selected);
        if (vm) {
          updateActionButtons(vm.status);
          $("#detail-status-pill").innerHTML = statusBadge(vm.status);
          if (state.activeTab === "overview") fillDetailMeta(vm);
        }
      } else if (state.selected && !names.has(state.selected)) {
        state.selected = null;
        showInspector(false);
      } else {
        showInspector(!!state.selected);
      }
    } catch (e) {
      setHealth({ healthy: false, message: String(e) });
    }
  }

  /* ── connections ── */
  async function loadConnections() {
    const sel = $("#conn-select");
    if (!sel) return;
    const conns = (await call("ListConnections")) || [];
    const active = await call("GetActiveConnection");
    sel.innerHTML =
      conns
        .map(
          (c) =>
            `<option value="${escapeHtml(c.name)}" ${c.name === active ? "selected" : ""}>${escapeHtml(c.name)}${c.api ? " (remote)" : ""}</option>`
        )
        .join("") + `<option value="__add__">Add new host…</option>`;
    sel.value = active;
  }

  /* ── images ── */
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
    let opts = ready
      .map((id) => `<option value="${escapeHtml(id)}">${escapeHtml(id)} (ready)</option>`)
      .join("");
    for (const img of all) {
      if (seen.has(img.id)) continue;
      if (img.pullable)
        opts += `<option value="${escapeHtml(img.id)}">${escapeHtml(img.id)} (not pulled)</option>`;
    }
    opts += `<option value="__custom__">Custom…</option>`;
    sel.innerHTML =
      opts ||
      `<option value="grain-ubuntu">grain-ubuntu</option><option value="__custom__">Custom…</option>`;
    const custom = $("#create-image-custom");
    sel.onchange = () => {
      custom.hidden = sel.value !== "__custom__";
    };
  }

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
            let btn = img.ready
              ? '<span class="muted">on disk</span>'
              : '<span class="muted">—</span>';
            if (img.pullable && !img.ready) {
              btn = `<button type="button" class="btn btn-primary btn-sm" data-pull="${escapeHtml(img.id)}">Pull</button>`;
            }
            return `<tr>
              <td><code>${escapeHtml(img.id)}</code></td>
              <td>${st}</td>
              <td>${img.has_agent ? "yes" : "—"}</td>
              <td class="muted">${escapeHtml(img.description || "")}</td>
              <td>${btn}</td>
            </tr>`;
          })
          .join("") || '<tr><td colspan="5" class="empty-cell">No images</td></tr>';
      $$("[data-pull]").forEach((b) => b.addEventListener("click", () => pullImage(b)));
    } catch (e) {
      tb.innerHTML = `<tr><td colspan="5" class="empty-cell" style="color:var(--danger)">${escapeHtml(String(e))}</td></tr>`;
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
      // Ignore progress for other concurrent pulls if any
      if (p?.id && p.id !== id) return;
      const pct = p?.percent ?? 0;
      const w = p?.written || 0;
      const t = p?.total || 0;
      if (btn.isConnected) btn.textContent = t ? `Pulling (${pct}%)` : "Pulling…";
      const now = Date.now();
      // Throttle toasts so stacked events cannot spam (also only one handler)
      if (now - lastToast > 800) {
        lastToast = now;
        toast(
          `Pulling \`${id}\` — ${fmtBytes(w)}${t ? "/" + fmtBytes(t) : ""} (${pct}%)…`,
          false,
          "pull progress"
        );
      }
    };
    // Single subscription for this pull; always tear down (Wails EventsOff).
    if (window.runtime?.EventsOn) window.runtime.EventsOn("pull:progress", onProg);
    try {
      await act("pull " + id, () => call("PullImage", id));
      toast(`Pulled ${id}`);
      await loadImagesPage();
    } catch (_) {
      if (btn.isConnected) {
        btn.disabled = false;
        btn.textContent = "Pull";
      }
    } finally {
      try {
        if (window.runtime?.EventsOff) window.runtime.EventsOff("pull:progress");
      } catch (_) {}
    }
  }

  /* ── MCP ── */
  async function loadMCP() {
    try {
      const st = await call("GetMCPStatus");
      $("#mcp-status-text").textContent = st.message || "—";
      $("#mcp-listen").textContent = st.listen || "—";
      const pill = $("#mcp-pill");
      pill.textContent = st.listening ? "listening" : st.enabled ? "enabled" : "disabled";
      pill.className = "status-pill " + (st.listening ? "ok" : st.enabled ? "warn" : "bad");
      $("#mcp-enabled").checked = !!st.enabled;
      $("#mcp-listen-input").value = st.listen || "127.0.0.1:7476";
      $("#mcp-cursor").textContent = st.cursor_snippet || "";
      $("#mcp-claude").textContent = st.claude_snippet || "";
      $("#mcp-generic").textContent = st.generic_snippet || "";
      $("#mcp-ensure").disabled = !st.local;
      $("#mcp-hint").textContent = st.local
        ? "Local only: Desktop can start MCP with grain up --mcp."
        : "Remote host — Desktop cannot start MCP on the remote machine.";
    } catch (e) {
      $("#mcp-status-text").textContent = String(e);
    }
  }

  /* ── Doctor ── */
  async function runDoctor() {
    const root = $("#doctor-list");
    root.innerHTML = '<p class="muted">Running checks…</p>';
    try {
      const checks = (await act("doctor", () => call("RunDoctor"))) || [];
      if (!checks.length) {
        root.innerHTML = '<p class="muted">No checks returned.</p>';
        return;
      }
      root.innerHTML = checks
        .map((c) => {
          const icon = c.ok ? '<span class="ok">✓</span>' : '<span class="bad">✗</span>';
          const fix = c.fix ? `<div class="doctor-fix">${escapeHtml(c.fix)}</div>` : "";
          const cmd = c.command
            ? `<button type="button" class="doctor-cmd" data-cmd="${escapeHtml(c.command)}" title="Copy">${escapeHtml(c.command)}</button>`
            : "";
          return `<div class="doctor-row">
            ${icon}
            <strong>${escapeHtml(c.name)}</strong>
            <div><div>${escapeHtml(c.message || "")}</div>${fix}</div>
            <div>${cmd}</div>
          </div>`;
        })
        .join("");
      $$(".doctor-cmd").forEach((b) => {
        b.addEventListener("click", async () => {
          try {
            await navigator.clipboard.writeText(b.dataset.cmd);
            toast("Copied command");
          } catch (_) {
            toast(b.dataset.cmd);
          }
        });
      });
    } catch (e) {
      root.innerHTML = `<p class="hint bad">${escapeHtml(String(e))}</p>`;
    }
  }

  /* ── views ── */
  function switchView(name) {
    state.currentView = name;
    $$(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.view === name));
    $$(".view").forEach((v) => {
      v.hidden = v.id !== `view-${name}`;
    });
    showInspector(name === "sandboxes" && !!state.selected);
    if (name === "settings") {
      setConfigEditMode(false);
      loadSettings();
    }
    if (name === "images") loadImagesPage();
    if (name === "mcp") loadMCP();
    if (name === "doctor") runDoctor();
    if (name === "sandboxes") {
      showInspector(!!state.selected);
    }
  }

  async function contextRefresh() {
    if (state.currentView === "images") return loadImagesPage();
    if (state.currentView === "mcp") return loadMCP();
    if (state.currentView === "doctor") return runDoctor();
    if (state.currentView === "settings") return loadSettings();
    return refreshList();
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
      const line = `\r\n\x1b[90m// session closed ${msg}\x1b[0m\r\n`;
      if (state.term) state.term.write(line);
      if (state.termOnly) state.termOnly.write(line);
    });
  }

  async function attachShell(vm, term) {
    if (!vm || !term) return;
    bindShellEventsOnce();
    const cols = term.cols || 80;
    const rows = term.rows || 24;
    try {
      await call("ShellClose");
    } catch (_) {}
    term.reset();
    try {
      await call("ShellAttach", vm, cols, rows);
      term.writeln(`\x1b[32mconnected\x1b[0m \x1b[90m${vm}\x1b[0m`);
    } catch (e) {
      term.writeln(`\x1b[31m${String(e)}\x1b[0m`);
    }
  }

  async function openShellWindow() {
    if (!state.selected) return;
    try {
      await act("shell window " + state.selected, () => call("OpenShellWindow", state.selected));
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

  async function afterLifecycle(name) {
    await refreshList();
    if (state.selected === name) await selectVM(name);
  }

  /* ── config ── */
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
  async function loadSettings() {
    try {
      const sum = await call("GetConfigSummary");
      const hint = $("#settings-path-hint");
      if (hint) {
        hint.innerHTML = `Config file: <span class="selectable mono">${escapeHtml(sum.path || "—")}</span>
          · dial <span class="selectable mono">${escapeHtml(sum.dial_hint || "—")}</span>
          · defaults ${escapeHtml(sum.image || "—")} · ${sum.cpus} CPU · ${sum.memory_mb} MiB · ${sum.disk_gb} GiB`;
      }
      // Preferences form
      const conns = sum.connections || [];
      const defSel = $("#set-default-conn");
      if (defSel) {
        const def = sum.desktop?.default_connection || "local";
        defSel.innerHTML = conns
          .map(
            (c) =>
              `<option value="${escapeHtml(c.name)}" ${c.name === def ? "selected" : ""}>${escapeHtml(c.name)}${c.local ? "" : " (remote)"}</option>`
          )
          .join("");
        if (!conns.length) {
          defSel.innerHTML = `<option value="local">local</option>`;
        }
      }
      const startLocal = $("#set-start-local");
      if (startLocal) {
        // StartLocalDaemonEnabled: nil → true
        const v = sum.desktop?.start_local_daemon;
        startLocal.checked = v === undefined || v === null ? true : !!v;
      }
      const dataDir = $("#set-data-dir");
      if (dataDir) dataDir.value = sum.data_dir || "";
      const api = $("#set-api");
      if (api) api.value = sum.api || "";
      const apiUrl = $("#set-api-url");
      if (apiUrl) apiUrl.value = sum.api_url || "";

      // Connections list with delete (local is protected)
      const cl = $("#connections-list");
      if (cl) {
        if (!conns.length) {
          cl.innerHTML = '<p class="hint">No connections listed.</p>';
        } else {
          cl.innerHTML = conns
            .map((c) => {
              const detail = c.local
                ? "local unix socket"
                : escapeHtml(c.api || "remote");
              const del = c.local
                ? '<span class="muted small">built-in</span>'
                : `<button type="button" class="btn btn-danger-ghost btn-sm" data-del-host="${escapeHtml(c.name)}">Remove</button>`;
              return `<div class="conn-row">
                <div>
                  <div class="conn-name">${escapeHtml(c.name)}</div>
                  <div class="conn-meta">${detail}${c.token_env ? " · token_env " + escapeHtml(c.token_env) : ""}</div>
                </div>
                ${del}
              </div>`;
            })
            .join("");
          $$("[data-del-host]").forEach((b) => {
            b.addEventListener("click", async () => {
              const name = b.dataset.delHost;
              if (!confirm(`Remove host “${name}”?`)) return;
              try {
                await act("delete host " + name, () => call("DeleteHost", name));
                toast(`Removed ${name}`);
                await loadConnections();
                await loadSettings();
              } catch (_) {}
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
      toast(String(e), true, "settings");
    }
  }

  async function onSettingsFormSave(ev) {
    ev.preventDefault();
    const err = $("#settings-form-error");
    if (err) err.hidden = true;
    const form = {
      default_connection: $("#set-default-conn")?.value || "local",
      start_local_daemon: !!$("#set-start-local")?.checked,
      data_dir: $("#set-data-dir")?.value || "",
      api: $("#set-api")?.value || "",
      api_url: $("#set-api-url")?.value || "",
    };
    try {
      await act("settings save", () => call("SaveSettingsForm", form));
      toast("Preferences saved");
      await loadConnections();
      await refreshList();
      await loadSettings();
    } catch (e) {
      if (err) {
        err.hidden = false;
        err.textContent = String(e).replace(/^Error:\s*/i, "");
      }
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
      await loadConnections();
      await refreshList();
      await loadSettings();
    } catch (e) {
      if (err) {
        err.hidden = false;
        err.textContent = String(e).replace(/^Error:\s*/i, "");
      }
    }
  }

  /* ── create / lifecycle ── */
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
      wait: fd.get("wait") || "auto",
      timeout: fd.get("timeout") || "",
      arch: fd.get("arch") || "",
      gpu: fd.get("gpu") || "",
      network: fd.get("network") || "",
      publish: fd.get("publish") || "",
      mounts: fd.get("mounts") || "",
      userdata: fd.get("userdata") || "",
    };
    const st = $("#create-status");
    const submit = $("#create-submit");
    if (st) st.textContent = "Creating…";
    if (submit) submit.disabled = true;
    const t0 = performance.now();
    try {
      if (opts.name) await call("ValidateName", opts.name);
      const sb = await call("CreateSandbox", opts);
      const ms = Math.round(performance.now() - t0);
      pushEvent("create " + (sb?.name || opts.name || "?"), true, "ready", { duration_ms: ms });
      toast(`Created ${sb.name} (${ms}ms)`, false, "create " + sb.name, { duration_ms: ms });
      closeModal("modal-create");
      if (st) st.textContent = "";
      if (submit) submit.disabled = false;
      try {
        ev.target.reset();
      } catch (_) {}
      switchView("sandboxes");
      await refreshList();
      await selectVM(sb.name);
      switchTab("overview");
    } catch (e) {
      const ms = Math.round(performance.now() - t0);
      const msg = String(e).replace(/^Error:\s*/i, "");
      pushEvent("create", false, msg, { duration_ms: ms });
      if (st) st.textContent = msg;
      toast(msg, true, "create");
      if (submit) submit.disabled = false;
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
      toast(String(e), true, "sandbox config");
    }
  }

  async function onSboxSave(ev) {
    ev.preventDefault();
    const name = $("#sbox-edit-name").textContent;
    const fd = new FormData(ev.target);
    const patch = {
      cpus: Number(fd.get("cpus") || 0),
      memory_mb: Number(fd.get("memory_mb") || 0),
      disk_gb: Number(fd.get("disk_gb") || 0),
      image: fd.get("image") || "",
      persistent: !!fd.get("persistent"),
    };
    try {
      const res = await act("save meta " + name, () => call("SaveSandboxMeta", name, patch));
      closeModal("modal-sbox");
      if (res.disk_resized) toast(res.message || "Disk resized");
      if (res.needs_restart) {
        const yes = await confirmDialog(`Saved config for ${name}. Restart now so changes take effect?`);
        if (yes) {
          try {
            await act("restart " + name, async () => {
              await call("StopSandbox", name);
              await call("StartSandbox", name);
            });
            toast(`Restarted ${name}`);
          } catch (_) {}
        } else toast("Saved — restart later when ready");
      } else if (!res.disk_resized) {
        toast(res.message || "Saved");
      }
      await refreshList();
      if (state.selected === name) await selectVM(name);
    } catch (_) {}
  }

  async function bulk(kind) {
    const names = [...state.selectedSet];
    if (!names.length) return;
    if (kind === "rm" && !confirm(`Remove ${names.length} sandboxes?`)) return;
    for (const name of names) {
      try {
        if (kind === "start") await act("start " + name, () => call("StartSandbox", name));
        if (kind === "stop") await act("stop " + name, () => call("StopSandbox", name));
        if (kind === "rm") await act("remove " + name, () => call("RemoveSandbox", name));
      } catch (_) {}
    }
    state.selectedSet.clear();
    await refreshList();
  }

  /* ── wire ── */
  function wire() {
    $("#btn-theme")?.addEventListener("click", () => toggleTheme());
    $("#settings-form")?.addEventListener("submit", onSettingsFormSave);
    $("#btn-refresh")?.addEventListener("click", () => contextRefresh());
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

    $("#btn-new")?.addEventListener("click", async () => {
      await fillImageSelect();
      try {
        const d = await call("ConfigDefaults");
        const f = $("#create-form");
        if (d.cpus) f.cpus.value = d.cpus;
        if (d.memory_mb) f.memory_mb.value = d.memory_mb;
        if (d.disk_gb) f.disk_gb.value = d.disk_gb;
      } catch (_) {}
      const st = $("#create-status");
      if (st) st.textContent = "";
      const submit = $("#create-submit");
      if (submit) submit.disabled = false;
      openModal("modal-create");
    });

    $$(".nav-item").forEach((b) =>
      b.addEventListener("click", () => switchView(b.dataset.view))
    );

    $("#conn-select")?.addEventListener("change", async (e) => {
      if (e.target.value === "__add__") {
        await loadConnections();
        openModal("modal-host");
        return;
      }
      try {
        await act("switch host", () => call("SetActiveConnection", e.target.value));
        await refreshList();
      } catch (_) {}
    });

    $("#host-mcp")?.addEventListener("change", (e) => {
      $("#host-mcp-fields").hidden = !e.target.checked;
    });
    $("#host-form")?.addEventListener("submit", async (ev) => {
      ev.preventDefault();
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
        await loadConnections();
        toast("Host saved");
      } catch (_) {}
    });
    $("#btn-add-host")?.addEventListener("click", () => openModal("modal-host"));

    $$("[data-close]").forEach((b) =>
      b.addEventListener("click", () => closeModal(b.dataset.close))
    );
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

    $$(".insp-tab").forEach((t) =>
      t.addEventListener("click", () => switchTab(t.dataset.tab))
    );

    const moreBtn = $("#btn-more");
    const moreMenu = $("#more-menu");
    moreBtn?.addEventListener("click", (e) => {
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
          await act("terminal " + name, () => call("OpenSystemTerminal", name));
          toast("Opened Terminal");
          return;
        }
        if (actName === "logs") return switchTab("logs");
        if (actName === "edit") return openSandboxEdit(name);
        if (actName === "start") {
          await act("start " + name, () => call("StartSandbox", name));
          toast(`Started ${name}`);
          await afterLifecycle(name);
          return;
        }
        if (actName === "stop") {
          await act("stop " + name, () => call("StopSandbox", name));
          toast(`Stopped ${name}`);
          await afterLifecycle(name);
          return;
        }
        if (actName === "rm") {
          if (!confirm(`Remove ${name}?`)) return;
          await act("remove " + name, () => call("RemoveSandbox", name));
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
    $("#btn-doctor-run")?.addEventListener("click", runDoctor);

    $("#mcp-save")?.addEventListener("click", async () => {
      try {
        await act("mcp save", () =>
          call("SetMCPEnabled", $("#mcp-enabled").checked, $("#mcp-listen-input").value)
        );
        toast("MCP config saved — restart daemon if needed");
        await loadMCP();
      } catch (_) {}
    });
    $("#mcp-ensure")?.addEventListener("click", async () => {
      try {
        const st = await act("mcp ensure", () => call("EnsureMCPLocal"));
        toast(st.message || "MCP ensure done");
        await loadMCP();
      } catch (_) {}
    });
    $$("[data-copy]").forEach((b) => {
      b.addEventListener("click", async () => {
        const el = $(`#${b.dataset.copy}`);
        try {
          await navigator.clipboard.writeText(el?.textContent || "");
          toast("Copied");
        } catch (_) {
          toast("Copy failed", true);
        }
      });
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
        const val = ta.value;
        if (start !== end && val.slice(start, end).includes("\n")) {
          const before = val.slice(0, start);
          const sel = val.slice(start, end);
          const after = val.slice(end);
          const indented = sel
            .split("\n")
            .map((line) => "  " + line)
            .join("\n");
          ta.value = before + indented + after;
          ta.selectionStart = start;
          ta.selectionEnd = start + indented.length;
        } else {
          ta.value = val.slice(0, start) + "  " + val.slice(end);
          ta.selectionStart = ta.selectionEnd = start + 2;
        }
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

    $("#toast")?.addEventListener("click", () => {
      const id = $("#toast").dataset.eventId;
      openActivity(id || null);
    });

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
      if (splash.error) toast(splash.error, true, "ensure ready");
      await loadConnections();
      await refreshList();
      showInspector(false);
      $("#splash").hidden = true;
      $("#app").hidden = false;
      state.pollTimer = setInterval(refreshList, 3000);
    } catch (e) {
      $("#splash").hidden = true;
      $("#app").hidden = false;
      try {
        await loadConnections();
        await refreshList();
      } catch (_) {}
      toast(String(e), true, "boot");
      state.pollTimer = setInterval(refreshList, 3000);
    }
  }

  document.addEventListener("DOMContentLoaded", () => {
    // Follow OS by default; do not freeze theme into localStorage on first paint.
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
