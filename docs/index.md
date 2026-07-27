---
layout: home
title: Linux microVMs on your hardware
description: Local Linux microVMs for shells, GitHub Actions (grain act), and throwaway k3s — on macOS and Linux.
---

<section class="hero">
  <div class="wrap hero-grid">
    <div>
      <p class="hero-kicker">Field manual</p>
      <h1>Real Linux sandboxes.<br><span class="hero-line2">On your machine.</span></h1>
      <p class="hero-lead">
        grain runs small, disposable Linux VMs locally — for a shell, for GitHub Actions,
        or for a throwaway k3s lab. Ephemeral by default; persistent when you need it.
      </p>
      <div class="hero-cta">
        <a class="btn btn-primary" href="#install">Install</a>
        <a class="btn btn-ghost" href="#paths">Pick a path</a>
        <a class="btn btn-ghost" href="{{ '/get-started/quickstart/' | relative_url }}">Quick start</a>
      </div>
      <p class="hero-meta">macOS · Linux · QEMU · optional Firecracker · Go · TypeScript · Python</p>
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
        <p class="panel-note">Places the CLI on your PATH and the guest agent under <code>~/.grain/agent/</code>. QEMU is required for real VMs.</p>
      </div>

      <div class="panel" data-panel="linux" id="panel-linux" hidden>
        <pre class="code-block install-code" id="install-cmd-linux" data-copy-text="curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash&#10;sudo apt-get install -y qemu-system qemu-utils&#10;grain doctor"><code><span class="line"><span class="prompt">$</span> curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash</span>
<span class="line"><span class="prompt">$</span> sudo apt-get install -y qemu-system qemu-utils   <span class="faint"># Debian/Ubuntu</span></span>
<span class="line"><span class="prompt">$</span> grain doctor</span></code></pre>
        <p class="panel-note">On Fedora use <code>dnf install qemu-system-x86 qemu-img</code>. KVM helps when available.</p>
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

<section class="section section-rule" id="paths">
  <div class="wrap">
    <h2 class="section-title">Pick a path</h2>
    <p class="section-lead">Same install, three first destinations. Start with the goal you care about.</p>
    <div class="paths">
      <a class="path" href="{{ '/get-started/first-sandbox/' | relative_url }}">
        <span class="path-num">01 · Shell</span>
        <h3>Open a Linux shell</h3>
        <p>Pull an image, create a sandbox, drop into a PTY. The everyday loop.</p>
        <p class="path-cmd">grain new && grain sh</p>
        <span class="path-go">First sandbox →</span>
      </a>
      <a class="path" href="{{ '/guides/recipes/act/' | relative_url }}">
        <span class="path-num">02 · CI</span>
        <h3>Run GitHub Actions</h3>
        <p>nektos/act inside an isolated microVM so host Docker stays clean.</p>
        <p class="path-cmd">grain act -- -j test</p>
        <span class="path-go">act recipe →</span>
      </a>
      <a class="path" href="{{ '/guides/recipes/k3s/' | relative_url }}">
        <span class="path-num">03 · k3s</span>
        <h3>Spin up a k3s lab</h3>
        <p>Single-node Kubernetes with the API on a host port. Tear it down when done.</p>
        <p class="path-cmd">grain new --preset k3s -p</p>
        <span class="path-go">k3s recipe →</span>
      </a>
    </div>
  </div>
</section>

<section class="section home-demo" id="demo-section">
  <div class="wrap">
    <div class="demo-section-head">
      <div>
        <h2 class="section-title">Try it here</h2>
        <p class="section-lead" style="margin-bottom:0">
          Simulated terminal — switch scenarios, type commands, or click <strong>Run step</strong>.
        </p>
      </div>
      <a class="btn btn-sm btn-ghost" href="{{ '/get-started/first-sandbox/' | relative_url }}">Full tutorial →</a>
    </div>
    {% include sandbox-demo.html %}
  </div>
</section>

<section class="section section-rule">
  <div class="wrap">
    <h2 class="section-title">From zero to shell</h2>
    <p class="section-lead">After install, this is the happy path most people use every day.</p>
    <div class="steps">
      <div class="step">
        <div class="step-num">Step 1</div>
        <h3>Start the daemon</h3>
        <p><code>grain up</code> starts the local control plane (unix socket + optional TCP API).</p>
      </div>
      <div class="step">
        <div class="step-num">Step 2</div>
        <h3>Pull a base image</h3>
        <p><code>grain image pull grain-ubuntu</code> for the golden agent image.</p>
      </div>
      <div class="step">
        <div class="step-num">Step 3</div>
        <h3>Create &amp; shell</h3>
        <p><code>grain new</code> then <code>grain sh</code> — or just <code>grain sh</code> to auto-create.</p>
      </div>
    </div>
    <pre class="cmd-strip" aria-label="Happy path commands"><span class="prompt">$</span> grain up
<span class="prompt">$</span> grain image pull grain-ubuntu
<span class="prompt">$</span> grain new
<span class="prompt">$</span> grain sh</pre>
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
          mounts your repo, runs workflows, then tears the sandbox down.
        </p>
        <pre class="code-block install-code" id="workload-act" data-copy-text="grain up&#10;grain image pull grain-ubuntu&#10;cd /path/to/your/repo&#10;grain act -- -l&#10;grain act -- -j test"><code><span class="line"><span class="prompt">$</span> grain up</span>
