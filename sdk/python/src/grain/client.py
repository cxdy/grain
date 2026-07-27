"""HTTP client for the grain daemon (TCP or Unix socket)."""

from __future__ import annotations

import json
import socket
import ssl
from http.client import HTTPConnection, HTTPSConnection, HTTPResponse
from typing import Any, BinaryIO, Callable, Iterator, Mapping, Optional, Union
from urllib.parse import quote, urlencode, urlparse

from .errors import GrainAPIError
from .types import (
    PHASE_ERROR,
    PHASE_READY,
    AgentHealth,
    CreateEvent,
    CreateRequest,
    ExecFrame,
    ExecOpts,
    ExecResult,
    FSInfo,
    Instance,
    LiveForward,
    Stats,
)

# Default TCP base matches grain config `api: 127.0.0.1:7474`.
DEFAULT_BASE_URL = "http://127.0.0.1:7474"


class _UnixHTTPConnection(HTTPConnection):
    """HTTPConnection over a Unix domain socket."""

    def __init__(self, socket_path: str, timeout: Optional[float] = None) -> None:
        super().__init__("localhost", timeout=timeout)
        self.socket_path = socket_path

    def connect(self) -> None:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        if self.timeout is not socket._GLOBAL_DEFAULT_TIMEOUT:  # type: ignore[attr-defined]
            sock.settimeout(self.timeout)
        sock.connect(self.socket_path)
        self.sock = sock


