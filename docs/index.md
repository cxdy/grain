---
layout: home
title: Fast Linux microVMs on your hardware
description: grain runs disposable Linux sandboxes locally — install in one command, shell in seconds.
---

<section class="hero">
  <div class="wrap hero-grid">
    <div>
      <h1>Real Linux sandboxes.<br>On your machine.</h1>
      <p class="hero-lead">
        grain is a local microVM control plane — small like a grain of sand, complete like a real Linux box.
        Ephemeral by default, persistent when you need it, with a guest agent for exec, files, and shell.
      </p>
      <div class="hero-cta">
        <a class="btn btn-primary" href="#install">Install grain</a>
        <a class="btn btn-ghost" href="#demo">Try interactive demo</a>
        <a class="btn btn-ghost" href="{{ '/get-started/first-sandbox/' | relative_url }}">Full tutorial</a>
      </div>
      <p class="hero-meta">macOS Apple Silicon &amp; Linux · QEMU · optional Firecracker · Go, TypeScript &amp; Python SDKs</p>
    </div>

    <div class="install-card" id="install" data-tabs>
      <div class="install-card-head">
        <h2>Install</h2>
        <button type="button" class="copy-btn" data-copy="#install-cmd-active">Copy</button>
      </div>
      <div class="tabs" role="tablist" aria-label="Operating system">
        <button type="button" class="tab active" role="tab" data-tab="macos" aria-selected="true">macOS</button>
        <button type="button" class="tab" role="tab" data-tab="linux" aria-selected="false">Linux</button>
        <button type="button" class="tab" role="tab" data-tab="source" aria-selected="false">From source</button>
      </div>

      <div class="panel" data-panel="macos" id="panel-macos">
        <div class="code-block" id="install-cmd-macos">
          <div><span class="prompt">$</span> curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash</div>
          <div><span class="prompt">$</span> brew install qemu</div>
          <div><span class="prompt">$</span> grain doctor</div>
        </div>
        <p class="panel-note">Installer places the CLI on your PATH and the guest agent under <code>~/.grain/agent/</code>. QEMU is required for real VMs.</p>
      </div>

      <div class="panel" data-panel="linux" id="panel-linux" hidden>
        <div class="code-block" id="install-cmd-linux">
          <div><span class="prompt">$</span> curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash</div>
          <div><span class="prompt">$</span> sudo apt-get install -y qemu-system qemu-utils   <span class="faint"># Debian/Ubuntu</span></div>
          <div><span class="prompt">$</span> grain doctor</div>
        </div>
        <p class="panel-note">On Fedora use <code>dnf install qemu-system-x86 qemu-img</code>. KVM improves performance when available.</p>
      </div>

      <div class="panel" data-panel="source" id="panel-source" hidden>
        <div class="code-block" id="install-cmd-source">
          <div><span class="prompt">$</span> go install github.com/cxdy/grain/cmd/grain@latest</div>
          <div><span class="prompt">$</span> # or from a checkout:</div>
          <div><span class="prompt">$</span> just build && just agent-linux && ./bin/grain doctor</div>
        </div>
        <p class="panel-note">Requires Go 1.23+. Release binaries also ship on <a href="https://github.com/cxdy/grain/releases">GitHub Releases</a>.</p>
      </div>
    </div>
  </div>
</section>

<script>
  /* Keep copy button targeting the visible panel’s code block */
  document.addEventListener('DOMContentLoaded', function () {
    var card = document.querySelector('#install[data-tabs]');
    if (!card) return;
    var btn = card.querySelector('[data-copy]');
    card.querySelectorAll('[data-tab]').forEach(function (t) {
      t.addEventListener('click', function () {
        var id = t.getAttribute('data-tab');
        var map = { macos: '#install-cmd-macos', linux: '#install-cmd-linux', source: '#install-cmd-source' };
        if (btn && map[id]) btn.setAttribute('data-copy', map[id]);
      });
    });
    btn && btn.setAttribute('data-copy', '#install-cmd-macos');
  });
</script>

<section class="section home-demo" id="demo-section">
  <div class="wrap">
    <div class="demo-section-head">
      <div>
        <h2 class="section-title">Try it: install → shell</h2>
        <p class="section-lead" style="margin-bottom:0">A guided simulation of the first-sandbox path. Type commands or click <strong>Run step</strong>.</p>
      </div>
      <a class="btn btn-sm btn-ghost" href="{{ '/get-started/first-sandbox/' | relative_url }}">Open full tutorial →</a>
    </div>
    {% include sandbox-demo.html %}
  </div>