<span class="line"><span class="prompt">$</span> grain image pull grain-ubuntu</span>
<span class="line"><span class="prompt">$</span> cd /path/to/your/repo</span>
<span class="line"><span class="prompt">$</span> grain act -- -l</span>
<span class="line"><span class="prompt">$</span> grain act -- -j test</span></code></pre>
        <div class="workload-actions">
          <button type="button" class="btn btn-sm btn-ghost copy-btn-inline" data-copy="#workload-act">Copy</button>
          <a class="btn btn-sm btn-ghost" href="#demo-section">Try demo</a>
          <a class="btn btn-sm btn-primary" href="{{ '/guides/recipes/act/' | relative_url }}">act recipe →</a>
        </div>
      </article>

      <article class="workload">
        <div class="workload-head">
          <p class="workload-label">Local Kubernetes</p>
          <h3>Throwaway k3s lab</h3>
        </div>
        <p class="workload-lead">
          One preset installs single-node <strong>k3s</strong>, publishes the API port, and keeps state on a persistent disk with <code>-p</code>.
          Grab kubeconfig and use host <code>kubectl</code>.
        </p>
        <pre class="code-block install-code" id="workload-k3s" data-copy-text="grain up&#10;grain image pull grain-ubuntu&#10;grain new --preset k3s -n lab -p --wait userdata&#10;grain fwd ls lab"><code><span class="line"><span class="prompt">$</span> grain up</span>
<span class="line"><span class="prompt">$</span> grain image pull grain-ubuntu</span>
<span class="line"><span class="prompt">$</span> grain new --preset k3s -n lab -p --wait userdata</span>
<span class="line"><span class="prompt">$</span> grain fwd ls lab</span></code></pre>
        <div class="workload-actions">
          <button type="button" class="btn btn-sm btn-ghost copy-btn-inline" data-copy="#workload-k3s">Copy</button>
          <a class="btn btn-sm btn-ghost" href="#demo-section">Try demo</a>
          <a class="btn btn-sm btn-primary" href="{{ '/guides/recipes/k3s/' | relative_url }}">k3s recipe →</a>
        </div>
      </article>
    </div>
  </div>
</section>

<section class="section">
  <div class="wrap">
    <h2 class="section-title">What you get</h2>
    <p class="section-lead">Short commands, a guest agent, and optional hardening — without a cloud bill.</p>
    <dl class="spec-list">
      <div class="spec-item">
        <dt>Ephemeral by default</dt>
        <dd>Sandboxes disappear on stop. Pass <code>-p</code> when you want a lab that survives restarts.</dd>
      </div>
      <div class="spec-item">
        <dt>Guest agent</dt>
        <dd>Exec, PTY shell, file copy, and filesystem APIs without living in SSH for every call.</dd>
      </div>
      <div class="spec-item">
        <dt>Ports &amp; mounts</dt>
        <dd>Publish host ports, share directories (9p / virtiofs), and forward sockets for Docker-style workflows.</dd>
      </div>
      <div class="spec-item">
        <dt>Egress proxy</dt>
        <dd>Default-deny outbound HTTP(S) with allow rules and optional secret injection on the wire.</dd>
      </div>
      <div class="spec-item">
        <dt>Presets</dt>
        <dd><code>docker</code>, <code>k3s</code>, and <code>act</code> bake cloud-init so common labs are one flag.</dd>
      </div>
      <div class="spec-item">
        <dt>Automate it</dt>
        <dd>Unix socket API, <a href="{{ '/reference/openapi/' | relative_url }}">OpenAPI explorer</a>, Go, TypeScript, and Python SDKs for agents and CI.</dd>
      </div>
    </dl>
  </div>
</section>

<section class="section section-rule">
  <div class="wrap">
    <h2 class="section-title">How the docs are organized</h2>
    <p class="section-lead">Four kinds of pages. Pick the shape that matches what you need right now.</p>
    <div class="doc-map">
      <div class="map-card">
        <p class="map-kind">Tutorials</p>
        <h3>Learn</h3>
        <p>Guided paths from zero to a working sandbox. Start here if you are new.</p>
        <a href="{{ '/get-started/quickstart/' | relative_url }}">Get started →</a>
      </div>
      <div class="map-card">
        <p class="map-kind">How-to</p>
        <h3>Do</h3>
        <p>Recipes for act, k3s, mounts, networking, Firecracker, and more.</p>
        <a href="{{ '/guides/' | relative_url }}">Guides →</a>
      </div>
      <div class="map-card">
        <p class="map-kind">Reference</p>
        <h3>Look up</h3>
        <p>CLI, config, HTTP API, interactive OpenAPI explorer, and SDKs.</p>
        <a href="{{ '/reference/cli/' | relative_url }}">Reference →</a>
        <a href="{{ '/reference/openapi/' | relative_url }}">OpenAPI explorer →</a>
      </div>
      <div class="map-card">
        <p class="map-kind">Explanation</p>
        <h3>Understand</h3>
        <p>Architecture, security model, agent vs SSH, boot and images.</p>
        <a href="{{ '/explain/architecture/' | relative_url }}">Explain →</a>
      </div>
    </div>
  </div>
</section>

<section class="wrap">
  <div class="cta-band">
    <div>
      <h2>Ready when you are</h2>
      <p>Free and open source. No metering. Laptop or bare metal.</p>
    </div>
    <div class="hero-cta" style="margin:0">
      <a class="btn btn-primary" href="#install">Install</a>
      <a class="btn btn-ghost" href="https://github.com/cxdy/grain" rel="noopener" target="_blank">GitHub</a>
    </div>
  </div>
</section>
