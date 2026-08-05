"""Python client for the grain microVM daemon HTTP API.

Mirrors the Go client (``github.com/cxdy/grain/client``) and TypeScript
SDK (``@cxdy/grain``). Stdlib only — no third-party runtime deps.
"""

from .client import GrainClient
from .errors import GrainAPIError
from .types import (
    PHASE_BOOTSTRAP,
    PHASE_DISK,
    PHASE_ERROR,
    PHASE_IMAGE,
    PHASE_QEMU,
    PHASE_READY,
    PHASE_SEED,
    PHASE_USERDATA,
    PHASE_WAIT_AGENT,
    PHASE_WAIT_SSH,
    WAIT_AGENT,
    WAIT_AUTO,
    WAIT_BOOTSTRAP,
    WAIT_SSH,
    WAIT_USERDATA,
    AgentHealth,
    CreateEvent,
    CreateRequest,
    ExecFrame,
    ExecOpts,
    ExecResult,
    FSInfo,
    Instance,
    LiveForward,
    Mount,
    PortForward,
    SocketForward,
    Stats,
)

__all__ = [
    "GrainClient",
    "GrainAPIError",
    "Instance",
    "CreateRequest",
    "CreateEvent",
    "ExecResult",
    "ExecFrame",
    "ExecOpts",
    "AgentHealth",
    "Stats",
    "PortForward",
    "LiveForward",
    "Mount",
    "SocketForward",
    "FSInfo",
    "WAIT_AUTO",
    "WAIT_SSH",
    "WAIT_AGENT",
    "WAIT_USERDATA",
    "WAIT_BOOTSTRAP",
    "PHASE_IMAGE",
    "PHASE_DISK",
    "PHASE_SEED",
    "PHASE_QEMU",
    "PHASE_WAIT_SSH",
    "PHASE_WAIT_AGENT",
    "PHASE_USERDATA",
    "PHASE_BOOTSTRAP",
    "PHASE_READY",
    "PHASE_ERROR",
]

__version__ = "0.1.0"
