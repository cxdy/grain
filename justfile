# grain development commands. Run `just` to list recipes.
# Inspired by grex: just + svu next for tags, GoReleaser on v* tags.

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

# Unit tests (mock hypervisor — no QEMU required).
test:
    go test ./... -count=1

# Unit tests + CLI build.
all: test build

# Build the grain CLI into bin/grain.
build:
    mkdir -p bin
    go build -ldflags "{{ ldflags }}" -o {{ bin }} ./cmd/grain

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

# gofmt all packages.
fmt:
    go fmt ./...

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
