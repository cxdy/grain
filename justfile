# grain development commands. Run `just` to list recipes.

set shell := ["bash", "-euo", "pipefail", "-c"]

export CGO_ENABLED := "0"

version := `git describe --tags --always --dirty 2>/dev/null || echo dev`
# Strip leading v for -X main.version (matches GoReleaser {{ .Version }}).
version_plain := `git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev`
commit := `git rev-parse --short HEAD 2>/dev/null || echo none`
ldflags := "-s -w -X main.version=" + version_plain

bin := "bin/grain"
dist := "dist"

# List available recipes.
default:
    @just --list

# Install tool versions (mise) and git hooks (pre-commit).
init:
    command -v mise >/dev/null 2>&1 && mise install || true
    if command -v pre-commit >/dev/null 2>&1; then \
        pre-commit install; \
    else \
        echo "pre-commit not found; install via mise or: pip install pre-commit"; \
        false; \
    fi

# Unit tests (mock hypervisor — no QEMU required).
test:
    env -u GOROOT GOTOOLCHAIN=auto go test ./... -count=1

# Unit tests + CLI build.
all: test build

# Build the grain CLI into bin/grain (CGO off for portable binary).
build:
    mkdir -p bin
    CGO_ENABLED=0 go build -ldflags "{{ ldflags }}" -o {{ bin }} ./cmd/grain

# Build CLI with CGO (needed for grain tray on macOS/Linux).
build-tray:
    mkdir -p bin
    CGO_ENABLED=1 go build -ldflags "{{ ldflags }}" -o {{ bin }} ./cmd/grain

# Guest agent for the host architecture (bin/grain-agent).
agent:
    mkdir -p bin
    go build -ldflags "{{ ldflags }}" -o bin/grain-agent ./cmd/grain-agent

# Cross-build Linux guest agents (arm64 + amd64) for SSH deploy into VMs.
# Required for live create when the guest does not already ship grain-agent.
agent-linux:
    mkdir -p bin
    GOOS=linux GOARCH=arm64 go build -ldflags "{{ ldflags }}" -o bin/grain-agent-linux-arm64 ./cmd/grain-agent
    GOOS=linux GOARCH=amd64 go build -ldflags "{{ ldflags }}" -o bin/grain-agent-linux-amd64 ./cmd/grain-agent

# Coverage summary (func totals).
cover:
    go test ./... -coverprofile=coverage.out -count=1
    go tool cover -func=coverage.out | tail -5

# Profile without -race for cobertura (race + cover is slower / noisier in CI).
# Writes coverage.out, coverage.html, and coverage.xml for PR comments.
# Excludes cmd/* (main packages) and tray (CGO UI) from the profile + 75% gate.
# Packages are skipped at test time too: instrumenting mains/tray can fail with
# "go: no such tool covdata" on some toolchains and would abort set -e.
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    mapfile -t pkgs < <(go list ./... | grep -vE '/cmd/|/internal/tray')
    env -u GOROOT GOTOOLCHAIN=auto go test -count=1 "${pkgs[@]}" \
        -coverprofile coverage.raw.out -covermode count
    # Keep single mode header; drop mains, CGO tray, and interactive PTY shell
    # (shell_linux.go needs a real guest TTY; unit CI cannot exercise the bridge).
    head -n1 coverage.raw.out > coverage.out
    tail -n +2 coverage.raw.out | grep -vE '/cmd/grain|/cmd/grain-agent|/internal/tray/|shell_linux\.go' >> coverage.out || true
    rm -f coverage.raw.out
    env -u GOROOT GOTOOLCHAIN=auto go tool cover -html=coverage.out -o coverage.html
    env -u GOROOT GOTOOLCHAIN=auto go run github.com/boumenot/gocover-cobertura@v1.4.0 \
        --by-files -ignore-gen-files < coverage.out > coverage.xml
    total=$(env -u GOROOT GOTOOLCHAIN=auto go tool cover -func=coverage.out | awk '/^total:/{print $3}')
    echo "total coverage: ${total} (cmd + tray excluded)"
    # Cobertura line-rate (what the PR comment gates on) is typically a few points
    # under statement coverage — print both.
    if [[ -f coverage.xml ]]; then
      python3 -c "import xml.etree.ElementTree as ET; r=ET.parse('coverage.xml').getroot(); lr=float(r.attrib.get('line-rate') or 0)*100; print('cobertura line-rate: %.1f%% (%s/%s)' % (lr, r.attrib.get('lines-covered'), r.attrib.get('lines-valid')))"
    fi
    pct=$(echo "$total" | tr -d '%')
    awk -v p="$pct" 'BEGIN{ if ((p+0) < 75.0) { printf "coverage %.1f%% is below 75%% minimum\n", p+0; exit 1 } }'

# gofmt all packages.
fmt:
    go fmt ./...

# Run golangci-lint.
lint:
    env -u GOROOT GOTOOLCHAIN=auto golangci-lint run

# Lint markdown with markdownlint-cli2.
markdownlint:
    npx --yes markdownlint-cli2

# Run pre-commit on all files.
pre-commit:
    pre-commit run --all-files

