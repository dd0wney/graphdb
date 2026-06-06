from __future__ import annotations

from typing import Any, Mapping

from .._transport import Transport


def _as_dict(data: Any) -> dict[str, Any]:
    return data if isinstance(data, dict) else {}


class ComplianceResource:
    """Compliance operations (audit log + masking policy). Returns the server's
    raw JSON dict. Note: these live under /v1/compliance/... (NOT /api/v1/...)."""

    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def audit_log(
        self,
        *,
        user_id: str | None = None,
        username: str | None = None,
        action: str | None = None,
        resource_type: str | None = None,
        status: str | None = None,
        start_time: str | None = None,
        end_time: str | None = None,
        limit: int | None = None,
        offset: int | None = None,
    ) -> dict[str, Any]:
        """Query the compliance audit log (GET /v1/compliance/audit-log).
        `start_time`/`end_time` are RFC3339 strings. Unset filters are omitted."""
        params: dict[str, Any] = {}
        for name, val in (
            ("user_id", user_id), ("username", username), ("action", action),
            ("resource_type", resource_type), ("status", status),
            ("start_time", start_time), ("end_time", end_time),
            ("limit", limit), ("offset", offset),
        ):
            if val is not None:
                params[name] = val
        return _as_dict(self._t.request("GET", "/v1/compliance/audit-log", params=params).data)

    def get_masking_policy(self, tenant: str) -> dict[str, Any]:
        """Get a tenant's masking policy (GET /v1/compliance/masking-policy/{tenant}).

        The tenant is a path segment for reads (writes via set_masking_policy take
        the tenant from the caller's auth context — server asymmetry)."""
        return _as_dict(self._t.request("GET", f"/v1/compliance/masking-policy/{tenant}").data)

    def set_masking_policy(
        self, properties: Mapping[str, str], *, auto_detect: bool = False
    ) -> dict[str, Any]:
        """Set the tenant's masking policy (POST /v1/compliance/masking-policy). Admin-only.

        `properties` maps a property name to a strategy: one of "full", "partial",
        "hash", "redact", "tokenize", "none"."""
        body: dict[str, Any] = {"properties": dict(properties), "auto_detect": auto_detect}
        return _as_dict(self._t.request("POST", "/v1/compliance/masking-policy", json=body).data)
