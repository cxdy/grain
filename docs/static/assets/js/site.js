(function () {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  function current() {
    return root.getAttribute("data-theme") || "light";
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

  // Mobile primary nav
  var navToggle = document.getElementById("nav-toggle");
  var navMain = document.getElementById("nav-main");
  if (navToggle && navMain) {
    navToggle.addEventListener("click", function () {
      var open = navToggle.getAttribute("aria-expanded") === "true";
      navToggle.setAttribute("aria-expanded", open ? "false" : "true");
      navMain.classList.toggle("is-open", !open);
    });
  }

  // Version switcher — navigate on-site between /docs/<ver>/… (materialized at build).
  // GitHub commit link is a separate control in the version switcher partial.
  var verSelect = document.querySelector("[data-version-select]");
  if (verSelect) {
    verSelect.addEventListener("change", function () {
      var targetRoot = verSelect.value; // e.g. /docs/0.8.0/ or /docs/main/
      if (!targetRoot) return;
      if (/^https?:\/\//i.test(targetRoot)) {
        // Legacy external entries (if any) — open source, don't leave broken.
        window.open(targetRoot, "_blank", "noopener,noreferrer");
        return;
      }
      if (targetRoot.charAt(targetRoot.length - 1) !== "/") targetRoot += "/";
      var path = window.location.pathname;
      var m = path.match(/^\/docs\/[^/]+\/(.*)$/);
      if (m) {
        window.location.href = targetRoot + m[1];
      } else {
        window.location.href = targetRoot;
      }
    });
  }

  // Install OS tabs
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
    var ua = navigator.platform || "";
    var prefer = /Mac|iPhone|iPad/i.test(ua) ? "macos" : "linux";
    if (tabs.querySelector('[data-tab="' + prefer + '"]')) activate(prefer);
    else if (buttons[0]) activate(buttons[0].getAttribute("data-tab"));
  });

  // Copy buttons (install card + any [data-copy] targets)
  document.querySelectorAll("[data-copy]").forEach(function (el) {
    el.addEventListener("click", function () {
      var sel = el.getAttribute("data-copy");
      var node = document.querySelector(sel);
      if (!node) return;
      var text = node.getAttribute("data-copy-text");
      if (!text) {
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

  // Copy buttons on highlighted code windows
  document.querySelectorAll("[data-code-copy]").forEach(function (el) {
    el.addEventListener("click", function () {
      var win = el.closest(".code-window");
      if (!win) return;
      var pre = win.querySelector("pre");
      if (!pre) return;
      var text = pre.innerText || pre.textContent || "";
      navigator.clipboard.writeText(text.replace(/\n+$/, "")).then(function () {
        var prev = el.textContent;
        el.textContent = "Copied";
        setTimeout(function () {
          el.textContent = prev;
        }, 1400);
      });
    });
  });

  // Mobile docs nav
  var docsNavToggle = document.getElementById("docs-nav-toggle");
  var navInner = document.getElementById("docs-sidebar-inner");
  if (docsNavToggle && navInner) {
    docsNavToggle.addEventListener("click", function () {
      var open = docsNavToggle.getAttribute("aria-expanded") === "true";
      docsNavToggle.setAttribute("aria-expanded", open ? "false" : "true");
      navInner.classList.toggle("is-open", !open);
    });
    var currentLink = navInner.querySelector("a[aria-current='page'], a.active");
    var label = docsNavToggle.querySelector("[data-docs-nav-label]");
    if (currentLink && label) {
      label.textContent = currentLink.textContent.trim();
    }
  }

  // On-this-page TOC
  var article = document.getElementById("docs-article");
  var tocHost = document.getElementById("docs-toc");
  if (article && tocHost) {
    var content = article.querySelector(".docs-content") || article;
    var headings = content.querySelectorAll("h2");
    if (headings.length >= 2) {
      var labelEl = document.createElement("p");
      labelEl.className = "docs-toc-label";
      labelEl.textContent = "On this page";
      tocHost.appendChild(labelEl);
      var ol = document.createElement("ol");
      headings.forEach(function (h, i) {
        if (!h.id) {
          var slug = (h.textContent || "section-" + i)
            .toLowerCase()
            .replace(/[^\w\s-]/g, "")
            .trim()
            .replace(/\s+/g, "-");
          h.id = slug || "section-" + i;
        }
        var li = document.createElement("li");
        var a = document.createElement("a");
        a.href = "#" + h.id;
        a.textContent = h.textContent;
        li.appendChild(a);
        ol.appendChild(li);
      });
      tocHost.appendChild(ol);
    }
  }
})();
