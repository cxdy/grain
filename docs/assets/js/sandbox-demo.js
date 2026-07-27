(function () {
  var root = document.querySelector("[data-sandbox-demo]");
  if (!root) return;

  var outputEl = root.querySelector("[data-demo-output]");
  var inputEl = root.querySelector("[data-demo-input]");
  var promptEl = root.querySelector("[data-demo-prompt]");
  var inputLine = root.querySelector("[data-demo-input-line]");
  var stepsEl = root.querySelector("[data-demo-steps]");
  var hintEl = root.querySelector("[data-demo-hint]");
  var nextBtn = root.querySelector("[data-demo-next]");
  var skipBtn = root.querySelector("[data-demo-skip]");
  var restartBtn = root.querySelector("[data-demo-restart]");
  var term = root.querySelector("[data-demo-term]");

  var steps = [
    {
      id: "install",
      title: "Install grain",
      hint: "Type the install command, or press Run step →",
      prompt: "$",
      expect: ["curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash", "curl | bash", "install.sh"],
      suggest: "curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash",
      run: function (ctx) {
        return typeLines(ctx, [
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
      prompt: "$",
      expect: ["grain up"],
      suggest: "grain up",
      run: function (ctx) {
        return typeLines(ctx, [
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
      prompt: "$",
      expect: ["grain image pull grain-ubuntu", "grain image pull", "image pull"],
      suggest: "grain image pull grain-ubuntu",
      run: function (ctx) {
        return typeLines(ctx, [
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
      prompt: "$",
      expect: ["grain new", "new"],
      suggest: "grain new",
      run: function (ctx) {
        return typeLines(ctx, [
          { text: "  ⠹ creating  image         0s", cls: "dim", delay: 200 },
          { text: "  ⠸ creating  disk          1s", cls: "dim", delay: 350 },
          { text: "  ⠼ creating  boot          2s", cls: "dim", delay: 400 },
          { text: "  ⠴ creating  waiting agent 4s", cls: "dim", delay: 500 },
          { text: "created sbox-1  status=running  image=grain-ubuntu  persist=false  ssh=:2201  (6s)", cls: "ok", delay: 280 },
          { text: "next:  grain sh sbox-1", cls: "out", delay: 160 },
          { text: "       grain x sbox-1 -- uname -a", cls: "dim", delay: 120 },
        ]);
      },
    },
    {
      id: "sh",
      title: "Open a shell",
      hint: "Connect with grain sh — agent PTY by default",
      prompt: "$",
      expect: ["grain sh", "grain sh sbox-1", "sh"],
      suggest: "grain sh",
      run: function (ctx) {
        return typeLines(ctx, [
          { text: "connecting via agent to sbox-1 …", cls: "dim", delay: 300 },
          { text: "", delay: 100 },
          { text: "Welcome to Ubuntu 24.04 LTS (grain sandbox)", cls: "out", delay: 200 },
          { text: "  agent 0.2.0 · ephemeral · sbox-1", cls: "dim", delay: 160 },
        ]).then(function () {
          ctx.mode = "guest";
          setPrompt("ubuntu@sbox-1:~$");
          hintEl.textContent =
            "You're in the VM. Try uname -a, ls, or exit when done.";
          nextBtn.textContent = "Finish demo";
        });
      },
    },
  ];

  var guestCmds = {
    "uname -a":
      "Linux sbox-1 6.8.0-grain #1 SMP PREEMPT_DYNAMIC aarch64 GNU/Linux",
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
    mode: "host", // host | guest | done
    busy: false,
  };

  function sleep(ms) {
    return new Promise(function (r) {
      setTimeout(r, ms);
    });
  }

  function scrollTerm() {
    var wrap = root.querySelector(".demo-term");
    if (wrap) wrap.scrollTop = wrap.scrollHeight;
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

  function typeLines(ctx, items) {
    state.busy = true;
    inputEl.disabled = true;
    var i = 0;
    function next() {
      if (i >= items.length) {
        state.busy = false;
        inputEl.disabled = false;
        inputEl.focus();
        return Promise.resolve();
      }
      var item = items[i++];
      return sleep(item.delay || 120).then(function () {
        if (item.text === "" && !item.cls) {
          appendLine("\u00a0");
        } else if (item.multiline) {
          item.text.split("\n").forEach(function (t) {
            appendLine(t, item.cls);
          });
        } else {
          appendLine(item.text, item.cls);
        }
        return next();
      });
    }
    return next();
  }

  function setPrompt(p) {
    promptEl.textContent = p;
  }

  function renderSteps() {
    stepsEl.innerHTML = steps
      .map(function (s, i) {
        var cls =
          i < state.step ? "done" : i === state.step ? "current" : "todo";
        if (state.mode === "guest" || state.mode === "done") {
          cls = i <= state.step ? "done" : "todo";
          if (state.mode === "guest" && i === steps.length - 1) cls = "current";
        }
        return (
          '<li class="' +
          cls +
          '"><span class="demo-step-idx">' +
          (i + 1) +
          "</span>" +
          escapeHtml(s.title) +
          "</li>"
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
      return n === e2 || n.indexOf(e2) !== -1 || e2.indexOf(n) !== -1 && n.length > 3;
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
      appendLine("demo: try “" + step.suggest + "” (or click Run step)", "warn");
      inputEl.value = "";
      return;
    }
    var shown = force ? step.suggest : cmd.trim();
    echoCommand(shown);
    inputEl.value = "";
    step.run({}).then(function () {
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
      inputEl.placeholder = steps[state.step].suggest;
      nextBtn.textContent = "Run step →";
    });
  }

  function runGuest(cmd) {
    if (state.busy) return;
    var raw = cmd.trim();
    echoCommand(raw || "");
    inputEl.value = "";
    if (!raw) return;

    var key = normalizeCmd(raw);
    var resp = guestCmds[key];
    if (!resp) {
      // try first word
      var first = key.split(" ")[0];
      if (first === "echo") {
        resp = raw.replace(/^echo\s+/i, "").replace(/^['"]|['"]$/g, "");
      } else {
        resp = "bash: " + raw.split(/\s+/)[0] + ": command not found (demo)\ntry: uname -a, ls, help, exit";
      }
    }

    if (resp === "__clear__") {
      outputEl.innerHTML = "";
      return;
    }
    if (resp === "__exit__") {
      state.busy = true;
      typeLines({}, [
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
        inputEl.focus();
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
    setPrompt("$");
    inputLine.hidden = true;
    hintEl.innerHTML =
      'Demo complete. <a href="#install">Install grain</a> or read the <a href="' +
      (document.body.getAttribute("data-first-sandbox") || "/get-started/first-sandbox/") +
      '">full tutorial</a>.';
    nextBtn.textContent = "Replay";
    nextBtn.onclick = function () {
      restart();
    };
    skipBtn.hidden = true;
    renderSteps();
    appendLine("", "dim");
    appendLine("✓ You’re ready for a real sandbox on your machine.", "ok");
  }

  function restart() {
    state.step = 0;
    state.mode = "host";
    state.busy = false;
    outputEl.innerHTML = "";
    inputLine.hidden = false;
    inputEl.disabled = false;
    inputEl.value = "";
    setPrompt("$");
    nextBtn.textContent = "Run step →";
    nextBtn.onclick = function () {
      runHostStep(true);
    };
    skipBtn.hidden = false;
    hintEl.textContent = steps[0].hint;
    inputEl.placeholder = steps[0].suggest;
    appendLine("grain interactive demo — simulated terminal", "dim");
    appendLine("A grain of sand: small Linux, real workflow.", "dim");
    appendLine("", "dim");
    renderSteps();
    inputEl.focus();
  }

  inputEl.addEventListener("keydown", function (e) {
    if (e.key !== "Enter" || state.busy) return;
    e.preventDefault();
    if (state.mode === "host") runHostStep(false);
    else if (state.mode === "guest") runGuest(inputEl.value);
  });

  nextBtn.addEventListener("click", function () {
    if (state.mode === "done") {
      restart();
      return;
    }
    if (state.mode === "guest") {
      runGuest("exit");
      return;
    }
    runHostStep(true);
  });

  skipBtn.addEventListener("click", function () {
    if (state.busy || state.mode !== "host") return;
    runHostStep(true);
  });

  if (restartBtn) {
    restartBtn.addEventListener("click", function () {
      if (!state.busy) restart();
    });
  }

  term.addEventListener("click", function () {
    inputEl.focus();
  });

  // Deep link #demo
  if (location.hash === "#demo") {
    setTimeout(function () {
      root.scrollIntoView({ behavior: "smooth", block: "start" });
    }, 100);
  }

  restart();
})();
