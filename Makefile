# grain — local Linux microVM sandboxes

VERSION ?= 0.1.0-dev
BIN     ?= bin/grain
export CGO_ENABLED ?= 0

.PHONY: all build test cover lint fmt clean run-help smoke-api doctor obs-up obs-down

all: test build

build:
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/grain

test:
	go test ./... -count=1

cover:
	go test ./... -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out | tail -5

fmt:
	go fmt ./...

clean:
	rm -rf bin coverage.out

run-help: build
	$(BIN) --help

# Integration smoke with mock hypervisor (no qemu required)
up-mock: build
	@mkdir -p /tmp/grain-mock
	@printf '%s\n' \
	  'data_dir: /tmp/grain-mock' \
	  'socket: /tmp/grain-mock/grain.sock' \
	  'api: 127.0.0.1:7474' \
	  'metrics_addr: 127.0.0.1:7474' \
	  'hypervisor: mock' \
	  'log_level: info' \
	  > /tmp/grain-mock/config.yaml
	$(BIN) --config /tmp/grain-mock/config.yaml up --fg &
	@sleep 0.4
	@echo "mock daemon up"

smoke-api: build
	@mkdir -p /tmp/grain-smoke
	@printf '%s\n' \
	  'data_dir: /tmp/grain-smoke' \
	  'socket: /tmp/grain-smoke/grain.sock' \
	  'api: 127.0.0.1:17474' \
	  'hypervisor: mock' \
	  'log_level: error' \
	  > /tmp/grain-smoke/config.yaml
	@rm -f /tmp/grain-smoke/grain.sock /tmp/grain-smoke/grain.pid
	@$(BIN) --config /tmp/grain-smoke/config.yaml up --fg & echo $$! > /tmp/grain-smoke/test.pid
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
	  if curl -sf --unix-socket /tmp/grain-smoke/grain.sock http://grain/healthz >/dev/null; then break; fi; \
	  sleep 0.2; \
	done
	@$(BIN) --config /tmp/grain-smoke/config.yaml new
	@$(BIN) --config /tmp/grain-smoke/config.yaml ls
	@name=$$($(BIN) --config /tmp/grain-smoke/config.yaml ls | awk 'NR==2{print $$1}'); \
	  $(BIN) --config /tmp/grain-smoke/config.yaml rm $$name
	@curl -sf --unix-socket /tmp/grain-smoke/grain.sock http://grain/metrics | head -5
	@kill $$(cat /tmp/grain-smoke/test.pid) 2>/dev/null || true
	@echo "smoke-api OK"

doctor: build
	$(BIN) doctor || true

# Optional observability stack (Prometheus + Grafana + Loki)
obs-up:
	docker compose -f deploy/observability/docker-compose.yml up -d
	@echo "Grafana  http://localhost:3000  (admin/admin)"
	@echo "Prometheus http://localhost:9090"
	@echo "Scrape grain metrics: http://127.0.0.1:7474/metrics"

obs-down:
	docker compose -f deploy/observability/docker-compose.yml down
