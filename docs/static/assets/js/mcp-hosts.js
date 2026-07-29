/**
 * MCP host config picker — per-agent snippets with stdio ↔ HTTP toggle
 * and split OS/path variants where hosts differ (e.g. Claude Desktop).
 */
(function () {
  "use strict";

  var HTTP_URL = "http://127.0.0.1:7476/mcp";

  /**
   * Each host: id, name, defaultTransport, transports.stdio / transports.http
   * Transport config:
   *   path | paths[]  — string or {id,label,path}[] for multi-OS
   *   note, lang, prism, cli?, snippet
   */
  var HOSTS = [
    {
      id: "claude-code",
      name: "Claude Code",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          path: "Project .mcp.json  ·  or user ~/.claude.json",
          note:
            "Claude Code is not Claude Desktop. Prefer project-scoped .mcp.json (team-shareable) or the CLI.",
          lang: "JSON",
          prism: "json",
          cli: "claude mcp add --transport stdio grain -- grain mcp",
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "command": "grain",\n      "args": ["mcp"]\n    }\n  }\n}',
        },
        http: {
          path: "Project .mcp.json  ·  or user ~/.claude.json",
          note:
            "Requires grain up --mcp (or mcp.enabled: true). type http is required when using url.",
          lang: "JSON",
          prism: "json",
          cli: "claude mcp add --transport http grain " + HTTP_URL,
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "type": "http",\n      "url": "' +
            HTTP_URL +
            '"\n    }\n  }\n}',
        },
      },
    },
    {
      id: "claude-desktop",
      name: "Claude Desktop",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          paths: [
            {
              id: "macos",
              label: "macOS",
              path: "~/Library/Application Support/Claude/claude_desktop_config.json",
            },
            {
              id: "windows",
              label: "Windows",
              path: "%AppData%\\Claude\\claude_desktop_config.json",
            },
          ],
          note:
            "Fully quit and reopen Claude Desktop after editing. Merge into existing mcpServers if present.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "command": "grain",\n      "args": ["mcp"]\n    }\n  }\n}',
        },
        http: {
          paths: [
            {
              id: "macos",
              label: "macOS",
              path: "~/Library/Application Support/Claude/claude_desktop_config.json",
            },
            {
              id: "windows",
              label: "Windows",
              path: "%AppData%\\Claude\\claude_desktop_config.json",
            },
          ],
          note:
            "Desktop often expects stdio; this bridges Streamable HTTP via mcp-remote. Requires grain up --mcp and Node/npx.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "command": "npx",\n      "args": ["-y", "mcp-remote", "' +
            HTTP_URL +
            '"]\n    }\n  }\n}',
        },
      },
    },
    {
      id: "codex",
      name: "Codex",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          path: "~/.codex/config.toml  ·  or project .codex/config.toml",
          note:
            "Top-level key is mcp_servers (snake_case), not mcpServers. Restart Codex or reload MCP after saving.",
          lang: "TOML",
          prism: "toml",
          snippet:
            '[mcp_servers.grain]\ncommand = "grain"\nargs = ["mcp"]\n# Optional: raise if create/exec feels like a hang\n# startup_timeout_sec = 30',
        },
        http: {
          path: "~/.codex/config.toml  ·  or project .codex/config.toml",
          note: "Streamable HTTP — start grain with grain up --mcp first.",
          lang: "TOML",
          prism: "toml",
          snippet:
            '[mcp_servers.grain]\nurl = "' + HTTP_URL + '"',
        },
      },
    },
    {
      id: "cursor",
      name: "Cursor",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          path: "~/.cursor/mcp.json  ·  or project .cursor/mcp.json",
          note: "Settings → Tools & MCP, or edit the JSON file. Restart Cursor if tools do not appear.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "command": "grain",\n      "args": ["mcp"]\n    }\n  }\n}',
        },
        http: {
          path: "~/.cursor/mcp.json  ·  or project .cursor/mcp.json",
          note: "Remote / HTTP server entry. Requires grain up --mcp.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "mcpServers": {\n    "grain": {\n      "url": "' +
            HTTP_URL +
            '"\n    }\n  }\n}',
        },
      },
    },
    {
      id: "grok",
      name: "Grok Build",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          path: "~/.grok/config.toml",
          note:
            "Use an absolute path to grain if it is not on PATH for the Grok process. tool_timeout_sec should cover cold boots and long grain_exec runs.",
          lang: "TOML",
          prism: "toml",
          snippet:
            '[mcp_servers.grain]\ncommand = "grain"\nargs = ["mcp"]\nenabled = true\ntool_timeout_sec = 600\nstartup_timeout_sec = 15',
        },
        http: {
          path: "~/.grok/config.toml",
          note:
            "If your Grok build supports URL MCP servers, use this after grain up --mcp. Prefer stdio when the CLI is local.",
          lang: "TOML",
          prism: "toml",
          snippet:
            '[mcp_servers.grain]\nurl = "' +
            HTTP_URL +
            '"\nenabled = true\ntool_timeout_sec = 600',
        },
      },
    },
    {
      id: "opencode",
      name: "OpenCode",
      defaultTransport: "stdio",
      transports: {
        stdio: {
          path: "~/.config/opencode/opencode.json  ·  or project opencode.json",
          note:
            "OpenCode uses an mcp map with type local and command as an argv array — not the Claude-style mcpServers object.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "$schema": "https://opencode.ai/config.json",\n  "mcp": {\n    "grain": {\n      "type": "local",\n      "command": ["grain", "mcp"],\n      "enabled": true,\n      "timeout": 600000\n    }\n  }\n}',
        },
        http: {
          path: "~/.config/opencode/opencode.json  ·  or project opencode.json",
          note: "Remote MCP — requires grain up --mcp.",
          lang: "JSON",
          prism: "json",
          snippet:
            '{\n  "$schema": "https://opencode.ai/config.json",\n  "mcp": {\n    "grain": {\n      "type": "remote",\n      "url": "' +
            HTTP_URL +
            '",\n      "enabled": true,\n      "timeout": 600000\n    }\n  }\n}',
        },
      },
    },
  ];

  function init(root) {
    if (!root || root.getAttribute("data-ready") === "1") return;
    root.setAttribute("data-ready", "1");

    var rail = root.querySelector(".mcp-hosts-rail");
    var pathStack = root.querySelector("[data-mcp-path-stack]");
    var noteEl = root.querySelector("[data-mcp-note]");
    var langEl = root.querySelector("[data-mcp-lang]");
    var preEl = root.querySelector("pre.mcp-hosts-code");
    var snippetEl = root.querySelector("[data-mcp-snippet]");
    var cliEl = root.querySelector("[data-mcp-cli]");
    var copyBtn = root.querySelector("[data-mcp-copy]");
    var transportGroup = root.querySelector("[data-mcp-transport-toggle]");
    if (!rail || !snippetEl || !pathStack || !transportGroup) return;

    var state = {
      hostId: HOSTS[0].id,
      transport: HOSTS[0].defaultTransport || "stdio",
      pathId: null,
    };

    HOSTS.forEach(function (h, i) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "mcp-hosts-tab" + (i === 0 ? " is-active" : "");
      btn.setAttribute("role", "tab");
      btn.setAttribute("aria-selected", i === 0 ? "true" : "false");
      btn.setAttribute("data-host", h.id);
      btn.textContent = h.name;
      btn.addEventListener("click", function () {
        selectHost(h.id);
      });
      rail.appendChild(btn);
    });

    transportGroup.querySelectorAll("[data-transport]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        var t = btn.getAttribute("data-transport");
        if (!t || t === state.transport) return;
        var host = currentHost();
        if (!host || !host.transports[t]) return;
        state.transport = t;
        state.pathId = null;
        render();
      });
    });

    function currentHost() {
      return HOSTS.find(function (x) {
        return x.id === state.hostId;
      });
    }

    function currentCfg() {
      var h = currentHost();
      if (!h) return null;
      return h.transports[state.transport] || h.transports.stdio || null;
    }

    function selectHost(id) {
      var h = HOSTS.find(function (x) {
        return x.id === id;
      });
      if (!h) return;
      state.hostId = id;
      if (!h.transports[state.transport]) {
        state.transport = h.defaultTransport || "stdio";
      }
      state.pathId = null;
      render();
    }

    function setLanguageClasses(pl) {
      if (!preEl || !snippetEl) return;
      preEl.className = "mcp-hosts-code" + (pl ? " language-" + pl : "");
      snippetEl.className = pl ? "language-" + pl : "";
    }

    function highlightSnippet() {
      var cfg = currentCfg();
      if (!snippetEl || !cfg || !cfg.prism || !window.Prism) return;
      try {
        window.Prism.highlightElement(snippetEl);
      } catch (e) {}
    }

    function escapeHtml(s) {
      return String(s)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;");
    }

    function renderPaths(cfg) {
      pathStack.innerHTML = "";
      var variants =
        cfg.paths && cfg.paths.length
          ? cfg.paths
          : cfg.path
            ? [{ id: "default", label: null, path: cfg.path }]
            : [];

      if (!variants.length) return;

      if (!state.pathId || !variants.some(function (v) {
        return v.id === state.pathId;
      })) {
        state.pathId = variants[0].id;
      }

      // Multi-OS: segmented control + one path block
      if (variants.length > 1 && variants[0].label) {
        var seg = document.createElement("div");
        seg.className = "mcp-hosts-os-toggle";
        seg.setAttribute("role", "tablist");
        seg.setAttribute("aria-label", "Operating system");
        variants.forEach(function (v) {
          var b = document.createElement("button");
          b.type = "button";
          b.className =
            "mcp-hosts-os-btn" + (v.id === state.pathId ? " is-active" : "");
          b.setAttribute("role", "tab");
          b.setAttribute("aria-selected", v.id === state.pathId ? "true" : "false");
          b.textContent = v.label;
          b.addEventListener("click", function () {
            state.pathId = v.id;
            renderPaths(cfg);
          });
          seg.appendChild(b);
        });
        pathStack.appendChild(seg);
      }

      var active =
        variants.find(function (v) {
          return v.id === state.pathId;
        }) || variants[0];

      var row = document.createElement("div");
      row.className = "mcp-hosts-path-block";
      var label = document.createElement("span");
      label.className = "mcp-hosts-label";
      label.textContent = "Config file";
      row.appendChild(label);

      var box = document.createElement("div");
      box.className = "mcp-hosts-path-copy";
      var code = document.createElement("code");
      code.className = "mcp-hosts-path";
      code.textContent = active.path;
      box.appendChild(code);

      var pathCopy = document.createElement("button");
      pathCopy.type = "button";
      pathCopy.className = "copy-btn mcp-hosts-path-copy-btn";
      pathCopy.textContent = "Copy path";
      pathCopy.addEventListener("click", function () {
        navigator.clipboard.writeText(active.path).then(function () {
          var prev = pathCopy.textContent;
          pathCopy.textContent = "Copied";
          setTimeout(function () {
            pathCopy.textContent = prev;
          }, 1400);
        });
      });
      box.appendChild(pathCopy);
      row.appendChild(box);
      pathStack.appendChild(row);
    }

    function render() {
      var h = currentHost();
      var cfg = currentCfg();
      if (!h || !cfg) return;

      rail.querySelectorAll(".mcp-hosts-tab").forEach(function (b) {
        var on = b.getAttribute("data-host") === state.hostId;
        b.classList.toggle("is-active", on);
        b.setAttribute("aria-selected", on ? "true" : "false");
      });

      transportGroup.querySelectorAll("[data-transport]").forEach(function (btn) {
        var t = btn.getAttribute("data-transport");
        var available = !!h.transports[t];
        btn.disabled = !available;
        btn.classList.toggle("is-active", t === state.transport);
        btn.setAttribute("aria-pressed", t === state.transport ? "true" : "false");
        btn.hidden = !available;
      });

      renderPaths(cfg);
      noteEl.textContent = cfg.note || "";
      langEl.textContent = cfg.lang || "TEXT";
      setLanguageClasses(cfg.prism || "");
      snippetEl.textContent = cfg.snippet || "";

      if (cfg.cli) {
        cliEl.hidden = false;
        cliEl.innerHTML =
          "<strong>CLI</strong> <code>" + escapeHtml(cfg.cli) + "</code>";
      } else {
        cliEl.hidden = true;
        cliEl.textContent = "";
      }

      highlightSnippet();
      if (cfg.prism && !window.Prism) {
        var tries = 0;
        var t = setInterval(function () {
          tries++;
          if (window.Prism || tries > 40) {
            clearInterval(t);
            highlightSnippet();
          }
        }, 50);
      }
    }

    if (copyBtn) {
      copyBtn.addEventListener("click", function () {
        var cfg = currentCfg();
        var text = (cfg && cfg.snippet) || "";
        navigator.clipboard.writeText(text).then(function () {
          var prev = copyBtn.textContent;
          copyBtn.textContent = "Copied";
          setTimeout(function () {
            copyBtn.textContent = prev;
          }, 1400);
        });
      });
    }

    render();
  }

  function boot() {
    document.querySelectorAll("[data-mcp-hosts]").forEach(init);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();