</section>

<section class="section">
  <div class="wrap">
    <h2 class="section-title">From zero to shell in three steps</h2>
    <p class="section-lead">After install, this is the happy path most people use every day.</p>
    <div class="steps">
      <div class="step">
        <div class="step-num">1</div>
        <h3>Start the daemon</h3>
        <p><code>grain up</code> starts the local control plane (unix socket + optional TCP API).</p>
      </div>
      <div class="step">
        <div class="step-num">2</div>
        <h3>Pull a base image</h3>
        <p><code>grain image pull grain-ubuntu</code> for the golden agent image, or <code>ubuntu-cloud</code> once.</p>
      </div>
      <div class="step">
        <div class="step-num">3</div>
        <h3>Create &amp; shell</h3>
        <p><code>grain new</code> then <code>grain sh</code> — or just <code>grain sh</code> to auto-create when none exist.</p>
      </div>
    </div>
    <pre class="code-block" style="margin-top:1.25rem"><span class="prompt">$</span> grain up
<span class="prompt">$</span> grain image pull grain-ubuntu
<span class="prompt">$</span> grain new
<span class="prompt">$</span> grain sh
<span class="prompt">$</span> grain x -- uname -a</pre>
  </div>
</section>

<section class="section">
  <div class="wrap">
    <h2 class="section-title">Built for real work</h2>
    <p class="section-lead">Short commands, a guest agent, and optional hardening — without a cloud bill.</p>
    <div class="features">
      <div class="feature">
        <h3>Ephemeral by default</h3>
        <p>Sandboxes disappear on stop. Pass <code>-p</code> when you want a lab that survives restarts.</p>
      </div>
      <div class="feature">
        <h3>Guest agent</h3>
        <p>Exec, PTY shell, file copy, and filesystem APIs without living in SSH for every call.</p>
      </div>
      <div class="feature">
        <h3>Ports &amp; mounts</h3>
        <p>Publish host ports, share directories (9p / virtiofs), and forward sockets for Docker-style workflows.</p>
      </div>
      <div class="feature">
        <h3>Egress proxy</h3>
        <p>Default-deny outbound HTTP(S) with allow rules and optional secret injection on the wire.</p>
      </div>
      <div class="feature">
        <h3>GitHub Actions locally</h3>
        <p><code>grain act</code> boots Docker + <a href="https://github.com/nektos/act">act</a> in an isolated microVM, runs your workflows, then tears the sandbox down.</p>
      </div>
      <div class="feature">
        <h3>Automate it</h3>
        <p>Unix socket API, OpenAPI, Go, TypeScript, and Python SDKs for agents and CI.</p>
      </div>
    </div>
  </div>
</section>

<section class="section">
  <div class="wrap">
    <h2 class="section-title">Docs for every role</h2>
    <p class="section-lead">Same product, three entry paths.</p>
    <div class="audiences">
      <div class="audience">
        <h3>End users</h3>
        <p>Install, first sandbox, daily CLI, recipes for coding agents, k3s, and <code>grain act</code>.</p>
        <a class="btn btn-sm btn-ghost" href="{{ '/get-started/install/' | relative_url }}">Start here →</a>
      </div>
      <div class="audience">
        <h3>Administrators</h3>
        <p>Config, resource caps, images, proxy, secrets, Firecracker, troubleshooting.</p>
        <a class="btn btn-sm btn-ghost" href="{{ '/guides/' | relative_url }}">Ops guides →</a>
      </div>
      <div class="audience">
        <h3>Developers</h3>
        <p>HTTP API, OpenAPI, Go / TypeScript / Python SDKs, agent protocol.</p>
        <a class="btn btn-sm btn-ghost" href="{{ '/reference/go-sdk/' | relative_url }}">Build with grain →</a>
      </div>
    </div>
  </div>
</section>

<section class="wrap">
  <div class="cta-band">
    <div>
      <h2>Ready when you are</h2>
      <p>Free and open source. No metering. Run it on a laptop or a bare-metal box.</p>
    </div>
    <div class="hero-cta" style="margin:0">
      <a class="btn btn-primary" href="#install">Install</a>
      <a class="btn btn-ghost" href="https://github.com/cxdy/grain" rel="noopener" target="_blank">Star on GitHub</a>
    </div>
  </div>
</section>
