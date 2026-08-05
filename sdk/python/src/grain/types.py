"""Typed structures mirroring the grain daemon JSON (api/openapi.yaml)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal, Optional

# Create wait modes (query params on POST /vms).
WAIT_AUTO = "auto"
WAIT_SSH = "ssh"
WAIT_AGENT = "agent"
WAIT_USERDATA = "userdata"
WAIT_BOOTSTRAP = "bootstrap"

# Create stream phases (NDJSON).
PHASE_IMAGE = "image"
PHASE_DISK = "disk"
PHASE_SEED = "seed"
PHASE_QEMU = "qemu"
PHASE_WAIT_SSH = "wait_ssh"
PHASE_WAIT_AGENT = "wait_agent"
PHASE_USERDATA = "userdata"
PHASE_BOOTSTRAP = "bootstrap"
PHASE_READY = "ready"
PHASE_ERROR = "error"

Status = Literal[
    "creating",
    "running",
    "paused",
    "suspended",
    "stopped",
    "error",
]


@dataclass
class PortForward:
    host_port: int = 0
    guest_port: int = 0
    proto: Optional[str] = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "host_port": self.host_port,
            "guest_port": self.guest_port,
        }
        if self.proto:
            d["proto"] = self.proto
        return d

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> PortForward:
        return cls(
            host_port=int(d.get("host_port") or 0),
            guest_port=int(d.get("guest_port") or 0),
            proto=d.get("proto"),
        )


@dataclass
class LiveForward:
    host_port: int = 0
    guest_port: int = 0
    pid: int = 0

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> LiveForward:
        return cls(
            host_port=int(d.get("host_port") or 0),
            guest_port=int(d.get("guest_port") or 0),
            pid=int(d.get("pid") or 0),
        )


@dataclass
class Mount:
    host: str
    guest: str
    tag: Optional[str] = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {"host": self.host, "guest": self.guest}
        if self.tag:
            d["tag"] = self.tag
        return d

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> Mount:
        return cls(host=str(d.get("host") or ""), guest=str(d.get("guest") or ""), tag=d.get("tag"))


@dataclass
class SocketForward:
    host_path: str
    guest_path: str
    pid: int = 0

    def to_dict(self) -> dict[str, Any]:
        return {"host_path": self.host_path, "guest_path": self.guest_path}

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> SocketForward:
        return cls(
            host_path=str(d.get("host_path") or ""),
            guest_path=str(d.get("guest_path") or ""),
            pid=int(d.get("pid") or 0),
        )


@dataclass
class Instance:
    name: str
    status: str = "creating"
    persistent: bool = False
    cpus: int = 0
    memory_mb: int = 0
    disk_gb: int = 0
    image: str = ""
    ip: Optional[str] = None
    ssh_port: int = 0
    agent_port: int = 0
    agent_cid: int = 0
    forwards: list[PortForward] = field(default_factory=list)
    live_forwards: list[LiveForward] = field(default_factory=list)
    mounts: list[Mount] = field(default_factory=list)
    socket_forwards: list[SocketForward] = field(default_factory=list)
    tags: dict[str, str] = field(default_factory=dict)
    created_at: Optional[str] = None
    suspended_at: Optional[str] = None
    error: Optional[str] = None
    disk_path: Optional[str] = None
    pid: int = 0

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> Instance:
        return cls(
            name=str(d.get("name") or ""),
            status=str(d.get("status") or "creating"),
            persistent=bool(d.get("persistent")),
            cpus=int(d.get("cpus") or 0),
            memory_mb=int(d.get("memory_mb") or 0),
            disk_gb=int(d.get("disk_gb") or 0),
            image=str(d.get("image") or ""),
            ip=d.get("ip"),
            ssh_port=int(d.get("ssh_port") or 0),
            agent_port=int(d.get("agent_port") or 0),
            agent_cid=int(d.get("agent_cid") or 0),
            forwards=[PortForward.from_dict(x) for x in (d.get("forwards") or [])],
            live_forwards=[LiveForward.from_dict(x) for x in (d.get("live_forwards") or [])],
            mounts=[Mount.from_dict(x) for x in (d.get("mounts") or [])],
            socket_forwards=[SocketForward.from_dict(x) for x in (d.get("socket_forwards") or [])],
            tags=dict(d.get("tags") or {}),
            created_at=d.get("created_at"),
            suspended_at=d.get("suspended_at"),
            error=d.get("error"),
            disk_path=d.get("disk_path"),
            pid=int(d.get("pid") or 0),
        )


@dataclass
class CreateRequest:
    """JSON body for POST /vms. Wait/timeout are sent as query parameters."""

    name: Optional[str] = None
    persistent: bool = False
    cpus: Optional[int] = None
    memory_mb: Optional[int] = None
    disk_gb: Optional[int] = None
    image: Optional[str] = None
    tags: Optional[dict[str, str]] = None
    userdata: Optional[str] = None
    forwards: Optional[list[PortForward]] = None
    mounts: Optional[list[Mount]] = None
    socket_forwards: Optional[list[SocketForward]] = None
    # Query params (not JSON body) — set on GrainClient.create / create_stream.
    wait: Optional[str] = None
    timeout: Optional[str] = None

    def to_body(self) -> dict[str, Any]:
        d: dict[str, Any] = {"persistent": self.persistent}
        if self.name:
            d["name"] = self.name
        if self.cpus is not None:
            d["cpus"] = self.cpus
        if self.memory_mb is not None:
            d["memory_mb"] = self.memory_mb
        if self.disk_gb is not None:
            d["disk_gb"] = self.disk_gb
        if self.image:
            d["image"] = self.image
        if self.tags:
            d["tags"] = self.tags
        if self.userdata:
            d["userdata"] = self.userdata
        if self.forwards:
            d["forwards"] = [f.to_dict() for f in self.forwards]
        if self.mounts:
            d["mounts"] = [m.to_dict() for m in self.mounts]
        if self.socket_forwards:
            d["socket_forwards"] = [s.to_dict() for s in self.socket_forwards]
        return d


@dataclass
class CreateEvent:
    phase: str
    message: Optional[str] = None
    name: Optional[str] = None
    error: Optional[str] = None
    ssh_port: int = 0
    instance: Optional[Instance] = None

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> CreateEvent:
        inst = d.get("instance")
        return cls(
            phase=str(d.get("phase") or ""),
            message=d.get("message"),
            name=d.get("name"),
            error=d.get("error"),
            ssh_port=int(d.get("ssh_port") or 0),
            instance=Instance.from_dict(inst) if isinstance(inst, dict) else None,
        )


@dataclass
class Stats:
    uptime_sec: float = 0.0
    mem_total_bytes: int = 0
    mem_available_bytes: int = 0
    load1: float = 0.0
    disk_total_bytes: int = 0
    disk_free_bytes: int = 0

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> Stats:
        return cls(
            uptime_sec=float(d.get("uptime_sec") or 0),
            mem_total_bytes=int(d.get("mem_total_bytes") or 0),
            mem_available_bytes=int(d.get("mem_available_bytes") or 0),
            load1=float(d.get("load1") or 0),
            disk_total_bytes=int(d.get("disk_total_bytes") or 0),
            disk_free_bytes=int(d.get("disk_free_bytes") or 0),
        )


@dataclass
class AgentHealth:
    hostname: str = ""
    agent_version: str = ""
    agent_uptime_sec: int = 0
    userdata_ran: bool = False

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> AgentHealth:
        return cls(
            hostname=str(d.get("hostname") or ""),
            agent_version=str(d.get("agent_version") or ""),
            agent_uptime_sec=int(d.get("agent_uptime_sec") or 0),
            userdata_ran=bool(d.get("userdata_ran")),
        )


@dataclass
class ExecResult:
    stdout: str = ""
    stderr: str = ""
    exit_code: int = 0
    error: Optional[str] = None

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> ExecResult:
        return cls(
            stdout=str(d.get("stdout") or ""),
            stderr=str(d.get("stderr") or ""),
            exit_code=int(d.get("exit_code") or 0),
            error=d.get("error"),
        )


@dataclass
class ExecFrame:
    type: str
    timestamp: Optional[str] = None
    pid: int = 0
    data: Optional[str] = None
    exit_code: Optional[int] = None
    error: Optional[str] = None
    started_at: Optional[str] = None
    ended_at: Optional[str] = None

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> ExecFrame:
        ec = d.get("exit_code")
        return cls(
            type=str(d.get("type") or ""),
            timestamp=d.get("timestamp"),
            pid=int(d.get("pid") or 0),
            data=d.get("data"),
            exit_code=int(ec) if ec is not None else None,
            error=d.get("error"),
            started_at=d.get("started_at"),
            ended_at=d.get("ended_at"),
        )


@dataclass
class ExecOpts:
    cmd: str
    args: Optional[list[str]] = None
    uid: Optional[int] = None
    gid: Optional[int] = None
    cwd: Optional[str] = None


@dataclass
class FSInfo:
    name: str = ""
    type: str = ""  # file|directory|symlink
    size: int = 0
    mtime: int = 0
    mode: str = ""

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> FSInfo:
        return cls(
            name=str(d.get("name") or ""),
            type=str(d.get("type") or ""),
            size=int(d.get("size") or 0),
            mtime=int(d.get("mtime") or 0),
            mode=str(d.get("mode") or ""),
        )
