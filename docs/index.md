---
layout: home
title: Fast Linux microVMs on your hardware
description: Local Linux microVMs for GitHub Actions (grain act) and throwaway k3s — plus sandboxes, agent, and APIs. macOS & Linux.
---

<section class="hero">
  <div class="wrap hero-grid">
    <div>
      <h1>Real Linux sandboxes.<br>On your machine.</h1>
      <p class="hero-lead">
        grain is a local microVM control plane — small like a grain of sand, complete like a real Linux box.
        Ephemeral by default, persistent when you need it, with a guest agent for exec, files, and shell.
      </p>
      <p class="hero-wedges">
        <a href="#workloads"><strong>grain act</strong> — GitHub Actions in an isolated microVM</a>
        <span class="hero-wedge-sep" aria-hidden="true">·</span>
        <a href="#workloads"><strong>--preset k3s</strong> — throwaway single-node cluster</a>
      </p>
      <div class="hero-cta">
        <a class="btn btn-primary" href="#install">Install grain</a>
        <a class="btn btn-ghost" href="#workloads">act &amp; k3s</a>
        <a class="btn btn-ghost" href="{{ '/get-started/quickstart/' | relative_url }}">Quick start</a>
      </div>
      <p class="hero-meta">macOS Apple Silicon &amp; Linux · QEMU · optional Firecracker · Go, TypeScript &amp; Python SDKs</p>
    </div>

    <div class="install-card" id="install" data-tabs>
      <div class="install-card-head">
        <h2>Install</h2>
        <button type="button" class="copy-btn" data-copy="#install-cmd-macos">Copy</button>
      </div>
      <div class="tabs" role="tablist" aria-label="Operating system">
        <button type="button" class="tab active" role="tab" data-tab="macos" aria-selected="true">macOS</button>
        <button type="button" class="tab" role="tab" data-tab="linux" aria-selected="false">Linux</button>
        <button type="button" class="tab" role="tab" data-tab="source" aria-selected="false">From source</button>
      </div>

      <div class="panel" data-panel="macos" id="panel-macos">
        <pre class="code-block install-code" id="install-cmd-macos" data-copy-text="curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash&#10;brew install qemu&#10;grain doctor"><code><span class="line"><span class="prompt">$</span> curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash</span>
<span class="line"><span class="prompt">$</span> brew install qemu</span>
<span class="line"><span class="prompt">$</span> grain doctor</span></code></pre>
        <p class="panel-note">Installer places the CLI on your PATH and the guest agent under <code>~/.grain/agent/</code>. QEMU is required for real VMs.</p>
      </div>

      <div class="panel" data-panel="linux" id="panel-linux" hidden>
        <pre class="code-block install-code" id="install-cmd-linux" data-copy-text="curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash&#10;sudo apt-get install -y qemu-system qemu-utils&#10;grain doctor"><code><span class="line"><span class="prompt">$</span> curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash</span>
<span class="line"><span class="prompt">$</span> sudo apt-get install -y qemu-system qemu-utils   <span class="faint"># Debian/Ubuntu</span></span>
<span class="line"><span class="prompt">$</span> grain doctor</span></code></pre>
        <p class="panel-note">On Fedora use <code>dnf install qemu-system-x86 qemu-img</code>. KVM improves performance when available.</p>
      </div>

      <div class="panel" data-panel="source" id="panel-source" hidden>
        <pre class="code-block install-code" id="install-cmd-source" data-copy-text="go install github.com/cxdy/grain/cmd/grain@latest&#10;# or from a checkout:&#10;just build &amp;&amp; just agent-linux &amp;&amp; ./bin/grain doctor"><code><span class="line"><span class="prompt">$</span> go install github.com/cxdy/grain/cmd/grain@latest</span>
<span class="line"><span class="prompt">$</span> <span class="faint"># or from a checkout:</span></span>
<span class="line"><span class="prompt">$</span> just build &amp;&amp; just agent-linux &amp;&amp; ./bin/grain doctor</span></code></pre>
        <p class="panel-note">Requires Go 1.23+. Release binaries also ship on <a href="https://github.com/cxdy/grain/releases">GitHub Releases</a>.</p>
      </div>
    </div>
  </div>
</section>

