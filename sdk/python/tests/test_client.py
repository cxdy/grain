"""Unit tests against a stdlib mock HTTP server (no live grain daemon)."""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Callable, Optional
from urllib.parse import parse_qs, urlparse

import pytest

from grain import (
    WAIT_AGENT,
    CreateRequest,
    ExecOpts,
    GrainAPIError,
    GrainClient,
    Mount,
)


class _MockState:
    token: str = ""
    last_create_query: dict[str, list[str]] = {}
    last_create_body: dict[str, Any] = {}
    deleted: list[str] = []


STATE = _MockState()


class _Handler(BaseHTTPRequestHandler):
    def log_message(self, format: str, *args: Any) -> None:  # noqa: A003
        return

    def _auth_ok(self) -> bool:
        if not STATE.token:
            return True
        if self.path.startswith("/healthz"):
            return True
        auth = self.headers.get("Authorization", "")
        return auth == f"Bearer {STATE.token}"

    def _read_json(self) -> Any:
        n = int(self.headers.get("Content-Length") or 0)
        if n <= 0:
            return None
        raw = self.rfile.read(n)
        if not raw:
            return None
        return json.loads(raw.decode("utf-8"))

    def _write(self, status: int, obj: Any, content_type: str = "application/json") -> None:
        body = b"" if obj is None else json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _write_ndjson(self, lines: list[dict[str, Any]]) -> None:
        payload = "".join(json.dumps(x) + "\n" for x in lines).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/x-ndjson")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._write(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        if u.path == "/healthz":
            self._write(200, {"status": "ok"})
            return
        if u.path == "/info":
            self._write(200, {"name": "grain", "version": "test"})
            return
        if u.path == "/vms":
            self._write(200, [{"name": "demo", "status": "running", "cpus": 2}])
            return
        if u.path.startswith("/vms/") and u.path.endswith("/agent/health"):
            self._write(
                200,
                {
                    "hostname": "demo",
                    "agent_version": "0.1.0",
                    "agent_uptime_sec": 12,
                    "userdata_ran": True,
                },
            )
            return
        if u.path.startswith("/vms/") and u.path.endswith("/stats"):
            self._write(
                200,
                {
                    "uptime_sec": 1.5,
                    "mem_total_bytes": 2048,
                    "mem_available_bytes": 1024,
                    "load1": 0.25,
                },
            )
            return
        if u.path.startswith("/vms/") and "/fs/readdir" in u.path:
            self._write(200, [{"name": "etc", "type": "directory", "size": 0, "mtime": 0, "mode": "0755"}])
            return
        if u.path.startswith("/vms/") and "/fs/stat" in u.path:
            self._write(200, {"name": "hello", "type": "file", "size": 5, "mtime": 1, "mode": "0644"})
            return
        if u.path.startswith("/vms/") and "/cp" in u.path:
            self.send_response(200)
            self.send_header("Content-Type", "application/octet-stream")
            self.send_header("Content-Length", "5")
            self.end_headers()
            self.wfile.write(b"hello")
            return
        if u.path.startswith("/vms/"):
            name = u.path.split("/")[2]
            self._write(200, {"name": name, "status": "running"})
            return
        self._write(404, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._write(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        qs = parse_qs(u.query)
        body = self._read_json()

        if u.path == "/vms":
            STATE.last_create_query = qs
            STATE.last_create_body = body or {}
            if qs.get("stream") == ["1"]:
                name = (body or {}).get("name") or "auto"
                wait = (qs.get("wait") or ["ssh"])[0]
                phases = [{"phase": "qemu", "message": "qemu"}]
                if wait == "agent":
                    phases.append({"phase": "wait_agent", "message": "wait agent"})
                else:
                    phases.append({"phase": "wait_ssh", "message": "wait ssh"})
                phases.append(
                    {
                        "phase": "ready",
                        "name": name,
                        "instance": {"name": name, "status": "running", "cpus": 2},
                    }
                )
                self._write_ndjson(phases)
                return
            name = (body or {}).get("name") or "auto"
            tags = {}
            if "wait" in qs:
                tags["wait"] = qs["wait"][0]
            if "timeout" in qs:
                tags["timeout"] = qs["timeout"][0]
            self._write(201, {"name": name, "status": "running", "cpus": 2, "tags": tags})
            return

        if u.path.endswith("/exec"):
            if qs.get("buffered") == ["true"]:
                self._write(
                    200,
                    {"stdout": "Linux\n", "stderr": "", "exit_code": 0},
                )
                return
            # stream
            frames = [
                {"type": "start", "pid": 1},
                {"type": "stdout", "data": "hi\n"},
                {"type": "exit", "exit_code": 0},
            ]
            self._write_ndjson(frames)
            return

        if u.path.endswith("/start") or u.path.endswith("/restore"):
            name = u.path.split("/")[2]
            self._write(200, {"name": name, "status": "running"})
            return
        if u.path.endswith("/shutdown") or u.path.endswith("/pause") or u.path.endswith("/resume") or u.path.endswith("/suspend"):
            self._write(200, {"message": "ok"})
            return
        if u.path.endswith("/forwards"):
            gp = (body or {}).get("guest_port", 80)
            hp = (body or {}).get("host_port") or 18080
            self._write(200, {"host_port": hp, "guest_port": gp, "pid": 99})
            return
        if u.path.endswith("/fs/mkdir"):
            self._write(200, {"ok": True})
            return
        self._write(404, {"error": "not found"})

    def do_DELETE(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._write(401, {"error": "unauthorized"})
            return
        u = urlparse(self.path)
        if u.path.startswith("/vms/") and "/forwards/" in u.path:
            self._write(200, {"ok": True})
            return
        if u.path.startswith("/vms/") and "/fs/remove" in u.path:
            self._write(200, {"ok": True})
            return
        if u.path.startswith("/vms/"):
            name = u.path.split("/")[2]
            STATE.deleted.append(name)
            self._write(200, {"message": "deleted", "name": name})
            return
        self._write(404, {"error": "not found"})

    def do_PUT(self) -> None:  # noqa: N802
        if not self._auth_ok():
            self._write(401, {"error": "unauthorized"})
            return
        n = int(self.headers.get("Content-Length") or 0)
        _ = self.rfile.read(n) if n else b""
        self._write(200, {"ok": True})


@pytest.fixture()
def server() -> Any:
    STATE.token = ""
    STATE.last_create_query = {}
    STATE.last_create_body = {}
    STATE.deleted = []
    httpd = HTTPServer(("127.0.0.1", 0), _Handler)
    t = threading.Thread(target=httpd.serve_forever, daemon=True)
    t.start()
    host, port = httpd.server_address[:2]
    yield f"http://{host}:{port}"
    httpd.shutdown()


def test_health_and_info(server: str) -> None:
    c = GrainClient(server)
    c.health()
    info = c.info()
    assert info["name"] == "grain"
    assert info["version"] == "test"


def test_list_get_delete(server: str) -> None:
    c = GrainClient(server)
    vms = c.list()
    assert len(vms) == 1
    assert vms[0].name == "demo"
    assert c.get("demo").status == "running"
    c.delete("demo")
    assert "demo" in STATE.deleted


def test_create_wait_timeout_query(server: str) -> None:
    c = GrainClient(server)
    inst = c.create(
        CreateRequest(name="lab", cpus=2, mounts=[Mount(host="/tmp", guest="/work")]),
        wait=WAIT_AGENT,
        timeout="3m",
    )
    assert inst.name == "lab"
    assert inst.tags.get("wait") == "agent"
    assert inst.tags.get("timeout") == "3m"
    assert STATE.last_create_body.get("name") == "lab"
    assert STATE.last_create_body.get("mounts")[0]["guest"] == "/work"


def test_create_stream(server: str) -> None:
    c = GrainClient(server)
    phases: list[str] = []
    inst = c.create_stream(
        CreateRequest(name="streamed"),
        on_event=lambda ev: phases.append(ev.phase),
        wait="agent",
    )
    assert inst.name == "streamed"
    assert "ready" in phases
    assert "wait_agent" in phases


def test_lifecycle(server: str) -> None:
    c = GrainClient(server)
    assert c.start("x").status == "running"
    c.pause("x")
    c.resume("x")
    c.suspend("x")
    assert c.restore("x").name == "x"
    c.shutdown("x")
    c.stop("y")


def test_exec_and_stream(server: str) -> None:
    c = GrainClient(server)
    r = c.exec("demo", "uname", ["-a"])
    assert r.exit_code == 0
    assert "Linux" in r.stdout

    chunks: list[str] = []
    code = c.exec_stream(
        "demo",
        ExecOpts(cmd="sh", args=["-c", "echo hi"]),
        on_frame=lambda f: chunks.append(f.data or "") if f.type == "stdout" else None,
    )
    assert code == 0
    assert any("hi" in x for x in chunks)


def test_agent_health_stats_fs(server: str) -> None:
    c = GrainClient(server)
    h = c.agent_health("demo")
    assert h.agent_version == "0.1.0"
    assert h.userdata_ran
    st = c.stats("demo")
    assert st.load1 == 0.25
    entries = c.readdir("demo", "/")
    assert entries[0].type == "directory"
    info = c.stat("demo", "/hello")
    assert info.size == 5
    c.mkdir("demo", "/tmp/x", recursive=True, mode="0755")
    c.remove("demo", "/tmp/x", recursive=True)


def test_forwards_and_files(server: str) -> None:
    c = GrainClient(server)
    lf = c.add_forward("demo", 0, 80)
    assert lf.guest_port == 80
    assert lf.host_port == 18080
    c.remove_forward("demo", lf.host_port)
    c.put_file("demo", "/tmp/f", b"hello", mode="0644")
    assert c.get_file("demo", "/tmp/f") == b"hello"


def test_auth_required(server: str) -> None:
    STATE.token = "secret"
    c = GrainClient(server)  # no token
    with pytest.raises(GrainAPIError) as ei:
        c.info()
    assert ei.value.status == 401

    c2 = GrainClient(server, token="secret")
    c2.health()  # healthz unauthenticated
    assert c2.info()["name"] == "grain"


def test_http_constructor() -> None:
    c = GrainClient.http("http://127.0.0.1:9")
    assert c.base_url == "http://127.0.0.1:9"
