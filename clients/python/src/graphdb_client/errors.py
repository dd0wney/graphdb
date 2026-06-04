from __future__ import annotations

from typing import Any


class GraphDBError(Exception):
    """Base error for all graphdb client failures."""

    def __init__(
        self,
        message: str,
        *,
        status_code: int | None = None,
        body: Any = None,
        method: str | None = None,
        path: str | None = None,
    ) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.body = body
        self.method = method
        self.path = path


class ValidationError(GraphDBError):
    """400 — request rejected by validation."""


class AuthError(GraphDBError):
    """401 — missing/invalid/expired credentials (after refresh attempt)."""


class NotFoundError(GraphDBError):
    """404 — node/edge not found, or cross-tenant (unified error)."""


class ConflictError(GraphDBError):
    """409 — unique-constraint violation."""


class RateLimitError(GraphDBError):
    """429 — rate limited."""


class ServerError(GraphDBError):
    """5xx — server-side failure."""


_STATUS_MAP: dict[int, type[GraphDBError]] = {
    400: ValidationError,
    401: AuthError,
    404: NotFoundError,
    409: ConflictError,
    429: RateLimitError,
}


def _extract_message(body: Any) -> str:
    if isinstance(body, dict):
        for key in ("error", "message", "detail"):
            val = body.get(key)
            # Accept any present, non-empty value (incl. falsy like 0/False);
            # skip None and whitespace-only.
            if val is not None and str(val).strip():
                return str(val).strip()
    if isinstance(body, str) and body.strip():
        return body
    return "request failed"


def from_response(status_code: int, body: Any, method: str, path: str) -> GraphDBError:
    if status_code >= 500:
        cls: type[GraphDBError] = ServerError
    else:
        cls = _STATUS_MAP.get(status_code, GraphDBError)
    msg = f"{method} {path} -> {status_code}: {_extract_message(body)}"
    return cls(msg, status_code=status_code, body=body, method=method, path=path)
