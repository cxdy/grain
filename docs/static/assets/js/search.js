(function () {
  var input = document.querySelector("[data-search-input]");
  var resultsEl = document.querySelector("[data-search-results]");
  if (!input || !resultsEl) return;

  var docs = [];
  var ready = false;
  var active = -1;

  function loadIndex() {
    fetch("/search-index.json")
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        docs = Array.isArray(data) ? data : [];
        ready = true;
        if (input.value.trim()) render(search(input.value));
      })
      .catch(function () {
        resultsEl.hidden = false;
        resultsEl.innerHTML = '<p class="search-empty">Search index unavailable.</p>';
      });
  }

  function tokenize(q) {
    return q
      .toLowerCase()
      .split(/[^a-z0-9+#.\/_-]+/i)
      .filter(function (t) {
        return t.length > 1;
      });
  }

  function scoreDoc(doc, tokens) {
    var title = (doc.title || "").toLowerCase();
    var desc = (doc.description || "").toLowerCase();
    var section = (doc.section || "").toLowerCase();
    var keywords = (doc.keywords || "").toLowerCase();
    var content = (doc.content || "").toLowerCase();
    var url = (doc.url || "").toLowerCase();
    var score = 0;
    for (var i = 0; i < tokens.length; i++) {
      var t = tokens[i];
      if (title === t) score += 60;
      else if (title.indexOf(t) !== -1) score += 28;
      if (keywords.indexOf(t) !== -1) score += 22;
      if (desc.indexOf(t) !== -1) score += 14;
      if (section.indexOf(t) !== -1) score += 8;
      if (url.indexOf(t) !== -1) score += 6;
      if (content.indexOf(t) !== -1) score += 2;
      // Prefer get-started / short guides slightly for "how do I" queries
      if (url.indexOf("/get-started/") !== -1 && score > 0) score += 4;
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

  function sectionLabel(s) {
    if (!s) return "";
    var map = {
      "get-started": "Learn",
      guides: "Do",
      explain: "Understand",
      reference: "Look up",
      mcp: "MCP",
      developer: "Developer",
    };
    return map[s] || s;
  }

  function render(hits) {
    active = -1;
    if (!input.value.trim()) {
      resultsEl.hidden = true;
      resultsEl.innerHTML = "";
      return;
    }
    resultsEl.hidden = false;
    if (!hits.length) {
      resultsEl.innerHTML =
        '<p class="search-empty">No results. Try install, MCP, bootstrap, act, k3s…</p>';
      return;
    }
    resultsEl.innerHTML = hits
      .map(function (h, i) {
        return (
          '<a href="' +
          h.doc.url +
          '" data-idx="' +
          i +
          '"><div class="s-meta"></div><div class="s-title"></div><div class="s-desc"></div></a>'
        );
      })
      .join("");
    var links = resultsEl.querySelectorAll("a");
    hits.forEach(function (h, i) {
      links[i].querySelector(".s-meta").textContent = sectionLabel(h.doc.section);
      links[i].querySelector(".s-title").textContent = h.doc.title || h.doc.url;
      links[i].querySelector(".s-desc").textContent =
        h.doc.description || h.doc.url;
    });
  }

  input.addEventListener("focus", function () {
    if (!ready) loadIndex();
  });
  input.addEventListener("input", function () {
    if (!ready) loadIndex();
    else render(search(input.value));
  });
  input.addEventListener("keydown", function (e) {
    var links = resultsEl.querySelectorAll("a");
    if (e.key === "Escape") {
      resultsEl.hidden = true;
      input.blur();
      return;
    }
    if (!links.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active = Math.min(active + 1, links.length - 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active = Math.max(active - 1, 0);
    } else if (e.key === "Enter" && active >= 0) {
      e.preventDefault();
      links[active].click();
      return;
    } else {
      return;
    }
    links.forEach(function (a, i) {
      a.classList.toggle("is-active", i === active);
    });
  });
  document.addEventListener("click", function (e) {
    if (!e.target.closest("[data-site-search]")) {
      resultsEl.hidden = true;
    }
  });
})();
