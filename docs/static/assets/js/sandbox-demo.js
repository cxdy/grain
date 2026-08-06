(function () {
  function commonPrelude() {
    return [
      {
        id: "up",
        title: "Start the daemon",
        hint: "Start the local control plane with grain up",
        expect: ["grain up"],
        suggest: "grain up",
        run: function (ctx) {
          return ctx.typeLines([
            { text: "grain up  pid=4242", cls: "out", delay: 250 },
            { text: "  socket  ~/.grain/grain.sock", cls: "dim", delay: 180 },
            { text: "  api     http://127.0.0.1:7474", cls: "dim", delay: 150 },
          ]);
        },
      },
      {
        id: "pull",
        title: "Pull a base image",
        hint: "Download the golden image once (simulated)",
        expect: ["grain image pull grain-ubuntu", "grain image pull", "image pull"],
        suggest: "grain image pull grain-ubuntu",
        run: function (ctx) {
          return ctx.typeLines([
            { text: "pulling grain-ubuntu …", cls: "out", delay: 200 },
            { text: "  64 / 320 MB", cls: "dim", delay: 260 },
            { text: "  192 / 320 MB", cls: "dim", delay: 260 },
            { text: "  320 / 320 MB", cls: "dim", delay: 240 },
            { text: "ok grain-ubuntu in 4s  (sha256 verified)", cls: "ok", delay: 220 },
            { text: "ssh user: ubuntu · agent: baked-in", cls: "dim", delay: 150 },
          ]);
        },
      },
    ];
  }

  var scenarios = {
    shell: {
      id: "shell",
      label: "Shell",
      title: "~ — grain — first sandbox",
      blurb: "Install → create → shell in a microVM",
      doneHtml:
        'Demo complete. <a href="#install">Install grain</a> · <a href="/docs/' +
        (typeof document !== "undefined" &&
        document.querySelector("[data-docs-version]")
          ? document.querySelector("[data-docs-version]").getAttribute("data-docs-version")
          : "main") +
        '/get-started/quickstart/">Quick start</a>',
      guest: true,
      steps: function () {
        return [
          {
            id: "install",
            title: "Install grain",
            hint: "Type the install command, or press Run step →",
            expect: [
              "curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash",
              "curl | bash",
              "install.sh",
            ],
            suggest:
              "curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "→ detecting os=darwin arch=arm64", cls: "dim", delay: 200 },
                { text: "→ downloading grain_darwin_arm64 …", cls: "dim", delay: 400 },
                { text: "✓ installed /usr/local/bin/grain", cls: "ok", delay: 350 },
                { text: "✓ guest agent → ~/.grain/agent/", cls: "ok", delay: 200 },
                { text: "", delay: 80 },
                {
                  text: "grain " + (ctx.docsVersion || "dev"),
                  cls: "out",
                  delay: 150,
                },
              ]);
            },
          },
        ]
          .concat(commonPrelude())
          .concat([
            {
              id: "new",
              title: "Create a sandbox",
              hint: "Launch an ephemeral VM (grain new)",
              expect: ["grain new", "new"],
              suggest: "grain new",
              run: function (ctx) {
                return ctx.typeLines([
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
                ]);
              },
            },
            {
              id: "sh",
              title: "Open a shell",
              hint: "Connect with grain sh — agent PTY by default",
              expect: ["grain sh", "grain sh sbox-1", "sh"],
              suggest: "grain sh",
              run: function (ctx) {
                return ctx
                  .typeLines([
                    { text: "connecting via agent to sbox-1 …", cls: "dim", delay: 300 },
                    { text: "", delay: 100 },
                    { text: "Welcome to Ubuntu 24.04 LTS (grain sandbox)", cls: "out", delay: 200 },
                    { text: "  agent 0.2.0 · ephemeral · sbox-1", cls: "dim", delay: 160 },
                  ])
                  .then(function () {
                    ctx.enterGuest({
                      name: "sbox-1",
                      prompt: "ubuntu@sbox-1:~$",
                      title: "ubuntu@sbox-1: ~",
                      hint: "You're in the VM. Try uname -a, ls, or exit when done.",
                    });
                  });
              },
            },
          ]);
      },
    },

    act: {
      id: "act",
      label: "act",
      title: "~ — grain act — GitHub Actions",
      blurb: "Run workflows in an isolated microVM",
      doneHtml:
        'Demo complete. <a href="/guides/recipes/act/">act recipe</a> · <a href="#install">Install</a>',
      guest: false,
      steps: function () {
        return commonPrelude().concat([
          {
            id: "cd",
            title: "Enter your repo",
            hint: "act runs from a project with .github/workflows",
            expect: ["cd ", "cd my-app", "cd ./my-app"],
            suggest: "cd ~/src/my-app",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "", delay: 80 },
                { text: "# .github/workflows/ci.yml present", cls: "dim", delay: 180 },
              ]);
            },
          },
          {
            id: "list",
            title: "List workflows",
            hint: "grain act -- -l lists jobs (args after -- go to act)",
            expect: ["grain act -- -l", "grain act -l", "act -- -l"],
            suggest: "grain act -- -l",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "grain act  creating ephemeral sandbox (preset act) …", cls: "dim", delay: 280 },
                { text: "  ⠹ boot + docker + act   8s", cls: "dim", delay: 450 },
                { text: "  mounting $PWD → /work", cls: "dim", delay: 220 },
                { text: "", delay: 80 },
                { text: "Stage  Job ID  Job name  Workflow name  Workflow file  Events", cls: "out", delay: 200 },
                { text: "0      test    test      CI             ci.yml         push", cls: "out", delay: 160 },
                { text: "0      lint    lint      CI             ci.yml         push", cls: "out", delay: 140 },
                { text: "", delay: 80 },
                { text: "sandbox deleted (ephemeral)", cls: "dim", delay: 180 },
              ]);
            },
          },
          {
            id: "run",
            title: "Run a job",
            hint: "Run one job in a fresh microVM",
            expect: ["grain act -- -j test", "grain act -j test", "-j test"],
            suggest: "grain act -- -j test",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "grain act  sandbox act-7f3a  (docker + act)", cls: "dim", delay: 260 },
                { text: "[act/test] ⭐ Run Main actions/checkout@v4", cls: "out", delay: 320 },
                { text: "[act/test]   ✅  Success - Main actions/checkout@v4", cls: "ok", delay: 280 },
                { text: "[act/test] ⭐ Run Main npm test", cls: "out", delay: 300 },
                { text: "[act/test]   ✅  Success - Main npm test", cls: "ok", delay: 320 },
                { text: "", delay: 80 },
                { text: "Job succeeded.", cls: "ok", delay: 200 },
                { text: "sandbox deleted — host Docker untouched", cls: "dim", delay: 220 },
              ]);
            },
          },
        ]);
      },
    },

    k3s: {
      id: "k3s",
      label: "k3s",
      title: "~ — grain — k3s lab",
      blurb: "Single-node Kubernetes in a microVM",
      doneHtml:
        'Demo complete. <a href="/guides/recipes/k3s/">k3s recipe</a> · <a href="#install">Install</a>',
      guest: false,
      steps: function () {
        return commonPrelude().concat([
          {
            id: "new-k3s",
            title: "Create with k3s preset",
            hint: "Persistent disk + wait for userdata (k3s install)",
            expect: [
              "grain new --preset k3s -n lab -p --wait userdata",
              "grain new --preset k3s",
              "--preset k3s",
            ],
            suggest: "grain new --preset k3s -n lab -p --wait userdata",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "  ⠹ creating  image           0s", cls: "dim", delay: 200 },
                { text: "  ⠙ creating  disk (20G)      2s", cls: "dim", delay: 320 },
                { text: "  ⠹ creating  boot            5s", cls: "dim", delay: 360 },
                { text: "  ⠸ creating  waiting userdata  45s", cls: "dim", delay: 500 },
                {
                  text: "created lab  status=running  image=grain-ubuntu  persist=true  ssh=:2204  tcp=:6443→6443",
                  cls: "ok",
                  delay: 300,
                },
                { text: "preset=k3s  cpus=2  memory=4096MiB", cls: "dim", delay: 160 },
              ]);
            },
          },
          {
            id: "fwd",
            title: "Check API forward",
            hint: "See the published Kubernetes API port",
            expect: ["grain fwd ls lab", "grain fwd ls", "fwd ls"],
            suggest: "grain fwd ls lab",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "NAME  HOST  GUEST  PROTO", cls: "out", delay: 160 },
                { text: "ssh   2204  22     tcp", cls: "out", delay: 120 },
                { text: "api   6443  6443   tcp   ← k3s", cls: "ok", delay: 180 },
              ]);
            },
          },
          {
            id: "kubectl",
            title: "Talk to the cluster",
            hint: "After copying kubeconfig (see recipe), use kubectl on the host",
            expect: ["kubectl get nodes", "kubectl get node", "get nodes"],
            suggest: "kubectl get nodes",
            run: function (ctx) {
              return ctx.typeLines([
                { text: "NAME   STATUS   ROLES                  AGE   VERSION", cls: "out", delay: 200 },
                { text: "lab    Ready    control-plane,master   2m    v1.30.2+k3s1", cls: "ok", delay: 240 },
                { text: "", delay: 80 },
                { text: "# disposable lab: grain rm lab when done", cls: "dim", delay: 200 },
              ]);
            },
          },
        ]);
      },
    },
  };

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
    var scenarioEl = root.querySelector("[data-demo-scenarios]");
    var blurbEl = root.querySelector("[data-demo-blurb]");

    if (!outputEl || !inputEl || !promptEl || !stepsEl || !hintEl || !nextBtn || !skipBtn || !term) {
      console.warn("grain demo: missing DOM nodes", root);
      return;
    }

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
      scenarioId: "shell",
      steps: [],
      scenario: null,
      step: 0,
      mode: "host",
      busy: false,
    };

    var ctx = {
      typeLines: typeLines,
      enterGuest: enterGuest,
      // From data-docs-version on the demo root (Hugo site params) — never hardcode.
      docsVersion: root.getAttribute("data-docs-version") || "",
    };

    function sleep(ms) {
      return new Promise(function (r) {
        setTimeout(r, ms);
      });
    }

    // Scroll the output pane only (fixed-height terminal, like a real TTY).
    function scrollTerm() {
      if (!outputEl) return;
      // Double rAF so layout includes the just-appended line.
      requestAnimationFrame(function () {
        requestAnimationFrame(function () {
          outputEl.scrollTop = outputEl.scrollHeight;
        });
      });
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

    function enterGuest(opts) {
      state.mode = "guest";
      setPrompt(opts.prompt, true);
      setTitle(opts.title);
      setGhost("uname -a");
      hintEl.textContent = opts.hint;
      nextBtn.textContent = "Finish demo";
      renderSteps();
    }

    function currentSuggest() {
      if (state.mode === "guest") return "uname -a";
      if (state.mode === "done") return "";
      var step = state.steps[state.step];
      return step ? step.suggest : "";
    }

    function renderScenarioTabs() {
      if (!scenarioEl) return;
      scenarioEl.innerHTML = Object.keys(scenarios)
        .map(function (id) {
          var s = scenarios[id];
          var on = id === state.scenarioId ? " active" : "";
          return (
            '<button type="button" class="demo-scenario-tab' +
            on +
            '" data-demo-scenario="' +
            id +
            '" aria-pressed="' +
            (id === state.scenarioId ? "true" : "false") +
            '">' +
            escapeHtml(s.label) +
            "</button>"
          );
        })
        .join("");
    }

    function renderSteps() {
      stepsEl.innerHTML = state.steps
        .map(function (s, i) {
          var cls = i < state.step ? "done" : i === state.step ? "current" : "todo";
          if (state.mode === "guest") {
            cls = i < state.steps.length - 1 ? "done" : "current";
          } else if (state.mode === "done") {
            cls = "done";
          }
          return (
            '<li class="' +
            cls +
            '"><span class="demo-step-idx">' +
            (i + 1) +
            ")</span> <span class=\"demo-step-label\">" +
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
      var step = state.steps[state.step];
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
      Promise.resolve(step.run(ctx))
        .then(function () {
          if (state.mode === "guest") {
            renderSteps();
            return;
          }
          state.step++;
          if (state.step >= state.steps.length) {
            finish();
            return;
          }
          renderSteps();
          hintEl.textContent = state.steps[state.step].hint;
          setGhost(state.steps[state.step].suggest);
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
      setTitle((state.scenario && state.scenario.title) || "~ — grain — demo complete");
      setGhost("");
      inputLine.hidden = true;
      hintEl.innerHTML =
        (state.scenario && state.scenario.doneHtml) ||
        'Demo complete. <a href="#install">Install grain</a> or read the <a href="/docs/' +
          (ctx.docsVersion || "main") +
          '/get-started/quickstart/">quick start</a>.';
      nextBtn.textContent = "Replay";
      skipBtn.hidden = true;
      renderSteps();
      appendLine("\u00a0");
      appendLine("✓ Ready for a real run on your machine.", "ok");
    }

    function loadScenario(id) {
      var sc = scenarios[id] || scenarios.shell;
      state.scenarioId = sc.id;
      state.scenario = sc;
      state.steps = sc.steps();
      state.step = 0;
      state.mode = "host";
      state.busy = false;
      outputEl.innerHTML = "";
      inputLine.hidden = false;
      inputEl.disabled = false;
      inputEl.value = "";
      setPrompt("$", false);
      setTitle(sc.title);
      nextBtn.textContent = "Run step →";
      skipBtn.hidden = false;
      if (blurbEl) blurbEl.textContent = sc.blurb;
      hintEl.textContent = state.steps[0].hint;
      setGhost(state.steps[0].suggest);
      appendLine("Last login: demo session on ttys000", "dim");
      appendLine("grain interactive demo — simulated · scenario: " + sc.label, "dim");
      appendLine("\u00a0");
      renderScenarioTabs();
      renderSteps();
      focusDemoInput(inputEl);
    }

    function restart() {
      loadScenario(state.scenarioId);
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

    if (scenarioEl) {
      scenarioEl.addEventListener("click", function (e) {
        var btn = e.target.closest && e.target.closest("[data-demo-scenario]");
        if (!btn || state.busy) return;
        var id = btn.getAttribute("data-demo-scenario");
        if (id && id !== state.scenarioId) loadScenario(id);
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

    // Deep-link: #demo-act / #demo-k3s / #demo-shell
    var initial = "shell";
    var hash = (location.hash || "").replace(/^#/, "");
    if (hash === "demo-act" || hash === "workloads") {
      /* workloads section is separate; demo defaults shell unless act/k3s hash */
    }
    if (hash === "demo-act") initial = "act";
    if (hash === "demo-k3s") initial = "k3s";
    if (hash === "demo-shell" || hash === "demo" || hash === "demo-section") initial = "shell";

    loadScenario(initial);
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

    document.addEventListener("click", function (e) {
      var a =
        e.target.closest &&
        e.target.closest(
          'a[href="#demo"], a[href="#demo-section"], a[href="#demo-act"], a[href="#demo-k3s"], a[href="#demo-shell"], [data-scroll-demo]'
        );
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

      var demoRoot = document.querySelector("[data-sandbox-demo]");
      var scenarioId = null;
      if (hash === "#demo-act") scenarioId = "act";
      if (hash === "#demo-k3s") scenarioId = "k3s";
      if (hash === "#demo-shell") scenarioId = "shell";
      if (scenarioId && demoRoot) {
        var tab = demoRoot.querySelector('[data-demo-scenario="' + scenarioId + '"]');
        if (tab) tab.click();
      }

      scrollToDemo(true);
      var input = demoRoot && demoRoot.querySelector("[data-demo-input]");
      setTimeout(function () {
        focusDemoInput(input);
      }, 400);
    });

    if (
      location.hash === "#demo" ||
      location.hash === "#demo-section" ||
      location.hash === "#demo-act" ||
      location.hash === "#demo-k3s" ||
      location.hash === "#demo-shell"
    ) {
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
