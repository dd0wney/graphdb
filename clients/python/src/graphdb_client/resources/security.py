from __future__ import annotations

from typing import Any

from .._transport import Transport


def _as_dict(data: Any) -> dict[str, Any]:
    return data if isinstance(data, dict) else {}


class SecurityResource:
    """Admin security operations. All methods return the server's raw JSON (a
    freeform dict, or a list for audit_export); the shapes are not stable enough
    to type. Admin-only — a non-admin token raises AuthError (403)."""

    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def rotate_keys(self) -> dict[str, Any]:
        """Rotate encryption keys (POST /api/v1/security/keys/rotate)."""
        return _as_dict(self._t.request("POST", "/api/v1/security/keys/rotate").data)

    def key_info(self) -> dict[str, Any]:
        """Encryption key info (GET /api/v1/security/keys/info)."""
        return _as_dict(self._t.request("GET", "/api/v1/security/keys/info").data)

    def audit_logs(self, *, limit: int | None = None) -> dict[str, Any]:
        """In-memory security audit logs (GET /api/v1/security/audit/logs)."""
        params: dict[str, Any] = {}
        if limit is not None:
            params["limit"] = limit
        return _as_dict(self._t.request("GET", "/api/v1/security/audit/logs", params=params).data)

    def audit_export(self) -> list[dict[str, Any]]:
        """Export the audit-log events (GET /api/v1/security/audit/export). The
        server encodes a JSON array of event records; returned in-memory (no file)."""
        data = self._t.request("GET", "/api/v1/security/audit/export").data
        return data if isinstance(data, list) else []

    def health(self) -> dict[str, Any]:
        """Security component health (GET /api/v1/security/health)."""
        return _as_dict(self._t.request("GET", "/api/v1/security/health").data)