<section class="section home-demo" id="demo-section">
  <div class="wrap">
    <div class="demo-section-head">
      <div>
        <h2 class="section-title">Try it: shell · act · k3s</h2>
        <p class="section-lead" style="margin-bottom:0">
          Simulated terminal — switch scenarios above the prompt. Type commands or click <strong>Run step</strong>.
          <a href="#demo-act">act</a> · <a href="#demo-k3s">k3s</a> · <a href="#demo-shell">shell</a>
        </p>
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

<section class="section section-workloads" id="workloads">
  <div class="wrap">
    <h2 class="section-title">Two workflows people use first</h2>
    <p class="section-lead">
      Beyond a plain shell: run <strong>GitHub Actions</strong> or a <strong>k3s</strong> lab inside a disposable microVM —
      not on host Docker, not as a permanent desktop VM.
    </p>
    <div class="workloads">
      <article class="workload">
        <div class="workload-head">
          <p class="workload-label">CI debugging</p>
          <h3>GitHub Actions with <code>grain act</code></h3>
        </div>
        <p class="workload-lead">
          Boots Docker + <a href="https://github.com/nektos/act">nektos/act</a> in an isolated Linux microVM,
          mounts your repo, runs workflows, then tears the sandbox down. Host Docker stays clean.
        </p>
        <pre class="code-block install-code" id="workload-act" data-copy-text="grain up&#10;grain image pull grain-ubuntu&#10;cd /path/to/your/repo&#10;grain act -- -l&#10;grain act -- -j test"><code><span class="line"><span class="prompt">$</span> grain up</span>
<span class="line"><span class="prompt">$</span> grain image pull grain-ubuntu</span>
<span class="line"><span class="prompt">$</span> cd /path/to/your/repo</span>
<span class="line"><span class="prompt">$</span> grain act -- -l</span>
<span class="line"><span class="prompt">$</span> grain act -- -j test</span></code></pre>
        <div class="workload-actions">
          <button type="button" class="btn btn-sm btn-ghost copy-btn-inline" data-copy="#workload-act">Copy commands</button>
          <a class="btn btn-sm btn-ghost" href="#demo-act">Try demo</a>
          <a class="btn btn-sm btn-primary" href="{{ '/guides/recipes/act/' | relative_url }}">act recipe →</a>
        </div>
      </article>

      <article class="workload">
        <div class="workload-head">
          <p class="workload-label">Local Kubernetes</p>
          <h3>Throwaway k3s lab</h3>
        </div>
        <p class="workload-lead">
          One preset installs single-node <strong>k3s</strong>, publishes the API port, and keeps state on a persistent disk when you pass <code>-p</code>.
          Grab kubeconfig and use host <code>kubectl</code>.
        </p>
        <pre class="code-block install-code" id="workload-k3s" data-copy-text="grain up&#10;grain image pull grain-ubuntu&#10;grain new --preset k3s -n lab -p --wait userdata&#10;grain fwd ls lab"><code><span class="line"><span class="prompt">$</span> grain up</span>
<span class="line"><span class="prompt">$</span> grain image pull grain-ubuntu</span>
<span class="line"><span class="prompt">$</span> grain new --preset k3s -n lab -p --wait userdata</span>
<span class="line"><span class="prompt">$</span> grain fwd ls lab</span></code></pre>
        <div class="workload-actions">
          <button type="button" class="btn btn-sm btn-ghost copy-btn-inline" data-copy="#workload-k3s">Copy commands</button>
          <a class="btn btn-sm btn-ghost" href="#demo-k3s">Try demo</a>
          <a class="btn btn-sm btn-primary" href="{{ '/guides/recipes/k3s/' | relative_url }}">k3s recipe →</a>
        </div>
      </article>
    </div>
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
        <h3>Presets that matter</h3>
        <p><code>docker</code>, <code>k3s</code>, and <code>act</code> bake cloud-init so common labs are one flag — not a shell cookbook.</p>
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
        <p>Install, first sandbox, then jump to <a href="{{ '/guides/recipes/act/' | relative_url }}">act</a> or <a href="{{ '/guides/recipes/k3s/' | relative_url }}">k3s</a> when you have a real workload.</p>
        <a class="btn btn-sm btn-ghost" href="{{ '/get-started/quickstart/' | relative_url }}">Start here →</a>
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
