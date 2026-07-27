(function () {
  function initDemo(root) {
    if (!root || root.getAttribute("data-demo-ready") === "1") return;
    root.setAttribute("data-demo-ready", "1");

    var outputEl = root.querySelector("[data-demo-output]");
    var inputEl = root.querySelector("[data-demo-input]");
    var promptEl = root.querySelector("[data-demo-prompt]");
    var ghostEl = root.querySelector("[data-demo-ghost]");
    var inputLine = root.querySelector("[data-demo-input-line]");
    var stepsEl = root.querySelector("[data-demo-steps]");
    var hintEl = root.querySelector("[data-demo-hint]");
    var nextBtn = root.querySelector("[data-demo-next]");
    var skipBtn = root.querySelector("[data-demo-skip]");
    var restartBtn = root.querySelector("[data-demo-restart]");
    var titleEl = root.querySelector("[data-demo-title]");
    var term = root.querySelector("[data-demo-term]");

    if (!outputEl || !inputEl || !promptEl || !stepsEl || !hintEl || !nextBtn || !skipBtn || !term) {
      console.warn("grain demo: missing DOM nodes", root);
      return;
    }

    var steps = [
      {
        id: "install",
        title: "Install grain",
        hint: "Type the install command, or press Run step →",
        expect: [
          "curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash",
          "curl | bash",
          "install.sh",
        ],
        suggest: "curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash",
        run: function () {
          return typeLines([
            { text: "→ detecting os=darwin arch=arm64", cls: "dim", delay: 200 },
            { text: "→ downloading grain_darwin_arm64 …", cls: "dim", delay: 400 },
            { text: "✓ installed /usr/local/bin/grain", cls: "ok", delay: 350 },
            { text: "✓ guest agent → ~/.grain/agent/", cls: "ok", delay: 200 },
            { text: "", delay: 80 },
            { text: "grain 0.1.0", cls: "out", delay: 150 },
          ]);
        },
      },
      {
        id: "up",
        title: "Start the daemon",
        hint: "Start the local control plane with grain up",
        expect: ["grain up"],
        suggest: "grain up",
        run: function () {
          return typeLines([
            { text: "grain up  pid=4242", cls: "out", delay: 250 },
            { text: "  socket  ~/.grain/grain.sock", cls: "dim", delay: 180 },
            { text: "  api     http://127.0.0.1:7474", cls: "dim", delay: 150 },
            { text: "  metrics http://127.0.0.1:7474/metrics", cls: "dim", delay: 120 },
          ]);
        },
      },
      {
        id: "pull",
        title: "Pull a base image",
        hint: "Download the golden image once (simulated)",
        expect: ["grain image pull grain-ubuntu", "grain image pull", "image pull"],
        suggest: "grain image pull grain-ubuntu",
        run: function () {
          return typeLines([
            { text: "pulling grain-ubuntu …", cls: "out", delay: 200 },
            { text: "  64 / 320 MB", cls: "dim", delay: 280 },
            { text: "  192 / 320 MB", cls: "dim", delay: 280 },
            { text: "  320 / 320 MB", cls: "dim", delay: 260 },
            { text: "ok grain-ubuntu in 4s  (sha256 verified)", cls: "ok", delay: 220 },
            { text: "ssh user: ubuntu · agent: baked-in", cls: "dim", delay: 150 },
          ]);
        },
      },
      {
        id: "new",
        title: "Create a sandbox",
        hint: "Launch an ephemeral VM (grain new)",
        expect: ["grain new", "new"],
        suggest: "grain new",
        run: function () {
          return typeLines([
            { text: "  ⠹ creating  image         0s", cls: "dim", delay: 200 },
            { text: "  ⠸ creating  disk          1s", cls: "dim", delay: 350 },
            { text: "  ⠼ creating  boot          2s", cls: "dim", delay: 400 },
            { text: "  ⠴ creating  waiting agent 4s", cls: "dim", delay: 500 },
            {
              text: "created sbox-1  status=running  image=grain-ubuntu  persist=false  ssh=:2201  (6s)",
              cls: "ok",
              delay: 280,
            },
            { text: "next:  grain sh sbox-1", cls: "out", delay: 160 },
            { text: "       grain x sbox-1 -- uname -a", cls: "dim", delay: 120 },
          ]);
        },
      },
      {
        id: "sh",
        title: "Open a shell",
        hint: "Connect with grain sh — agent PTY by default",
        expect: ["grain sh", "grain sh sbox-1", "sh"],
        suggest: "grain sh",
        run: function () {
          return typeLines([
            { text: "connecting via agent to sbox-1 …", cls: "dim", delay: 300 },
            { text: "", delay: 100 },
            { text: "Welcome to Ubuntu 24.04 LTS (grain sandbox)", cls: "out", delay: 200 },
            { text: "  agent 0.2.0 · ephemeral · sbox-1", cls: "dim", delay: 160 },
          ]).then(function () {
            state.mode = "guest";
            setPrompt("ubuntu@sbox-1:~$", true);
            setTitle("ubuntu@sbox-1: ~");
            setGhost("uname -a");
            hintEl.textContent = "You're in the VM. Try uname -a, ls, or exit when done.";
            nextBtn.textContent = "Finish demo";
          });
        },
      },
    ];

    var guestCmds = {
      "uname -a": "Linux sbox-1 6.8.0-grain #1 SMP PREEMPT_DYNAMIC aarch64 GNU/Linux",
      uname: "Linux",
      ls: "work  README.md",
      "ls -la":
        "total 12\ndrwxr-x--- 1 ubuntu ubuntu  80 Jul 26 12:00 .\ndrwxr-xr-x 1 root   root    60 Jul 26 12:00 ..\n-rw-r--r-- 1 ubuntu ubuntu  42 Jul 26 12:00 README.md\ndrwxr-xr-x 1 ubuntu ubuntu  40 Jul 26 12:00 work",
      pwd: "/home/ubuntu",
      whoami: "ubuntu",
      hostname: "sbox-1",
      "cat /etc/os-release":
        'NAME="Ubuntu"\nVERSION="24.04 LTS (Noble Numbat)"\nID=ubuntu\nPRETTY_NAME="Ubuntu 24.04 LTS"',
      help: "demo commands: uname -a, ls, pwd, whoami, hostname, cat /etc/os-release, exit",
      clear: "__clear__",
      exit: "__exit__",
      logout: "__exit__",
    };

    var state = {
      step: 0,
      mode: "host",
      busy: false,
    };

    function sleep(ms) {
      return new Promise(function (r) {
        setTimeout(r, ms);
      });
    }

    function scrollTerm() {
      term.scrollTop = term.scrollHeight;
    }

    function appendLine(text, cls) {
      var line = document.createElement("div");
      line.className = "demo-line" + (cls ? " " + cls : "");
      line.textContent = text;
      outputEl.appendChild(line);
      scrollTerm();
      return line;
    }

    function appendHtml(html, cls) {
      var line = document.createElement("div");
      line.className = "demo-line" + (cls ? " " + cls : "");
      line.innerHTML = html;
      outputEl.appendChild(line);
      scrollTerm();
      return line;
    }

    function typeLines(items) {
      state.busy = true;
      inputEl.disabled = true;
      var i = 0;
      function next() {
        if (i >= items.length) {
          state.busy = false;
          inputEl.disabled = false;
          focusDemoInput(inputEl);
          return Promise.resolve();
        }
        var item = items[i++];
        return sleep(item.delay || 120).then(function () {
          if (item.text === "" && !item.cls) {
            appendLine("\u00a0");
          } else {
            appendLine(item.text, item.cls);
          }
          return next();
        });
      }
      return next();
    }

    function setTitle(t) {
      if (titleEl) titleEl.textContent = t;
    }

    function setPrompt(p, guest) {
      promptEl.textContent = p;
      promptEl.classList.toggle("is-guest", !!guest);
      requestAnimationFrame(function () {
        if (!inputLine) return;
        var w = promptEl.getBoundingClientRect().width;
        inputLine.style.setProperty("--ghost-pad", Math.ceil(w + 8) + "px");
      });
    }

    function setGhost(text) {
      if (!ghostEl) return;
      if (inputEl.value) {
        ghostEl.textContent = "";
        return;
      }
      ghostEl.textContent = text || "";
    }

    function currentSuggest() {
      if (state.mode === "guest") return "uname -a";
      if (state.mode === "done") return "";
      var step = steps[state.step];
      return step ? step.suggest : "";
    }

    function renderSteps() {
      stepsEl.innerHTML = steps
        .map(function (s, i) {
          var cls = i < state.step ? "done" : i === state.step ? "current" : "todo";
          if (state.mode === "guest") {
            cls = i < steps.length - 1 ? "done" : "current";
          } else if (state.mode === "done") {
            cls = "done";
          }
          return (
            '<li class="' +
            cls +
            '"><span class="demo-step-idx">' +
            (i + 1) +
            '</span><span class="demo-step-label">' +
            escapeHtml(s.title) +
            "</span></li>"
          );
        })
        .join("");
    }

    function escapeHtml(s) {
      return String(s)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;");
    }

    function normalizeCmd(s) {
      return s.trim().replace(/\s+/g, " ").toLowerCase();
    }

    function matchesExpect(input, expectList) {
      var n = normalizeCmd(input);
      return expectList.some(function (e) {
        var e2 = normalizeCmd(e);
        return n === e2 || n.indexOf(e2) !== -1 || (e2.indexOf(n) !== -1 && n.length > 3);
      });
    }

    function echoCommand(cmd) {
      appendHtml(
        '<span class="demo-echo-prompt">' +
          escapeHtml(promptEl.textContent) +
          "</span> " +
          escapeHtml(cmd),
        "echo"
      );
    }

    function runHostStep(force) {
      if (state.busy || state.mode !== "host") return;
      var step = steps[state.step];
      if (!step) return;
      var cmd = force ? step.suggest : inputEl.value;
      if (!force && !matchesExpect(cmd, step.expect)) {
        appendLine('demo: try "' + step.suggest + '" (or click Run step)', "warn");
        inputEl.value = "";
        setGhost(step.suggest);
        return;
      }
      var shown = force ? step.suggest : cmd.trim();
      echoCommand(shown);
      inputEl.value = "";
      setGhost("");
      step
        .run()
        .then(function () {
          if (state.mode === "guest") {
            renderSteps();
            return;
          }
          state.step++;
          if (state.step >= steps.length) {
            finish();
            return;
          }
          renderSteps();
          hintEl.textContent = steps[state.step].hint;
          setGhost(steps[state.step].suggest);
          nextBtn.textContent = "Run step →";
        })
        .catch(function (err) {
          state.busy = false;
          inputEl.disabled = false;
          console.error(err);
          appendLine("demo error: " + (err && err.message ? err.message : String(err)), "warn");
        });
    }

    function runGuest(cmd) {
      if (state.busy) return;
      var raw = cmd.trim();
      echoCommand(raw || "");
      inputEl.value = "";
      setGhost(currentSuggest());
      if (!raw) return;

      var key = normalizeCmd(raw);
      var resp = guestCmds[key];
      if (!resp) {
        var first = key.split(" ")[0];
        if (first === "echo") {
          resp = raw.replace(/^echo\s+/i, "").replace(/^['"]|['"]$/g, "");
        } else {
          resp =
            "bash: " +
            raw.split(/\s+/)[0] +
            ": command not found (demo)\ntry: uname -a, ls, help, exit";
        }
      }

      if (resp === "__clear__") {
        outputEl.innerHTML = "";
        return;
      }
      if (resp === "__exit__") {
        state.busy = true;
        typeLines([
          { text: "logout", cls: "dim", delay: 200 },
          { text: "Connection to sbox-1 closed.", cls: "dim", delay: 250 },
        ]).then(function () {
          finish();
        });
        return;
      }

      state.busy = true;
      inputEl.disabled = true;
      var lines = String(resp).split("\n");
      var i = 0;
      function next() {
        if (i >= lines.length) {
          state.busy = false;
          inputEl.disabled = false;
          focusDemoInput(inputEl);
          return;
        }
        var t = lines[i++];
        sleep(40).then(function () {
          appendLine(t, "out");
          next();
        });
      }
      next();
    }

    function finish() {
      state.mode = "done";
      state.busy = false;
      setPrompt("$", false);
      setTitle("~ — grain — demo complete");
      setGhost("");
      inputLine.hidden = true;
      hintEl.innerHTML =
        'Demo complete. <a href="#install">Install grain</a> or read the <a href="/get-started/first-sandbox/">full tutorial</a>.';
      nextBtn.textContent = "Replay";
      skipBtn.hidden = true;
      renderSteps();
      appendLine("\u00a0");
      appendLine("✓ You're ready for a real sandbox on your machine.", "ok");
    }

    function setTitle(t) {
      if (titleEl) titleEl.textContent = t;
    }

    function setPrompt(p, guest) {
      promptEl.textContent = p;
      promptEl.classList.toggle("is-guest", !!guest);
      requestAnimationFrame(function () {
        if (!inputLine) return;
        var w = promptEl.getBoundingClientRect().width;
        inputLine.style.setProperty("--ghost-pad", Math.ceil(w + 8) + "px");
      });
    }

    function setGhost(text) {
      if (!ghostEl) return;
      if (inputEl.value) {
        ghostEl.textContent = "";
        return;
      }
      ghostEl.textContent = text || "";
    }

    function restart() {
      state.step = 0;
      state.mode = "host";
      state.busy = false;
      outputEl.innerHTML = "";
      inputLine.hidden = false;
      inputEl.disabled = false;
      inputEl.value = "";
      setPrompt("$", false);
      setTitle("~ — grain — interactive demo");
      nextBtn.textContent = "Run step →";
      skipBtn.hidden = false;
      hintEl.textContent = steps[0].hint;
      setGhost(steps[0].suggest);
      appendLine("Last login: demo session on ttys000", "dim");
      appendLine("grain interactive demo — simulated terminal", "dim");
      appendLine("\u00a0");
      renderSteps();
      focusDemoInput(inputEl);
    }

    function onNextClick(e) {
      if (e) e.preventDefault();
      if (state.busy) return;
      if (state.mode === "done") {
        restart();
        return;
      }
      if (state.mode === "guest") {
        runGuest("exit");
        return;
      }
      runHostStep(true);
    }

    function onSkipClick(e) {
      if (e) e.preventDefault();
      if (state.busy || state.mode !== "host") return;
      runHostStep(true);
    }

    nextBtn.addEventListener("click", onNextClick);
    skipBtn.addEventListener("click", onSkipClick);

    if (restartBtn) {
      restartBtn.addEventListener("click", function (e) {
        e.preventDefault();
        if (!state.busy) restart();
      });
    }

    inputEl.addEventListener("input", function () {
      setGhost(inputEl.value ? "" : currentSuggest());
    });

    inputEl.addEventListener("keydown", function (e) {
      if (e.key === "Tab" && !inputEl.value && currentSuggest()) {
        e.preventDefault();
        inputEl.value = currentSuggest();
        setGhost("");
        return;
      }
      if (e.key !== "Enter" || state.busy) return;
      e.preventDefault();
      if (state.mode === "host") runHostStep(false);
      else if (state.mode === "guest") runGuest(inputEl.value);
    });

    term.addEventListener("click", function () {
      focusDemoInput(inputEl);
    });

    restart();
  }

  /** Focus without re-scrolling the page (avoids hiding the demo under the sticky header). */
  function focusDemoInput(inputEl) {
    if (!inputEl) return;
    try {
      inputEl.focus({ preventScroll: true });
    } catch (e) {
      try {
        inputEl.focus();
      } catch (e2) {}
    }
  }

  /**
   * Scroll so the demo section sits just below the sticky header.
   * Prefer #demo-section (title + terminal); fall back to #demo / [data-sandbox-demo].
   */
  function scrollToDemo(smooth) {
    var el =
      document.getElementById("demo-section") ||
      document.getElementById("demo") ||
      document.querySelector("[data-sandbox-demo]");
    if (!el) return;
    var header = document.querySelector(".site-header");
    var offset = (header ? header.offsetHeight : 64) + 16;
    var top = el.getBoundingClientRect().top + window.pageYOffset - offset;
    window.scrollTo({
      top: Math.max(0, top),
      behavior: smooth === false ? "auto" : "smooth",
    });
  }

  function boot() {
    var nodes = document.querySelectorAll("[data-sandbox-demo]");
    for (var i = 0; i < nodes.length; i++) {
      initDemo(nodes[i]);
    }

    // Hero / in-page links: custom scroll (native #hash ignores sticky header).
    document.addEventListener("click", function (e) {
      var a = e.target.closest && e.target.closest('a[href="#demo"], a[href="#demo-section"], [data-scroll-demo]');
      if (!a) return;
      var href = a.getAttribute("href") || "#demo-section";
      if (href.charAt(0) !== "#" && !a.hasAttribute("data-scroll-demo")) return;
      e.preventDefault();
      var hash = href.charAt(0) === "#" ? href : "#demo-section";
      if (history.replaceState) {
        history.replaceState(null, "", hash);
      } else {
        location.hash = hash;
      }
      scrollToDemo(true);
      var demoRoot = document.querySelector("[data-sandbox-demo]");
      var input = demoRoot && demoRoot.querySelector("[data-demo-input]");
      setTimeout(function () {
        focusDemoInput(input);
      }, 400);
    });

    if (location.hash === "#demo" || location.hash === "#demo-section") {
      setTimeout(function () {
        scrollToDemo(true);
      }, 60);
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot);
  } else {
    boot();
  }
})();