# Snapshot GoReleaser artifacts (tarballs + checksums), no publish.
# Requires: go install github.com/goreleaser/goreleaser/v2@latest
release-build:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v goreleaser >/dev/null 2>&1; then
      goreleaser release --snapshot --clean --skip=publish
      ls -la dist/*.tar.gz dist/checksums.txt 2>/dev/null || ls -la dist/
    else
      echo "goreleaser not found — writing bare binaries to {{ dist }}/"
      echo "  install: go install github.com/goreleaser/goreleaser/v2@latest"
      mkdir -p {{ dist }}
      GOOS=darwin GOARCH=arm64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain_darwin_arm64 ./cmd/grain
      GOOS=darwin GOARCH=amd64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain_darwin_amd64 ./cmd/grain
      GOOS=linux  GOARCH=arm64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain_linux_arm64  ./cmd/grain
      GOOS=linux  GOARCH=amd64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain_linux_amd64  ./cmd/grain
      GOOS=linux GOARCH=arm64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain-agent-linux-arm64 ./cmd/grain-agent
      GOOS=linux GOARCH=amd64 go build -ldflags "{{ ldflags }}" -o {{ dist }}/grain-agent-linux-amd64 ./cmd/grain-agent
      ls -la {{ dist }}/
    fi

# Remove build outputs.
clean:
    rm -rf bin dist coverage.out

# Build and print CLI help.
run-help: build
    {{ bin }} --help

# CLI + daemon e2e with mock hypervisor (no QEMU).
smoke-api: build
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p /tmp/grain-smoke
    printf '%s\n' \
      'data_dir: /tmp/grain-smoke' \
      'socket: /tmp/grain-smoke/grain.sock' \
      'api: 127.0.0.1:17474' \
      'hypervisor: mock' \
      'log_level: error' \
      > /tmp/grain-smoke/config.yaml
    rm -f /tmp/grain-smoke/grain.sock /tmp/grain-smoke/grain.pid
    {{ bin }} --config /tmp/grain-smoke/config.yaml up --fg & echo $! > /tmp/grain-smoke/test.pid
    for i in 1 2 3 4 5 6 7 8 9 10; do
      if curl -sf --unix-socket /tmp/grain-smoke/grain.sock http://grain/healthz >/dev/null; then break; fi
      sleep 0.2
    done
    {{ bin }} --config /tmp/grain-smoke/config.yaml new
    {{ bin }} --config /tmp/grain-smoke/config.yaml ls
    name=$({{ bin }} --config /tmp/grain-smoke/config.yaml ls | awk 'NR==2{print $1}')
    {{ bin }} --config /tmp/grain-smoke/config.yaml rm "$name"
    curl -sf --unix-socket /tmp/grain-smoke/grain.sock http://grain/metrics | head -5
    kill "$(cat /tmp/grain-smoke/test.pid)" 2>/dev/null || true
    echo "smoke-api OK"

# Dependency check (soft-fail doctor so recipe exits 0 when only warnings).
doctor: build
    {{ bin }} doctor || true

# Optional observability stack (Prometheus + Grafana + Loki).
obs-up:
    docker compose -f deploy/observability/docker-compose.yml up -d
    @echo "Grafana  http://localhost:3000  (admin/admin)"
    @echo "Prometheus http://localhost:9090"
    @echo "Scrape grain metrics: http://127.0.0.1:7474/metrics"

obs-down:
    docker compose -f deploy/observability/docker-compose.yml down

# Preview the next semver tag from conventional commits (does not tag).
version:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v svu >/dev/null 2>&1; then
      echo "svu is required: https://github.com/caarlos0/svu"
      echo "  brew install svu"
      echo "  # or: go install github.com/caarlos0/svu/v3@latest"
      exit 1
    fi
    echo "current: $(svu current 2>/dev/null || echo '(none)')"
    echo "next:    $(svu next)"

# Create and push the next semver tag from conventional commits (svu next).
# Triggers GoReleaser via .github/workflows/release.yml.
# Preview only: just version   (or: svu next)
release-tag:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v svu >/dev/null 2>&1; then
      echo "svu is required: https://github.com/caarlos0/svu"
      echo "  brew install svu"
      echo "  # or: go install github.com/caarlos0/svu/v3@latest"
      exit 1
    fi
    if [[ -n "$(git status --porcelain)" ]]; then
      echo "Working tree is dirty; commit or stash before releasing"
      exit 1
    fi
    git fetch --tags --quiet
    TAG=$(svu next)
    if git rev-parse "$TAG" >/dev/null 2>&1; then
      echo "Tag $TAG already exists (nothing new to release, or fetch remote tags)"
      echo "  svu current → $(svu current 2>/dev/null || true)"
      echo "  svu next    → $TAG"
      exit 1
    fi
    echo "Creating tag $TAG"
    git tag "$TAG"
    echo "Pushing tag $TAG to origin (and current HEAD)"
    git push origin HEAD
    git push origin "$TAG"
    echo "GoReleaser will publish GitHub Release assets for $TAG"

# Copy api/openapi.yaml into the GitHub Pages tree (docs/assets).
openapi-docs:
    cp api/openapi.yaml docs/assets/openapi.yaml
    @echo "updated docs/assets/openapi.yaml from api/openapi.yaml"
