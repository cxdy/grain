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

  // Install OS tabs
  document.querySelectorAll("[data-tabs]").forEach(function (tabs) {
    var buttons = tabs.querySelectorAll("[data-tab]");
    var panels = tabs.querySelectorAll("[data-panel]");
    function activate(id) {
      buttons.forEach(function (b) {
        var on = b.getAttribute("data-tab") === id;
        b.classList.toggle("active", on);
        b.setAttribute("aria-selected", on ? "true" : "false");
      });
      panels.forEach(function (p) {
        p.hidden = p.getAttribute("data-panel") !== id;
      });
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

  // Copy buttons
  document.querySelectorAll("[data-copy]").forEach(function (el) {
    el.addEventListener("click", function () {
      var sel = el.getAttribute("data-copy");
      var node = document.querySelector(sel);
      if (!node) return;
      var text = node.innerText || node.textContent;
      navigator.clipboard.writeText(text.trim()).then(function () {
        var prev = el.textContent;
        el.textContent = "Copied";
        setTimeout(function () {
          el.textContent = prev;
        }, 1400);
      });
    });
  });
})();
