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

  // Mobile docs nav
  var navToggle = document.getElementById("docs-nav-toggle");
  var navInner = document.getElementById("docs-sidebar-inner");
  if (navToggle && navInner) {
    navToggle.addEventListener("click", function () {
      var open = navToggle.getAttribute("aria-expanded") === "true";
      navToggle.setAttribute("aria-expanded", open ? "false" : "true");
      navInner.classList.toggle("is-open", !open);
    });

    var current = navInner.querySelector("a[aria-current='page'], a.active");
    var label = navToggle.querySelector("[data-docs-nav-label]");
    if (current && label) {
      label.textContent = current.textContent.trim();
    }
  }

  // On-this-page TOC for docs with 3+ h2s
  var article = document.getElementById("docs-article");
  if (article) {
    var headings = article.querySelectorAll("h2");
    if (headings.length >= 3) {
      var toc = document.createElement("nav");
      toc.className = "docs-toc";
      toc.setAttribute("aria-label", "On this page");
      var labelEl = document.createElement("p");
      labelEl.className = "docs-toc-label";
      labelEl.textContent = "On this page";
      toc.appendChild(labelEl);
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
      toc.appendChild(ol);
      var anchor = article.querySelector(".lead") || article.querySelector("h1");
      if (anchor && anchor.parentNode === article) {
        anchor.insertAdjacentElement("afterend", toc);
      } else {
        article.insertBefore(toc, article.firstChild);
      }
    }
  }
})();
