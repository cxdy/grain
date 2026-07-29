/**
 * grain API explorer — custom renderer for OpenAPI 3 (not Swagger UI).
 * UX cues: operation list, multi-language samples, JSON response blocks.
 * Visual language stays grain (mint/paper), not a third-party docs clone.
 */
(function () {
  "use strict";

  var root = document.getElementById("api-explorer");
  if (!root) return;

  var specUrl = root.getAttribute("data-spec") || "/assets/openapi.json";
  var ENV_DEFAULTS = {
    host: "127.0.0.1",
    port: "7474",
    token: "test-api-token",
    socket: "~/.grain/grain.sock",
    /** "tcp" | "unix" */
    transport: "tcp",
  };
  var state = {
    spec: null,
    lang: loadLang(),
    /** "sdk" = official grain clients (default); "raw" = requests / fetch / net/http */
    flavor: loadFlavor(),
    env: loadEnv(),
    filter: "",
    selectedId: null,
  };

  function loadLang() {
    try {
      var lang = localStorage.getItem("grain-api-sample-lang") || "curl";
      if (lang === "node") return "typescript";
      return lang;
    } catch (e) {
      return "curl";
    }
  }
  function saveLang(lang) {
    try {
      localStorage.setItem("grain-api-sample-lang", lang);
    } catch (e) {}
  }
  function loadFlavor() {
    try {
      var f = localStorage.getItem("grain-api-sample-flavor");
      return f === "raw" ? "raw" : "sdk";
    } catch (e) {
      return "sdk";
    }
  }
  function saveFlavor(flavor) {
    try {
      localStorage.setItem("grain-api-sample-flavor", flavor);
    } catch (e) {}
  }
  function loadEnv() {
    var env = {
      host: ENV_DEFAULTS.host,
      port: ENV_DEFAULTS.port,
      token: ENV_DEFAULTS.token,
      socket: ENV_DEFAULTS.socket,
      transport: ENV_DEFAULTS.transport,
    };
    try {
      var raw = localStorage.getItem("grain-api-sample-env");
      if (!raw) return env;
      var parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object") {
        if (parsed.host) env.host = String(parsed.host);
        if (parsed.port) env.port = String(parsed.port);
        if (parsed.token != null) env.token = String(parsed.token);
        if (parsed.socket) env.socket = String(parsed.socket);
        if (parsed.transport === "unix" || parsed.transport === "tcp") {
          env.transport = parsed.transport;
        }
      }
    } catch (e) {}
    return env;
  }
  function saveEnv() {
    try {
      localStorage.setItem("grain-api-sample-env", JSON.stringify(state.env));
    } catch (e) {}
  }
  function env() {
    return {
      host: (state.env.host || ENV_DEFAULTS.host).trim() || ENV_DEFAULTS.host,
      port: String(state.env.port || ENV_DEFAULTS.port).trim() || ENV_DEFAULTS.port,
      token: state.env.token != null ? String(state.env.token) : ENV_DEFAULTS.token,
      socket: (state.env.socket || ENV_DEFAULTS.socket).trim() || ENV_DEFAULTS.socket,
      transport: state.env.transport === "unix" ? "unix" : "tcp",
    };
  }
  /** TCP base used in portable samples (also when transport is unix, for non-curl SDKs that need a host). */
  function tcpBaseURL() {
    var e = env();
    return "http://" + e.host + ":" + e.port;
  }
  function preferUnix() {
    return env().transport === "unix";
  }
  function sampleToken() {
    return env().token;
  }
  function tokenLit() {
    return JSON.stringify(sampleToken());
  }
  function bearerHeader() {
    return "Bearer " + sampleToken();
  }
  function socketPath() {
    return env().socket;
  }

  function escapeHtml(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  /** Map sample tabs → Prism language ids */
  function prismLang(lang) {
    if (lang === "curl" || lang === "bash" || lang === "shell") return "bash";
    if (lang === "python") return "python";
    if (lang === "go") return "go";
    if (lang === "typescript" || lang === "ts" || lang === "node" || lang === "javascript" || lang === "js")
      return "typescript";
    if (lang === "json") return "json";
    return "markup";
  }

  function codeBlock(lang, source) {
    var pl = prismLang(lang);
    return (
      '<pre class="api-code language-' +
      pl +
      '" data-lang="' +
      escapeHtml(lang) +
      '"><code class="language-' +
      pl +
      '">' +
      escapeHtml(source) +
      "</code></pre>"
    );
  }

  function highlightCode() {
    if (!window.Prism || !root) return;
    try {
      window.Prism.highlightAllUnder(root);
    } catch (e) {}
  }

  function resolveRef(ref) {
    if (!ref || !state.spec) return null;
    var parts = ref.replace(/^#\//, "").split("/");
    var cur = state.spec;
    for (var i = 0; i < parts.length; i++) {
      if (!cur) return null;
      cur = cur[parts[i]];
    }
    return cur;
  }

  function deref(obj) {
    if (!obj) return obj;
    if (obj.$ref) return deref(resolveRef(obj.$ref)) || obj;
    return obj;
  }

  /** Build a simple example value from a schema (examples preferred). */
  function exampleFromSchema(schema, depth) {
    depth = depth || 0;
    schema = deref(schema);
    if (!schema || depth > 6) return null;
    if (schema.example !== undefined) return schema.example;
    if (schema.default !== undefined) return schema.default;
    if (schema.enum && schema.enum.length) return schema.enum[0];

    var t = schema.type;
    if (Array.isArray(t)) t = t[0];
    if (schema.properties || t === "object") {
      var out = {};
      var props = schema.properties || {};
      Object.keys(props).forEach(function (k) {
        var v = exampleFromSchema(props[k], depth + 1);
        if (v !== null && v !== undefined) out[k] = v;
      });
      if (schema.additionalProperties && typeof schema.additionalProperties === "object") {
        var av = exampleFromSchema(schema.additionalProperties, depth + 1);
        if (av !== null) out["key"] = av;
      }
      return out;
    }
    if (t === "array" || schema.items) {
      var item = exampleFromSchema(schema.items || {}, depth + 1);
      return item === null ? [] : [item];
    }
    if (t === "integer" || t === "number") return 0;
    if (t === "boolean") return false;
    if (t === "string") {
      if (schema.format === "date-time") return "2026-01-01T00:00:00Z";
      if (schema.format === "binary") return "<binary>";
      return "string";
    }
    return null;
  }

  function responseExample(resp) {
    resp = deref(resp);
    if (!resp || !resp.content) return null;
    var json = resp.content["application/json"];
    if (json) {
      if (json.example !== undefined) return { media: "application/json", value: json.example };
      if (json.examples) {
        var keys = Object.keys(json.examples);
        if (keys.length) {
          var ex = deref(json.examples[keys[0]]);
          if (ex && ex.value !== undefined) return { media: "application/json", value: ex.value };
        }
      }
      if (json.schema) {
        var built = exampleFromSchema(json.schema);
        if (built !== null) return { media: "application/json", value: built };
      }
    }
    // first content type
    var ctypes = Object.keys(resp.content);
    if (!ctypes.length) return null;
    var c0 = resp.content[ctypes[0]];
    if (c0.example !== undefined) return { media: ctypes[0], value: c0.example };
    if (c0.schema) {
      var b2 = exampleFromSchema(c0.schema);
      if (b2 !== null) return { media: ctypes[0], value: b2 };
    }
    return null;
  }

  function pathParams(pathItem, op) {
    var list = [];
    function add(arr) {
      (arr || []).forEach(function (p) {
        p = deref(p);
        if (p) list.push(p);
      });
    }
    add(pathItem.parameters);
    add(op.parameters);
    return list;
  }

  function fillPath(path, params) {
    var out = path;
    (params || []).forEach(function (p) {
      if (p.in !== "path") return;
      var schema = deref(p.schema) || {};
      var sample =
        p.example !== undefined
          ? p.example
          : schema.example !== undefined
            ? schema.example
            : p.name === "name"
              ? "sbox-1"
              : p.name === "hostPort"
                ? "8080"
                : "value";
      out = out.replace("{" + p.name + "}", encodeURIComponent(String(sample)));
    });
    return out;
  }

  function needsAuth(op) {
    if (op.security && op.security.length === 0) return false;
    if (op.security) return true;
    return !!(state.spec.security && state.spec.security.length);
  }

  function sampleCurl(method, url, op, bodyExample, useUnix) {
    var pathOnly = url.replace(/^https?:\/\/[^/]+/, "") || "/";
    var sock = socketPath();
    var auth = needsAuth(op);
    var lines = [];
    if (useUnix) {
      lines.push("curl -sS --unix-socket " + sock + " \\");
      lines.push("  -X " + method.toUpperCase() + " 'http://grain" + pathOnly + "' \\");
    } else {
      lines.push("curl -sS -X " + method.toUpperCase() + " \\");
      lines.push("  '" + url + "' \\");
    }
    if (auth) {
      lines.push("  -H 'Authorization: " + bearerHeader() + "' \\");
    }
    if (bodyExample !== null && bodyExample !== undefined && method !== "get" && method !== "delete") {
      lines.push("  -H 'Content-Type: application/json' \\");
      lines.push("  -d '" + JSON.stringify(bodyExample).replace(/'/g, "'\\''") + "'");
    } else {
      var last = lines[lines.length - 1];
      if (last.endsWith(" \\")) lines[lines.length - 1] = last.slice(0, -2);
    }
    // When TCP is selected, still show a short unix alternate as a comment block.
    if (!useUnix) {
      lines.push("");
      lines.push("# Same call over the CLI unix socket:");
      lines.push(
        "curl -sS --unix-socket " +
          sock +
          " -X " +
          method.toUpperCase() +
          " 'http://grain" +
          pathOnly +
          "'" +
          (auth ? " -H 'Authorization: " + bearerHeader() + "'" : "")
      );
    }
    return lines.join("\n");
  }

  /** Extract sample path params (name, hostPort, …) for SDK snippets. */
  function samplePathArgs(path, params) {
    var args = {};
    (params || []).forEach(function (p) {
      if (p.in !== "path") return;
      var schema = deref(p.schema) || {};
      args[p.name] =
        p.example !== undefined
          ? p.example
          : schema.example !== undefined
            ? schema.example
            : p.name === "name"
              ? "sbox-1"
              : p.name === "hostPort"
                ? 8080
                : p.name === "guestPort"
                  ? 80
                  : "value";
    });
    // Also pull from path template when parameters list is incomplete
    var m;
    var re = /\{([^}]+)\}/g;
    while ((m = re.exec(path))) {
      if (args[m[1]] === undefined) {
        args[m[1]] = m[1] === "name" ? "sbox-1" : m[1] === "hostPort" ? 8080 : "value";
      }
    }
    return args;
  }

  function pyQuote(s) {
    return JSON.stringify(String(s));
  }
  function goQuote(s) {
    return JSON.stringify(String(s));
  }

  /**
   * Map OpenAPI method+path → official grain SDK calls.
   * Prefer typed clients over raw HTTP for Python / TypeScript / Go.
   */
  function sdkCall(method, path, op, bodyExample, preferUnix) {
    var m = method.toLowerCase();
    var p = path;
    var auth = needsAuth(op);
    var name = "sbox-1";
    var hostPort = 8080;
    var guestPort = 80;
    var pathArgs = samplePathArgs(p, op.parameters || []);
    if (pathArgs.name != null) name = String(pathArgs.name);
    if (pathArgs.hostPort != null) hostPort = Number(pathArgs.hostPort) || 8080;
    if (pathArgs.guestPort != null) guestPort = Number(pathArgs.guestPort) || 80;

    // Python / TS / Go call bodies (single expression or short block after client setup)
    var py = null;
    var ts = null;
    var go = null;

    function match(methodNeed, pathRe) {
      return m === methodNeed && pathRe.test(p);
    }

    if (match("get", /^\/healthz$/)) {
      py = "grain.health()";
      ts = "await grain.health();";
      go = "err = c.Health(ctx)";
    } else if (match("get", /^\/info$/)) {
      py = "info = grain.info()\nprint(info)";
      ts = "const info = await grain.info();\nconsole.log(info);";
      go = "info, err := c.Info(ctx)\n_ = info";
    } else if (match("get", /^\/vms$/)) {
      py = "vms = grain.list()\nprint(vms)";
      ts = "const vms = await grain.list();\nconsole.log(vms);";
      go = "list, err := c.List(ctx)\n_ = list";
    } else if (match("post", /^\/vms$/) || match("post", /^\/vms\?/)) {
      py =
        "from grain import CreateRequest\n\n" +
        "inst = grain.create(CreateRequest(name=" +
        pyQuote(name) +
        ", persistent=False))\nprint(inst.name)";
      ts =
        "const inst = await grain.create({ name: " +
        JSON.stringify(name) +
        ", persistent: false });\nconsole.log(inst.name);";
      go =
        "inst, err := c.Create(ctx, client.CreateRequest{Name: " +
        goQuote(name) +
        ", Persistent: false})\n_ = inst";
    } else if (match("get", /^\/vms\/\{name\}$/)) {
      py = "inst = grain.get(" + pyQuote(name) + ")\nprint(inst)";
      ts = "const inst = await grain.get(" + JSON.stringify(name) + ");\nconsole.log(inst);";
      go = "inst, err := c.Get(ctx, " + goQuote(name) + ")\n_ = inst";
    } else if (match("delete", /^\/vms\/\{name\}$/)) {
      py = "grain.delete(" + pyQuote(name) + ")";
      ts = "await grain.delete(" + JSON.stringify(name) + ");";
      go = "err = c.Delete(ctx, " + goQuote(name) + ")";
    } else if (match("post", /^\/vms\/\{name\}\/start$/)) {
      py = "inst = grain.start(" + pyQuote(name) + ")\nprint(inst.status)";
      ts = "const inst = await grain.start(" + JSON.stringify(name) + ");\nconsole.log(inst.status);";
      go = "inst, err := c.Start(ctx, " + goQuote(name) + ")\n_ = inst";
    } else if (match("post", /^\/vms\/\{name\}\/shutdown$/)) {
      py = "grain.shutdown(" + pyQuote(name) + ")  # or grain.stop(...)";
      ts = "await grain.shutdown(" + JSON.stringify(name) + "); // or grain.stop(...)";
      go = "err = c.Shutdown(ctx, " + goQuote(name) + ") // or c.Stop";
    } else if (match("post", /^\/vms\/\{name\}\/pause$/)) {
      py = "grain.pause(" + pyQuote(name) + ")";
      ts = "await grain.pause(" + JSON.stringify(name) + ");";
      go = "err = c.Pause(ctx, " + goQuote(name) + ")";
    } else if (match("post", /^\/vms\/\{name\}\/resume$/)) {
      py = "grain.resume(" + pyQuote(name) + ")";
      ts = "await grain.resume(" + JSON.stringify(name) + ");";
      go = "err = c.Resume(ctx, " + goQuote(name) + ")";
    } else if (match("post", /^\/vms\/\{name\}\/suspend$/)) {
      py = "grain.suspend(" + pyQuote(name) + ")";
      ts = "await grain.suspend(" + JSON.stringify(name) + ");";
      go = "err = c.Suspend(ctx, " + goQuote(name) + ")";
    } else if (match("post", /^\/vms\/\{name\}\/restore$/)) {
      py = "inst = grain.restore(" + pyQuote(name) + ")\nprint(inst.status)";
      ts = "const inst = await grain.restore(" + JSON.stringify(name) + ");\nconsole.log(inst.status);";
      go = "inst, err := c.Restore(ctx, " + goQuote(name) + ")\n_ = inst";
    } else if (match("post", /^\/vms\/\{name\}\/forwards$/)) {
      py =
        "fwd = grain.add_forward(" +
        pyQuote(name) +
        ", " +
        hostPort +
        ", " +
        guestPort +
        ")\nprint(fwd)";
      ts =
        "const fwd = await grain.addForward(" +
        JSON.stringify(name) +
        ", " +
        hostPort +
        ", " +
        guestPort +
        ");\nconsole.log(fwd);";
      go =
        "fwd, err := c.AddForward(ctx, " +
        goQuote(name) +
        ", " +
        hostPort +
        ", " +
        guestPort +
        ")\n_ = fwd";
    } else if (match("delete", /^\/vms\/\{name\}\/forwards\/\{hostPort\}$/)) {
      py = "grain.remove_forward(" + pyQuote(name) + ", " + hostPort + ")";
      ts = "await grain.removeForward(" + JSON.stringify(name) + ", " + hostPort + ");";
      go = "err = c.RemoveForward(ctx, " + goQuote(name) + ", " + hostPort + ")";
    } else if (match("get", /^\/vms\/\{name\}\/agent\/health$/) || match("get", /agent.*health/)) {
      py = "h = grain.agent_health(" + pyQuote(name) + ")\nprint(h)";
      ts = "const h = await grain.agentHealth(" + JSON.stringify(name) + ");\nconsole.log(h);";
      go = "h, err := c.AgentHealth(ctx, " + goQuote(name) + ")\n_ = h";
    } else if (match("get", /^\/vms\/\{name\}\/stats$/)) {
      py = "st = grain.stats(" + pyQuote(name) + ")\nprint(st)";
      ts = "const st = await grain.stats(" + JSON.stringify(name) + ");\nconsole.log(st);";
      go = "st, err := c.Stats(ctx, " + goQuote(name) + ")\n_ = st";
    } else if (match("post", /^\/vms\/\{name\}\/exec/)) {
      py =
        "res = grain.exec(" +
        pyQuote(name) +
        ', "uname", "-a")\nprint(res.stdout, res.exit_code)';
      ts =
        "const res = await grain.exec(" +
        JSON.stringify(name) +
        ', "uname", "-a");\nconsole.log(res.stdout, res.exitCode);';
      go =
        "res, err := c.Exec(ctx, " +
        goQuote(name) +
        ', "uname", "-a")\n_ = res';
    } else if (match("get", /^\/secrets$/)) {
      py = "secrets = grain.list_secrets()\nprint(secrets)  # method name may vary by SDK version";
      ts = "// Secrets helpers land in the TS SDK as the surface grows — curl tab shows the HTTP form.";
      go = "list, err := c.ListSecrets(ctx)\n_ = list";
    } else if (match("get", /^\/vms\/\{name\}\/fs\/readdir/) || match("get", /\/fs\/readdir/)) {
      py = 'entries = grain.readdir(' + pyQuote(name) + ', "/")\nprint(entries)';
      ts = 'const entries = await grain.readdir?.(' + JSON.stringify(name) + ', "/");';
      go = 'entries, err := c.ReadDir(ctx, ' + goQuote(name) + ', "/")\n_ = entries';
    } else {
      // Fallback: honest note + curl-equivalent path (prefer SDK when a helper exists)
      var filledPath = p.replace(/\{name\}/g, name).replace(/\{hostPort\}/g, String(hostPort));
      py =
        "# No first-class helper mapped for " +
        m.toUpperCase() +
        " " +
        p +
        " — see HTTP API notes or curl tab.\n" +
        "print(" +
        pyQuote(m.toUpperCase() + " " + filledPath) +
        ")";
      ts =
        "// No first-class helper mapped for " +
        m.toUpperCase() +
        " " +
        p +
        " — see the curl tab for the raw HTTP form.";
      go =
        "// No first-class helper mapped for " +
        m.toUpperCase() +
        " " +
        p +
        " — see the curl tab.\n_ = c";
    }

    return { py: py, ts: ts, go: go, auth: auth, preferUnix: preferUnix };
  }

  function samplePythonSDK(method, path, op, bodyExample, useUnix) {
    var call = sdkCall(method, path, op, bodyExample, useUnix);
    var tok = sampleToken();
    var lines = [
      "from grain import GrainClient",
      "",
      useUnix
        ? "grain = GrainClient.unix(" +
          JSON.stringify(socketPath()) +
          (call.auth ? ", token=" + tokenLit() : "") +
          ")"
        : "grain = GrainClient.http(" +
          JSON.stringify(tcpBaseURL()) +
          (call.auth ? ", token=" + tokenLit() : "") +
          ")",
      "",
      call.py,
    ];
    return lines.join("\n");
  }

  function samplePythonRaw(method, url, op, bodyExample) {
    var headers = {};
    if (needsAuth(op)) headers["Authorization"] = bearerHeader();
    var lines = [
      "import requests",
      "",
      "url = " + JSON.stringify(url),
      "headers = " + JSON.stringify(headers, null, 4),
    ];
    if (bodyExample !== null && bodyExample !== undefined && method !== "get" && method !== "delete") {
      lines.push("payload = " + JSON.stringify(bodyExample, null, 4));
      lines.push(
        "r = requests.request(" +
          JSON.stringify(method.toUpperCase()) +
          ", url, headers=headers, json=payload)"
      );
    } else {
      lines.push(
        "r = requests.request(" + JSON.stringify(method.toUpperCase()) + ", url, headers=headers)"
      );
    }
    lines.push("print(r.status_code)");
    lines.push("print(r.text)");
    return lines.join("\n");
  }

  function sampleTypeScriptSDK(method, path, op, bodyExample, useUnix) {
    var call = sdkCall(method, path, op, bodyExample, useUnix);
    var lines = [
      'import { GrainClient } from "@cxdy/grain";',
      "",
      useUnix
        ? "const grain = new GrainClient({" +
          '\n  baseURL: "http://grain",' +
          "\n  socketPath: " +
          JSON.stringify(socketPath()) +
          "," +
          (call.auth ? "\n  token: " + tokenLit() + "," : "") +
          "\n});"
        : "const grain = new GrainClient({" +
          "\n  baseURL: " +
          JSON.stringify(tcpBaseURL()) +
          "," +
          (call.auth ? "\n  token: " + tokenLit() + "," : "") +
          "\n});",
      "",
      call.ts,
    ];
    return lines.join("\n");
  }

  function sampleTypeScriptRaw(method, url, op, bodyExample) {
    var headers = { Accept: "application/json" };
    if (needsAuth(op)) headers["Authorization"] = bearerHeader();
    var hasBody = bodyExample !== null && bodyExample !== undefined && method !== "get" && method !== "delete";
    if (hasBody) headers["Content-Type"] = "application/json";
    var lines = [
      "const url = " + JSON.stringify(url) + ";",
      "const res = await fetch(url, {",
      "  method: " + JSON.stringify(method.toUpperCase()) + ",",
      "  headers: " + JSON.stringify(headers, null, 4).replace(/\n/g, "\n  ") + ",",
    ];
    if (hasBody) {
      lines.push(
        "  body: JSON.stringify(" + JSON.stringify(bodyExample, null, 4).replace(/\n/g, "\n  ") + "),"
      );
    }
    lines.push("});");
    lines.push("console.log(res.status);");
    lines.push("console.log(await res.text());");
    return lines.join("\n");
  }

  function sampleGoSDK(method, path, op, bodyExample, useUnix) {
    var call = sdkCall(method, path, op, bodyExample, useUnix);
    var tok = tokenLit();
    var dial = useUnix
      ? "c, err := client.DialUnixToken(" +
        JSON.stringify(socketPath()) +
        ", " +
        (call.auth ? tok : `""`) +
        ")"
      : "c, err := client.DialHTTP(" +
        JSON.stringify(tcpBaseURL()) +
        ", " +
        (call.auth ? tok : `""`) +
        ")";
    var lines = [
      "package main",
      "",
      "import (",
      '\t"context"',
      '\t"fmt"',
      "",
      '\t"github.com/cxdy/grain/client"',
      ")",
      "",
      "func main() {",
      "\tctx := context.Background()",
      "\t" + dial,
      "\tif err != nil {",
      "\t\tpanic(err)",
      "\t}",
      "\t" + call.go.replace(/\n/g, "\n\t"),
      "\tif err != nil {",
      "\t\tpanic(err)",
      "\t}",
      '\tfmt.Println("ok")',
      "}",
    ];
    return lines.join("\n");
  }

  function sampleGoRaw(method, url, op, bodyExample) {
    var auth = needsAuth(op);
    var hasBody = bodyExample !== null && bodyExample !== undefined && method !== "get" && method !== "delete";
    var lines = [
      "package main",
      "",
      "import (",
      '\t"bytes"',
      '\t"fmt"',
      '\t"io"',
      '\t"net/http"',
      ")",
      "",
      "func main() {",
      "\turl := " + JSON.stringify(url),
    ];
    if (hasBody) {
      lines.push("\tbody := []byte(`" + JSON.stringify(bodyExample) + "`)");
      lines.push(
        "\treq, err := http.NewRequest(" +
          JSON.stringify(method.toUpperCase()) +
          ", url, bytes.NewReader(body))"
      );
    } else {
      lines.push(
        "\treq, err := http.NewRequest(" + JSON.stringify(method.toUpperCase()) + ", url, nil)"
      );
    }
    lines.push("\tif err != nil { panic(err) }");
    if (hasBody) lines.push('\treq.Header.Set("Content-Type", "application/json")');
    if (auth) lines.push("\treq.Header.Set(\"Authorization\", " + JSON.stringify(bearerHeader()) + ")");
    lines.push("\tres, err := http.DefaultClient.Do(req)");
    lines.push("\tif err != nil { panic(err) }");
    lines.push("\tdefer res.Body.Close()");
    lines.push("\tb, _ := io.ReadAll(res.Body)");
    lines.push("\tfmt.Println(res.StatusCode)");
    lines.push("\tfmt.Println(string(b))");
    lines.push("}");
    return lines.join("\n");
  }

  function requestBodyExample(op) {
    if (!op.requestBody) return null;
    var rb = deref(op.requestBody);
    if (!rb || !rb.content) return null;
    var json = rb.content["application/json"];
    if (!json) {
      var k = Object.keys(rb.content)[0];
      json = rb.content[k];
    }
    if (!json) return null;
    if (json.example !== undefined) return json.example;
    if (json.schema) return exampleFromSchema(json.schema);
    return null;
  }

  function collectOperations(spec) {
    var ops = [];
    var paths = spec.paths || {};
    Object.keys(paths).forEach(function (path) {
      var item = paths[path] || {};
      ["get", "post", "put", "patch", "delete", "head", "options"].forEach(function (method) {
        var op = item[method];
        if (!op) return;
        var tags = op.tags && op.tags.length ? op.tags : ["other"];
        var id = (op.operationId || method + "-" + path).replace(/[^\w-]+/g, "-");
        ops.push({
          id: id,
          method: method,
          path: path,
          pathItem: item,
          op: op,
          tags: tags,
          summary: op.summary || "",
          description: op.description || "",
        });
      });
    });
    return ops;
  }

  function groupByTag(ops) {
    var order = (state.spec.tags || []).map(function (t) {
      return t.name;
    });
    var groups = {};
    ops.forEach(function (o) {
      o.tags.forEach(function (tag) {
        if (!groups[tag]) groups[tag] = [];
        groups[tag].push(o);
      });
    });
    var keys = Object.keys(groups).sort(function (a, b) {
      var ia = order.indexOf(a);
      var ib = order.indexOf(b);
      if (ia === -1) ia = 999;
      if (ib === -1) ib = 999;
      if (ia !== ib) return ia - ib;
      return a.localeCompare(b);
    });
    return keys.map(function (k) {
      return { name: k, ops: groups[k] };
    });
  }

  function renderNav(groups) {
    var q = state.filter.trim().toLowerCase();
    var e = env();
    var html = '<nav class="api-nav" aria-label="API operations">';
    html += '<div class="api-nav-tools">';
    html +=
      '<label class="visually-hidden" for="api-filter">Filter operations</label>' +
      '<input type="search" id="api-filter" class="api-filter" placeholder="Filter paths…" value="' +
      escapeHtml(state.filter) +
      '" />';

    // Prefill env for copy-paste samples (persisted)
    html += '<div class="api-env" data-api-env>';
    html += '<p class="api-env-title">Sample environment</p>';
    html += '<p class="api-env-hint">Values are injected into the code samples below.</p>';
    html += '<div class="api-env-grid">';
    // Single-column stack in the rail — never clip side-by-side fields.
    // Host and port stay separate for clarity; token/transport full width.
    html +=
      '<label class="api-env-field"><span class="api-env-label">Host</span>' +
      '<input type="text" id="api-env-host" class="api-env-input" autocomplete="off" spellcheck="false" value="' +
      escapeHtml(e.host) +
      '" /></label>';
    html +=
      '<label class="api-env-field"><span class="api-env-label">Port</span>' +
      '<input type="text" id="api-env-port" class="api-env-input" inputmode="numeric" autocomplete="off" spellcheck="false" value="' +
      escapeHtml(e.port) +
      '" /></label>';
    html +=
      '<label class="api-env-field api-env-field-wide"><span class="api-env-label">API token</span>' +
      '<input type="text" id="api-env-token" class="api-env-input" autocomplete="off" spellcheck="false" value="' +
      escapeHtml(e.token) +
      '" /></label>';
    html +=
      '<label class="api-env-field api-env-field-wide"><span class="api-env-label">Transport</span>' +
      '<select id="api-env-transport" class="api-env-input api-env-select">' +
      '<option value="tcp"' +
      (e.transport === "tcp" ? " selected" : "") +
      ">TCP (HTTP)</option>" +
      '<option value="unix"' +
      (e.transport === "unix" ? " selected" : "") +
      ">Unix socket</option>" +
      "</select></label>";
    html +=
      '<label class="api-env-field api-env-field-wide' +
      (e.transport === "unix" ? "" : " is-hidden") +
      '" id="api-env-socket-wrap"><span class="api-env-label">Socket path</span>' +
      '<input type="text" id="api-env-socket" class="api-env-input" autocomplete="off" spellcheck="false" value="' +
      escapeHtml(e.socket) +
      '" /></label>';
    html += "</div>";
    html +=
      '<button type="button" class="api-env-reset" id="api-env-reset">Reset to defaults</button>';
    html += "</div>";

    groups.forEach(function (g) {
      var ops = g.ops.filter(function (o) {
        if (!q) return true;
        return (
          o.path.toLowerCase().indexOf(q) !== -1 ||
          o.summary.toLowerCase().indexOf(q) !== -1 ||
          o.method.indexOf(q) !== -1 ||
          g.name.toLowerCase().indexOf(q) !== -1
        );
      });
      if (!ops.length) return;
      html += '<div class="api-nav-group">';
      html += "<h3>" + escapeHtml(g.name) + "</h3><ul>";
      ops.forEach(function (o) {
        var active = o.id === state.selectedId ? " is-active" : "";
        html +=
          '<li><a class="api-nav-link' +
          active +
          '" href="#' +
          encodeURIComponent(o.id) +
          '" data-op-id="' +
          escapeHtml(o.id) +
          '">' +
          '<span class="api-method api-method-' +
          escapeHtml(o.method) +
          '">' +
          escapeHtml(o.method.toUpperCase()) +
          "</span>" +
          '<span class="api-nav-path">' +
          escapeHtml(o.path) +
          "</span></a></li>";
      });
      html += "</ul></div>";
    });
    html += "</nav>";
    return html;
  }

  function renderDetail(opEntry) {
    if (!opEntry) {
      return (
        '<div class="api-detail api-detail-empty">' +
        "<p>Select an operation from the list.</p>" +
        "<p class=\"api-muted\">Samples use the base URL you pick above. Prefer the unix socket for local CLI traffic.</p>" +
        "</div>"
      );
    }
    var op = opEntry.op;
    var params = pathParams(opEntry.pathItem, op);
    var filled = fillPath(opEntry.path, params);
    var useUnix = preferUnix();
    var base = tcpBaseURL();
    var url = base + filled;
    var bodyEx = requestBodyExample(op);
    var useSDK = state.flavor !== "raw";
    var samples = {
      curl: sampleCurl(opEntry.method, url, op, bodyEx, useUnix),
      go: useSDK
        ? sampleGoSDK(opEntry.method, opEntry.path, op, bodyEx, useUnix)
        : sampleGoRaw(opEntry.method, url, op, bodyEx),
      typescript: useSDK
        ? sampleTypeScriptSDK(opEntry.method, opEntry.path, op, bodyEx, useUnix)
        : sampleTypeScriptRaw(opEntry.method, url, op, bodyEx),
      python: useSDK
        ? samplePythonSDK(opEntry.method, opEntry.path, op, bodyEx, useUnix)
        : samplePythonRaw(opEntry.method, url, op, bodyEx),
    };

    var html = '<article class="api-detail" id="op-' + escapeHtml(opEntry.id) + '">';
    html += '<header class="api-detail-head">';
    html +=
      '<div class="api-detail-route">' +
      '<span class="api-method api-method-' +
      escapeHtml(opEntry.method) +
      '">' +
      escapeHtml(opEntry.method.toUpperCase()) +
      "</span>" +
      '<code class="api-detail-path">' +
      escapeHtml(opEntry.path) +
      "</code></div>";
    if (opEntry.summary) html += "<h2>" + escapeHtml(opEntry.summary) + "</h2>";
    if (opEntry.description) {
      html += '<p class="api-detail-desc">' + escapeHtml(opEntry.description) + "</p>";
    }
    html +=
      '<p class="api-auth-note">' +
      (needsAuth(op)
        ? "Auth: samples use <code>Authorization: Bearer " +
          escapeHtml(sampleToken()) +
          "</code> (edit Token in Sample environment)."
        : "Auth: not required for this operation.") +
      "</p>";
    html += "</header>";

    if (params.length) {
      html += '<section class="api-section"><h3>Parameters</h3>';
      html += '<table class="api-table"><thead><tr><th>Name</th><th>In</th><th>Type</th><th>Notes</th></tr></thead><tbody>';
      params.forEach(function (p) {
        var schema = deref(p.schema) || {};
        html +=
          "<tr><td><code>" +
          escapeHtml(p.name) +
          "</code>" +
          (p.required ? ' <span class="api-req">required</span>' : "") +
          "</td><td>" +
          escapeHtml(p.in || "") +
          "</td><td><code>" +
          escapeHtml(schema.type || "string") +
          "</code></td><td>" +
          escapeHtml(p.description || "") +
          "</td></tr>";
      });
      html += "</tbody></table></section>";
    }

    if (bodyEx !== null && bodyEx !== undefined) {
      html += '<section class="api-section"><h3>Request body</h3>';
      html += codeBlock("json", JSON.stringify(bodyEx, null, 2));
      html += "</section>";
    }

    html += '<section class="api-section"><h3>Request samples</h3>';
    html += '<div class="api-sample-toolbar">';
    html += '<div class="api-lang-tabs" role="tablist" aria-label="Sample language">';
    var langs = [
      { id: "curl", label: "curl" },
      { id: "go", label: "Go" },
      { id: "typescript", label: "TypeScript" },
      { id: "python", label: "Python" },
    ];
    if (state.lang === "node") state.lang = "typescript";
    langs.forEach(function (lang) {
      var on = state.lang === lang.id ? " is-active" : "";
      html +=
        '<button type="button" class="api-lang-tab' +
        on +
        '" role="tab" data-lang="' +
        lang.id +
        '" aria-selected="' +
        (state.lang === lang.id ? "true" : "false") +
        '">' +
        lang.label +
        "</button>";
    });
    html += "</div>";

    // Flavor: grain SDK (default) vs language-only (requests / fetch / net/http)
    var showFlavor = state.lang !== "curl";
    if (showFlavor) {
      var flavorLabels = {
        go: { sdk: "Go + grain SDK", raw: "Go (net/http)" },
        typescript: { sdk: "TypeScript + grain SDK", raw: "TypeScript (fetch)" },
        python: { sdk: "Python + grain SDK", raw: "Python (requests)" },
      };
      var fl = flavorLabels[state.lang] || { sdk: "grain SDK", raw: "Language only" };
      html +=
        '<label class="api-flavor">' +
        '<span class="visually-hidden">Sample style</span>' +
        '<select id="api-flavor" class="api-flavor-select" title="SDK vs language-only sample">' +
        '<option value="sdk"' +
        (state.flavor !== "raw" ? " selected" : "") +
        ">" +
        escapeHtml(fl.sdk) +
        "</option>" +
        '<option value="raw"' +
        (state.flavor === "raw" ? " selected" : "") +
        ">" +
        escapeHtml(fl.raw) +
        "</option>" +
        "</select></label>";
    }
    html += "</div>";

    var sampleLang = samples[state.lang] ? state.lang : "curl";
    html += codeBlock(sampleLang, samples[sampleLang]);
    if (state.lang === "curl") {
      html +=
        '<p class="api-muted">curl shows the raw HTTP or unix-socket call. Switch to Go, TypeScript, or Python for SDK or language-only samples. Nothing is executed from this page.</p>';
    } else if (state.flavor === "raw") {
      html +=
        '<p class="api-muted">Language-only sample (no grain SDK). Prefer the SDK option for production clients. Nothing is executed from this page.</p>';
    } else {
      html +=
        '<p class="api-muted">Official grain SDK sample. Use the dropdown for a plain-language version (<code>requests</code> / <code>fetch</code> / <code>net/http</code>). Nothing is executed from this page.</p>';
    }
    html += "</section>";

    html += '<section class="api-section"><h3>Responses</h3>';
    var responses = op.responses || {};
    Object.keys(responses)
      .sort()
      .forEach(function (code) {
        var resp = deref(responses[code]) || {};
        var ex = responseExample(resp);
        html += '<div class="api-response">';
        html +=
          '<div class="api-response-head"><span class="api-status">' +
          escapeHtml(code) +
          "</span> " +
          escapeHtml(resp.description || "") +
          "</div>";
        if (ex) {
          var pretty =
            typeof ex.value === "string" && ex.media && ex.media.indexOf("json") === -1
              ? ex.value
              : JSON.stringify(ex.value, null, 2);
          var blockLang =
            ex.media && ex.media.indexOf("json") !== -1
              ? "json"
              : typeof ex.value === "string"
                ? "bash"
                : "json";
          html +=
            '<p class="api-media">' +
            escapeHtml(ex.media) +
            "</p>" +
            codeBlock(blockLang, pretty);
        }
        html += "</div>";
      });
    html += "</section>";

    html += "</article>";
    return html;
  }

  function findOp(id) {
    var ops = collectOperations(state.spec);
    for (var i = 0; i < ops.length; i++) {
      if (ops[i].id === id) return ops[i];
    }
    return ops[0] || null;
  }

  function render() {
    if (!state.spec) return;
    var ops = collectOperations(state.spec);
    var groups = groupByTag(ops);
    if (!state.selectedId && ops.length) {
      var hash = (location.hash || "").replace(/^#/, "");
      state.selectedId = hash || ops[0].id;
    }
    var selected = findOp(state.selectedId);
    if (selected) state.selectedId = selected.id;

    root.innerHTML =
      '<div class="api-explorer-grid">' +
      renderNav(groups) +
      '<div class="api-main">' +
      renderDetail(selected) +
      "</div></div>";

    // wire controls
    var filter = root.querySelector("#api-filter");
    if (filter) {
      filter.addEventListener("input", function () {
        state.filter = filter.value;
        render();
        var f = root.querySelector("#api-filter");
        if (f) {
          f.focus();
          var v = f.value;
          f.value = "";
          f.value = v;
        }
      });
    }

    function readEnvFromForm() {
      var hostEl = root.querySelector("#api-env-host");
      var portEl = root.querySelector("#api-env-port");
      var tokenEl = root.querySelector("#api-env-token");
      var transportEl = root.querySelector("#api-env-transport");
      var socketEl = root.querySelector("#api-env-socket");
      if (hostEl) state.env.host = hostEl.value;
      if (portEl) state.env.port = portEl.value;
      if (tokenEl) state.env.token = tokenEl.value;
      if (transportEl) state.env.transport = transportEl.value === "unix" ? "unix" : "tcp";
      if (socketEl) state.env.socket = socketEl.value;
      saveEnv();
    }
    function bindEnvField(sel, onChange) {
      var el = root.querySelector(sel);
      if (!el) return;
      el.addEventListener("change", onChange);
      el.addEventListener("input", function () {
        // Debounce full re-render for typing; update state live so blur is enough
        readEnvFromForm();
      });
    }
    // Re-render on change (transport) and on blur after typing host/port/token
    ["#api-env-host", "#api-env-port", "#api-env-token", "#api-env-socket"].forEach(function (sel) {
      var el = root.querySelector(sel);
      if (!el) return;
      el.addEventListener("change", function () {
        readEnvFromForm();
        render();
      });
      el.addEventListener("blur", function () {
        readEnvFromForm();
        render();
      });
      // Enter applies immediately
      el.addEventListener("keydown", function (ev) {
        if (ev.key === "Enter") {
          ev.preventDefault();
          readEnvFromForm();
          render();
        }
      });
    });
    var transportEl = root.querySelector("#api-env-transport");
    if (transportEl) {
      transportEl.addEventListener("change", function () {
        readEnvFromForm();
        render();
      });
    }
    var resetBtn = root.querySelector("#api-env-reset");
    if (resetBtn) {
      resetBtn.addEventListener("click", function () {
        state.env = {
          host: ENV_DEFAULTS.host,
          port: ENV_DEFAULTS.port,
          token: ENV_DEFAULTS.token,
          socket: ENV_DEFAULTS.socket,
          transport: ENV_DEFAULTS.transport,
        };
        saveEnv();
        render();
      });
    }
    root.querySelectorAll("[data-op-id]").forEach(function (a) {
      a.addEventListener("click", function (e) {
        e.preventDefault();
        state.selectedId = a.getAttribute("data-op-id");
        if (history.replaceState) {
          history.replaceState(null, "", "#" + state.selectedId);
        } else {
          location.hash = state.selectedId;
        }
        render();
      });
    });
    root.querySelectorAll(".api-lang-tab[data-lang]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        state.lang = btn.getAttribute("data-lang");
        saveLang(state.lang);
        render();
      });
    });
    var flavorSel = root.querySelector("#api-flavor");
    if (flavorSel) {
      flavorSel.addEventListener("change", function () {
        state.flavor = flavorSel.value === "raw" ? "raw" : "sdk";
        saveFlavor(state.flavor);
        render();
      });
    }

    // Prism runs after DOM is in place (script is deferred after Prism components).
    highlightCode();
    requestAnimationFrame(highlightCode);
  }

  function boot(spec) {
    state.spec = spec;
    render();
    window.addEventListener("hashchange", function () {
      var h = (location.hash || "").replace(/^#/, "");
      if (h && h !== state.selectedId) {
        state.selectedId = h;
        render();
      }
    });
  }

  fetch(specUrl)
    .then(function (r) {
      if (!r.ok) throw new Error("Failed to load " + specUrl + " (" + r.status + ")");
      return r.json();
    })
    .then(boot)
    .catch(function (err) {
      root.innerHTML =
        '<div class="api-error"><p><strong>Could not load OpenAPI spec.</strong></p><p>' +
        escapeHtml(err.message || String(err)) +
        "</p><p>Expected <code>" +
        escapeHtml(specUrl) +
        "</code>.</p></div>";
    });
})();