class GrainClient:
    """Thin HTTP client for a running grain daemon.

    Examples
    --------
    TCP (default config)::

        grain = GrainClient(base_url="http://127.0.0.1:7474")
        grain.health()
        inst = grain.create(CreateRequest(persistent=False), wait="agent", timeout="3m")
        result = grain.exec(inst.name, "uname", ["-a"])
        grain.delete(inst.name)

    Unix socket (CLI default)::

        from pathlib import Path
        grain = GrainClient.unix(Path.home() / ".grain" / "grain.sock")
    """

    def __init__(
        self,
        base_url: str = DEFAULT_BASE_URL,
        *,
        token: str = "",
        socket_path: Optional[str] = None,
        timeout: Optional[float] = 300.0,
    ) -> None:
        base = (base_url or DEFAULT_BASE_URL).rstrip("/")
        if not base:
            raise ValueError("base_url is required")
        parsed = urlparse(base)
        if parsed.scheme not in ("http", "https"):
            raise ValueError(f"unsupported URL scheme: {parsed.scheme!r}")
        self.base_url = base
        self._scheme = parsed.scheme
        self._host = parsed.hostname or "localhost"
        self._port = parsed.port or (443 if parsed.scheme == "https" else 80)
        self._token = token or ""
        self._socket_path = socket_path
        self._timeout = timeout

    # ── constructors ───────────────────────────────────────────────────────

    @classmethod
    def unix(
        cls,
        socket_path: str,
        *,
        token: str = "",
        timeout: Optional[float] = 300.0,
    ) -> GrainClient:
        """Connect via Unix domain socket (typical local CLI path)."""
        if not socket_path:
            raise ValueError("socket_path is required")
        return cls(
            base_url="http://grain",
            token=token,
            socket_path=socket_path,
            timeout=timeout,
        )

    @classmethod
    def http(
        cls,
        base_url: str,
        *,
        token: str = "",
        timeout: Optional[float] = 300.0,
    ) -> GrainClient:
        """Connect over TCP HTTP(S)."""
        return cls(base_url=base_url, token=token, timeout=timeout)

    def set_token(self, token: str) -> None:
        self._token = token or ""

    @property
    def token(self) -> str:
        return self._token

    # ── system ─────────────────────────────────────────────────────────────

    def health(self) -> None:
        """GET /healthz — raise if not healthy."""
        status, body = self._request("GET", "/healthz", auth=False)
        if status != 200:
            raise GrainAPIError(status, "unhealthy", body.decode("utf-8", "replace"))

    def info(self) -> dict[str, str]:
        """GET /info — daemon name + version."""
        return self._json("GET", "/info")

    # ── VMs ────────────────────────────────────────────────────────────────

    def list(self) -> list[Instance]:
        data = self._json("GET", "/vms")
        if not isinstance(data, list):
            raise GrainAPIError(0, "list: expected JSON array")
        return [Instance.from_dict(x) for x in data]

    def create(
        self,
        req: Optional[CreateRequest] = None,
        *,
        wait: Optional[str] = None,
        timeout: Optional[str] = None,
    ) -> Instance:
        """POST /vms — blocking create until ready."""
        req = req or CreateRequest()
        wait = wait if wait is not None else req.wait
        timeout = timeout if timeout is not None else req.timeout
        path = _vms_create_path(stream=False, wait=wait, timeout=timeout)
        data = self._json("POST", path, body=req.to_body())
        return Instance.from_dict(data)

    def create_stream(
        self,
        req: Optional[CreateRequest] = None,
        on_event: Optional[Callable[[CreateEvent], None]] = None,
        *,
        wait: Optional[str] = None,
        timeout: Optional[str] = None,
    ) -> Instance:
        """POST /vms?stream=1 — NDJSON progress; return instance from ready event."""
        req = req or CreateRequest()
        wait = wait if wait is not None else req.wait
        timeout = timeout if timeout is not None else req.timeout
        path = _vms_create_path(stream=True, wait=wait, timeout=timeout)
        status, body_iter = self._request_stream(
            "POST",
            path,
            body=req.to_body(),
            headers={"Accept": "application/x-ndjson"},
        )
        if status >= 300:
            raw = b"".join(body_iter)
            raise self._error_from_body(status, raw)

        inst: Optional[Instance] = None
        stream_err: Optional[Exception] = None
        for line in _iter_lines(body_iter):
            if not line.strip():
                continue
            try:
                ev = CreateEvent.from_dict(json.loads(line))
            except json.JSONDecodeError as e:
                raise GrainAPIError(0, f"decode create event: {line}") from e
            if on_event is not None:
                on_event(ev)
            if ev.phase == PHASE_READY:
                if ev.instance is not None:
                    inst = ev.instance
                elif ev.name:
                    inst = Instance(name=ev.name, status="running", ssh_port=ev.ssh_port)
            elif ev.phase == PHASE_ERROR:
                stream_err = GrainAPIError(0, ev.error or ev.message or "create failed")

        if stream_err is not None:
            raise stream_err
        if inst is None:
            raise GrainAPIError(0, "create stream ended without ready event")
        return inst

    def get(self, name: str) -> Instance:
        return Instance.from_dict(self._json("GET", f"/vms/{_enc(name)}"))

    def delete(self, name: str) -> None:
        self._empty("DELETE", f"/vms/{_enc(name)}")

    def start(self, name: str) -> Instance:
        return Instance.from_dict(self._json("POST", f"/vms/{_enc(name)}/start"))

    def stop(self, name: str) -> None:
        """Alias for :meth:`shutdown`."""
        self.shutdown(name)

    def shutdown(self, name: str) -> None:
        self._empty("POST", f"/vms/{_enc(name)}/shutdown")

    def pause(self, name: str) -> None:
        self._empty("POST", f"/vms/{_enc(name)}/pause")

    def resume(self, name: str) -> None:
        self._empty("POST", f"/vms/{_enc(name)}/resume")

    def suspend(self, name: str) -> None:
        self._empty("POST", f"/vms/{_enc(name)}/suspend")

    def restore(self, name: str) -> Instance:
        return Instance.from_dict(self._json("POST", f"/vms/{_enc(name)}/restore"))

    def add_forward(self, name: str, host_port: int, guest_port: int) -> LiveForward:
        data = self._json(
            "POST",
            f"/vms/{_enc(name)}/forwards",
            body={"host_port": host_port, "guest_port": guest_port},
        )
        return LiveForward.from_dict(data)

    def remove_forward(self, name: str, host_port: int) -> None:
        self._empty("DELETE", f"/vms/{_enc(name)}/forwards/{int(host_port)}")

    # ── agent ──────────────────────────────────────────────────────────────

    def exec(
        self,
        name: str,
        cmd: str,
        args: Optional[list[str]] = None,
        *,
        uid: Optional[int] = None,
        gid: Optional[int] = None,
        cwd: Optional[str] = None,
    ) -> ExecResult:
        """Buffered exec. Non-zero guest exit codes are returned, not raised."""
        if not cmd:
            raise ValueError("cmd is required")
        q: list[tuple[str, str]] = [("cmd", cmd), ("buffered", "true")]
        for a in args or []:
            q.append(("args", a))
        if uid is not None:
            q.append(("uid", str(uid)))
        if gid is not None:
            q.append(("gid", str(gid)))
        if cwd:
            q.append(("cwd", cwd))
        path = f"/vms/{_enc(name)}/exec?{urlencode(q)}"
        status, raw = self._request("POST", path)
        text = raw.decode("utf-8", "replace")
        if status >= 300:
            raise self._error_from_body(status, raw)
        return ExecResult.from_dict(json.loads(text) if text else {})

    def exec_stream(
        self,
        name: str,
        opts: ExecOpts,
        on_frame: Callable[[ExecFrame], None],
    ) -> int:
        """Streaming exec (NDJSON). Returns the final exit code."""
        if not opts.cmd:
            raise ValueError("cmd is required")
        if on_frame is None:
            raise ValueError("on_frame is required")
        q: list[tuple[str, str]] = [("cmd", opts.cmd), ("buffered", "false")]
        for a in opts.args or []:
            q.append(("args", a))
        if opts.uid is not None:
            q.append(("uid", str(opts.uid)))
        if opts.gid is not None:
            q.append(("gid", str(opts.gid)))
        if opts.cwd:
            q.append(("cwd", opts.cwd))
        path = f"/vms/{_enc(name)}/exec?{urlencode(q)}"
        status, body_iter = self._request_stream("POST", path)
        if status >= 300:
            raw = b"".join(body_iter)
            raise self._error_from_body(status, raw)

        got_exit = False
        code = -1
        for line in _iter_lines(body_iter):
            if not line.strip():
                continue
            try:
                frame = ExecFrame.from_dict(json.loads(line))
            except json.JSONDecodeError as e:
                raise GrainAPIError(0, f"exec stream frame decode: {line}") from e
            on_frame(frame)
            if frame.type == "exit":
                got_exit = True
                if frame.exit_code is not None:
                    code = frame.exit_code
            elif frame.type == "error":
                raise GrainAPIError(0, frame.error or "exec stream error")
        if not got_exit:
            raise GrainAPIError(0, "exec stream ended without exit frame")
        return code

    def agent_health(self, name: str) -> AgentHealth:
        return AgentHealth.from_dict(self._json("GET", f"/vms/{_enc(name)}/agent/health"))

    def stats(self, name: str) -> Stats:
        return Stats.from_dict(self._json("GET", f"/vms/{_enc(name)}/stats"))

    # ── filesystem (agent) ─────────────────────────────────────────────────

    def readdir(self, name: str, guest_path: str) -> list[FSInfo]:
        if not guest_path:
            raise ValueError("path is required")
        path = f"/vms/{_enc(name)}/fs/readdir?{urlencode({'path': guest_path})}"
        data = self._json("GET", path)
        if not isinstance(data, list):
            raise GrainAPIError(0, "readdir: expected JSON array")
        return [FSInfo.from_dict(x) for x in data]

    def stat(self, name: str, guest_path: str) -> FSInfo:
        if not guest_path:
            raise ValueError("path is required")
        path = f"/vms/{_enc(name)}/fs/stat?{urlencode({'path': guest_path})}"
        return FSInfo.from_dict(self._json("GET", path))

    def mkdir(
        self,
        name: str,
        guest_path: str,
        *,
        recursive: bool = False,
        mode: str = "",
    ) -> None:
        if not guest_path:
            raise ValueError("path is required")
        body: dict[str, Any] = {"path": guest_path, "recursive": recursive}
        if mode:
            body["mode"] = mode
        self._empty("POST", f"/vms/{_enc(name)}/fs/mkdir", body=body)

    def remove(self, name: str, guest_path: str, *, recursive: bool = False) -> None:
        if not guest_path:
            raise ValueError("path is required")
        q: dict[str, str] = {"path": guest_path}
        if recursive:
            q["recursive"] = "true"
        self._empty("DELETE", f"/vms/{_enc(name)}/fs/remove?{urlencode(q)}")

    def put_file(
        self,
        name: str,
        guest_path: str,
        data: Union[bytes, BinaryIO],
        *,
        mode: str = "",
        uid: Optional[int] = None,
        gid: Optional[int] = None,
    ) -> None:
        """PUT /vms/{name}/cp?mode=binary — upload bytes or a file-like object."""
        if not guest_path:
            raise ValueError("path is required")
        q: list[tuple[str, str]] = [("path", guest_path), ("mode", "binary")]
        if mode:
            q.append(("permissions", mode))
        if uid is not None:
            q.append(("uid", str(uid)))
        if gid is not None:
            q.append(("gid", str(gid)))
        path = f"/vms/{_enc(name)}/cp?{urlencode(q)}"
        if isinstance(data, (bytes, bytearray)):
            body_bytes: Optional[bytes] = bytes(data)
            body_file: Optional[BinaryIO] = None
        else:
            body_bytes = None
            body_file = data  # type: ignore[assignment]
        status, raw = self._request(
            "PUT",
            path,
            body_bytes=body_bytes,
            body_file=body_file,
            content_type="application/octet-stream",
        )
        if status >= 300:
            raise self._error_from_body(status, raw)

    def get_file(self, name: str, guest_path: str) -> bytes:
        """GET /vms/{name}/cp?mode=binary — download guest file as bytes."""
        if not guest_path:
            raise ValueError("path is required")
        path = f"/vms/{_enc(name)}/cp?{urlencode({'path': guest_path, 'mode': 'binary'})}"
        status, raw = self._request("GET", path)
        if status >= 300:
            raise self._error_from_body(status, raw)
        return raw

    # ── HTTP internals ─────────────────────────────────────────────────────

    def _connect(self) -> HTTPConnection:
        if self._socket_path:
            return _UnixHTTPConnection(self._socket_path, timeout=self._timeout)
        if self._scheme == "https":
            ctx = ssl.create_default_context()
            return HTTPSConnection(self._host, self._port, timeout=self._timeout, context=ctx)
        return HTTPConnection(self._host, self._port, timeout=self._timeout)

    def _request(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        body_bytes: Optional[bytes] = None,
        body_file: Optional[BinaryIO] = None,
        headers: Optional[Mapping[str, str]] = None,
        content_type: Optional[str] = None,
        auth: bool = True,
    ) -> tuple[int, bytes]:
        hdrs = dict(headers or {})
        if auth and self._token:
            hdrs["Authorization"] = f"Bearer {self._token}"
        payload: Optional[bytes] = body_bytes
        if body is not None:
            payload = json.dumps(body).encode("utf-8")
            hdrs.setdefault("Content-Type", "application/json")
        if content_type:
            hdrs["Content-Type"] = content_type

        conn = self._connect()
        try:
            if body_file is not None:
                # Read into memory for Content-Length (simple + portable).
                data = body_file.read()
                if isinstance(data, str):
                    data = data.encode("utf-8")
                payload = data
            conn.request(method, path, body=payload, headers=hdrs)
            res = conn.getresponse()
            raw = res.read()
            return res.status, raw
        finally:
            conn.close()

    def _request_stream(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        headers: Optional[Mapping[str, str]] = None,
        auth: bool = True,
    ) -> tuple[int, Iterator[bytes]]:
        hdrs = dict(headers or {})
        if auth and self._token:
            hdrs["Authorization"] = f"Bearer {self._token}"
        payload: Optional[bytes] = None
        if body is not None:
            payload = json.dumps(body).encode("utf-8")
            hdrs.setdefault("Content-Type", "application/json")

        conn = self._connect()
        try:
            conn.request(method, path, body=payload, headers=hdrs)
            res = conn.getresponse()

            def gen() -> Iterator[bytes]:
                try:
                    while True:
                        chunk = res.read(8192)
                        if not chunk:
                            break
                        yield chunk
                finally:
                    conn.close()

            return res.status, gen()
        except Exception:
            conn.close()
            raise

    def _json(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        headers: Optional[Mapping[str, str]] = None,
        auth: bool = True,
    ) -> Any:
        status, raw = self._request(method, path, body=body, headers=headers, auth=auth)
        if status >= 300:
            raise self._error_from_body(status, raw)
        if not raw:
            return {}
        return json.loads(raw.decode("utf-8"))

    def _empty(
        self,
        method: str,
        path: str,
        *,
        body: Any = None,
        auth: bool = True,
    ) -> None:
        status, raw = self._request(method, path, body=body, auth=auth)
        if status >= 300:
            raise self._error_from_body(status, raw)

    def _error_from_body(self, status: int, raw: bytes) -> GrainAPIError:
        text = raw.decode("utf-8", "replace").strip()
        msg = f"status {status}"
        if text:
            try:
                e = json.loads(text)
                if isinstance(e, dict) and e.get("error"):
                    msg = str(e["error"])
                else:
                    msg = text
            except json.JSONDecodeError:
                msg = text
        return GrainAPIError(status, msg, text or None)


def _enc(name: str) -> str:
    return quote(name, safe="")


def _vms_create_path(
    *,
    stream: bool,
    wait: Optional[str],
    timeout: Optional[str],
) -> str:
    q: list[tuple[str, str]] = []
    if stream:
        q.append(("stream", "1"))
    if wait:
        q.append(("wait", wait))
    if timeout:
        q.append(("timeout", timeout))
    if not q:
        return "/vms"
    return f"/vms?{urlencode(q)}"


def _iter_lines(chunks: Iterator[bytes]) -> Iterator[str]:
    buf = b""
    for chunk in chunks:
        buf += chunk
        while True:
            idx = buf.find(b"\n")
            if idx < 0:
                break
            line = buf[:idx]
            buf = buf[idx + 1 :]
            yield line.decode("utf-8", "replace")
    if buf:
        yield buf.decode("utf-8", "replace")
