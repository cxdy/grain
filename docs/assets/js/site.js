(function () {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  function current() {
    return root.getAttribute("data-theme") || "dark";
  }
  function setTheme(t) {
    root.setAttribute("data-theme", t);
    try {
      localStorage.setItem("grain-theme", t);
    } catch (e) {}
  }
  if (btn) {
    btn.addEventListener("click", function () {
      setTheme(current() === "dark" ? "light" : "dark");
    });
  }

  // Install OS tabs (+ keep homepage Copy button on the visible panel)
  document.querySelectorAll("[data-tabs]").forEach(function (tabs) {
    var buttons = tabs.querySelectorAll("[data-tab]");
    var panels = tabs.querySelectorAll("[data-panel]");
    var copyBtn = tabs.querySelector("[data-copy]");
    function activate(id) {
      buttons.forEach(function (b) {
        var on = b.getAttribute("data-tab") === id;
        b.classList.toggle("active", on);
        b.setAttribute("aria-selected", on ? "true" : "false");
      });
      panels.forEach(function (p) {
        p.hidden = p.getAttribute("data-panel") !== id;
      });
      if (copyBtn) {
        var map = {
          macos: "#install-cmd-macos",
          linux: "#install-cmd-linux",
          source: "#install-cmd-source",
        };
        if (map[id]) copyBtn.setAttribute("data-copy", map[id]);
      }
    }
    buttons.forEach(function (b) {
      b.addEventListener("click", function () {
        activate(b.getAttribute("data-tab"));
      });
    });
    // Prefer host OS
    var ua = navigator.platform || "";
    var prefer = /Mac|iPhone|iPad/i.test(ua) ? "macos" : "linux";
    if (tabs.querySelector('[data-tab="' + prefer + '"]')) activate(prefer);
    else if (buttons[0]) activate(buttons[0].getAttribute("data-tab"));
  });

  // Copy buttons — prefer data-copy-text on the target (exact pasteable commands)
  document.querySelectorAll("[data-copy]").forEach(function (el) {
    el.addEventListener("click", function () {
      var sel = el.getAttribute("data-copy");
      var node = document.querySelector(sel);
      if (!node) return;
      var text = node.getAttribute("data-copy-text");
      if (!text) {
        // Fallback: strip shell prompts from displayed lines
        text = (node.innerText || node.textContent || "")
          .split("\n")
          .map(function (line) {
            return line.replace(/^\s*\$\s?/, "");
          })
          .join("\n");
      }
      navigator.clipboard.writeText(text.replace(/\n+$/, "")).then(function () {
        var prev = el.textContent;
        el.textContent = "Copied";
        setTimeout(function () {
          el.textContent = prev;
        }, 1400);
      });
    });
  });
})();
