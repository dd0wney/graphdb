from __future__ import annotations

from typing import Any

from ..transport import AsyncTransport


def _as_dict(data: Any) -> dict[str, Any]:
    return data if isinstance(data, dict) else {}


class AsyncSecurityResource:
    """Admin security operations. All methods return the server's raw JSON (a
    freeform dict, or a list for audit_export); the shapes are not stable enough
    to type. Admin-only — a non-admin token raises AuthError (403)."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._t = transport

    async def rotate_keys(self) -> dict[str, Any]:
        """Rotate encryption keys (POST /api/v1/security/keys/rotate)."""
        return _as_dict((await self._t.request("POST", "/api/v1/security/keys/rotate")).data)

    async def key_info(self) -> dict[str, Any]:
        """Encryption key info (GET /api/v1/security/keys/info)."""
        return _as_dict((await self._t.request("GET", "/api/v1/security/keys/info")).data)

    async def audit_logs(self, *, limit: int | None = None) -> dict[str, Any]:
        """In-memory security audit logs (GET /api/v1/security/audit/logs)."""
        params: dict[str, Any] = {}
        if limit is not None:
            params["limit"] = limit
        res = await self._t.request("GET", "/api/v1/security/audit/logs", params=params)
        return _as_dict(res.data)

    async def audit_export(self) -> list[dict[str, Any]]:
        """Export the audit-log events (POST /api/v1/security/audit/export). The
        server encodes a JSON array of event records; returned in-memory (no file)."""
        data = (await self._t.request("POST", "/api/v1/security/audit/export")).data
        return data if isinstance(data, list) else []

    async def health(self) -> dict[str, Any]:
        """Security component health (GET /api/v1/security/health)."""
        return _as_dict((await self._t.request("GET", "/api/v1/security/health")).data)
