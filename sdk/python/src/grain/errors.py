"""Errors raised by the grain Python client."""

from __future__ import annotations


class GrainAPIError(Exception):
    """Non-2xx response from the grain daemon HTTP API."""

    def __init__(self, status: int, message: str, body: str | None = None) -> None:
        super().__init__(message)
        self.status = status
        self.body = body

    def __str__(self) -> str:  # pragma: no cover - trivial
        if self.status:
            return f"[{self.status}] {super().__str__()}"
        return super().__str__()
