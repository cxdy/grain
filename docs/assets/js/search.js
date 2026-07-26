(function () {
  var input = document.getElementById("site-search-input");
  var panel = document.getElementById("site-search-panel");
  var resultsEl = document.getElementById("site-search-results");
  var openBtn = document.getElementById("site-search-open");
  var backdrop = document.getElementById("site-search-backdrop");
  if (!input || !panel || !resultsEl) return;

  var docs = [];
  var ready = false;
  var active = -1;

  function openSearch() {
    panel.hidden = false;
    if (backdrop) backdrop.hidden = false;
    document.body.classList.add("search-open");
    input.focus();
    input.select();
    if (!ready) loadIndex();
  }

  function closeSearch() {
    panel.hidden = true;
    if (backdrop) backdrop.hidden = true;
    document.body.classList.remove("search-open");
    active = -1;
  }

  function loadIndex() {
    var url = document.body.getAttribute("data-search-index") || "/search-data.json";
    fetch(url)
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        docs = Array.isArray(data) ? data : [];
        ready = true;
        if (input.value.trim()) render(search(input.value));
      })
      .catch(function () {
        resultsEl.innerHTML = '<p class="search-empty">Search index unavailable.</p>';
      });
  }

  function tokenize(q) {
    return q
      .toLowerCase()
      .split(/[^a-z0-9+#.\/-]+/i)
      .filter(function (t) {
        return t.length > 1;
      });
  }

  function scoreDoc(doc, tokens) {
    var title = (doc.title || "").toLowerCase();
    var desc = (doc.description || "").toLowerCase();
    var content = (doc.content || "").toLowerCase();
    var section = (doc.section || "").toLowerCase();
    var score = 0;
    for (var i = 0; i < tokens.length; i++) {
      var t = tokens[i];
      if (title === t) score += 50;
      if (title.indexOf(t) !== -1) score += 20;
      if (section.indexOf(t) !== -1) score += 8;
      if (desc.indexOf(t) !== -1) score += 10;
      if (content.indexOf(t) !== -1) score += 3;
    }
    return score;
  }

  function search(q) {
    var tokens = tokenize(q);
    if (!tokens.length) return [];
    return docs
      .map(function (d) {
        return { doc: d, score: scoreDoc(d, tokens) };
      })
      .filter(function (x) {
        return x.score > 0;
      })
      .sort(function (a, b) {
        return b.score - a.score;
      })
      .slice(0, 12);
  }

  function highlight(text, q) {
    if (!text) return "";
    var tokens = tokenize(q);
    var out = text;
    tokens.forEach(function (t) {
      try {
        out = out.replace(new RegExp("(" + t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "ig"), "<mark>$1</mark>");
      } catch (e) {}
    });
    return out;
  }

  function render(hits) {
    active = -1;
    if (!input.value.trim()) {
      resultsEl.innerHTML = '<p class="search-hint">Search guides, reference, and SDKs…</p>';
      return;
    }
    if (!ready) {
      resultsEl.innerHTML = '<p class="search-hint">Loading index…</p>';
      return;
    }
    if (!hits.length) {
      resultsEl.innerHTML = '<p class="search-empty">No matches for “' + escapeHtml(input.value.trim()) + '”.</p>';
      return;
    }
    resultsEl.innerHTML = hits
      .map(function (h, i) {
        var d = h.doc;
        var sec = d.section ? '<span class="search-section">' + escapeHtml(d.section) + "</span>" : "";
        var desc = d.description || d.content || "";
        if (desc.length > 140) desc = desc.slice(0, 137) + "…";
        return (
          '<a class="search-hit" role="option" data-idx="' +
          i +
          '" href="' +
          escapeAttr(d.url) +
          '">' +
          sec +
          '<span class="search-hit-title">' +
          highlight(escapeHtml(d.title), input.value) +
          "</span>" +
          '<span class="search-hit-desc">' +
          highlight(escapeHtml(desc), input.value) +
          "</span></a>"
        );
      })
      .join("");
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }
  function escapeAttr(s) {
    return escapeHtml(s).replace(/'/g, "&#39;");
  }

  function moveActive(delta) {
    var hits = resultsEl.querySelectorAll(".search-hit");
    if (!hits.length) return;
    active = (active + delta + hits.length) % hits.length;
    hits.forEach(function (el, i) {
      el.classList.toggle("active", i === active);
    });
    hits[active].scrollIntoView({ block: "nearest" });
  }

  if (openBtn) openBtn.addEventListener("click", openSearch);
  if (backdrop) backdrop.addEventListener("click", closeSearch);

  document.addEventListener("keydown", function (e) {
    var metaK = (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k";
    if (metaK) {
      e.preventDefault();
      if (panel.hidden) openSearch();
      else closeSearch();
      return;
    }
    if (e.key === "Escape" && !panel.hidden) {
      e.preventDefault();
      closeSearch();
      return;
    }
    if (panel.hidden) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      moveActive(1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      moveActive(-1);
    } else if (e.key === "Enter" && active >= 0) {
      var hit = resultsEl.querySelector('.search-hit[data-idx="' + active + '"]');
      if (hit) window.location.href = hit.getAttribute("href");
    }
  });

  input.addEventListener("input", function () {
    render(search(input.value));
  });

  // Prefetch index on idle
  if ("requestIdleCallback" in window) {
    requestIdleCallback(function () {
      if (!ready) loadIndex();
    });
  }
})();
