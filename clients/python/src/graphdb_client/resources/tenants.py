from __future__ import annotations

from typing import Any, Mapping

from .._path import quote_segment
from .._transport import Transport
from ..models import Tenant, TenantUsage


class TenantsResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        id: str,
        name: str,
        *,
        description: str | None = None,
        quota: Mapping[str, Any] | None = None,
        metadata: Mapping[str, Any] | None = None,
    ) -> Tenant:
        """Create a tenant (POST /api/v1/tenants). Admin-only."""
        body: dict[str, Any] = {"id": id, "name": name}
        if description is not None:
            body["description"] = description
        if quota is not None:
            body["quota"] = dict(quota)
        if metadata is not None:
            body["metadata"] = dict(metadata)
        res = self._t.request("POST", "/api/v1/tenants", json=body)
        return Tenant.from_dict(res.data)

    def list(self) -> list[Tenant]:
        """List tenants (GET /api/v1/tenants). Admin-only."""
        res = self._t.request("GET", "/api/v1/tenants")
        return [Tenant.from_dict(d) for d in (res.data.get("tenants") or [])]

    def get(self, tenant_id: str) -> Tenant:
        """Get one tenant (GET /api/v1/tenants/{id})."""
        res = self._t.request("GET", f"/api/v1/tenants/{quote_segment(tenant_id)}")
        return Tenant.from_dict(res.data)

    def update(
        self,
        tenant_id: str,
        *,
        name: str | None = None,
        description: str | None = None,
        quota: Mapping[str, Any] | None = None,
        metadata: Mapping[str, Any] | None = None,
    ) -> Tenant:
        """Update a tenant (PUT /api/v1/tenants/{id}). Sends only provided fields. Admin-only."""
        body: dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if description is not None:
            body["description"] = description
        if quota is not None:
            body["quota"] = dict(quota)
        if metadata is not None:
            body["metadata"] = dict(metadata)
        res = self._t.request("PUT", f"/api/v1/tenants/{quote_segment(tenant_id)}", json=body)
        return Tenant.from_dict(res.data)

    def delete(self, tenant_id: str) -> None:
        """Delete a tenant (DELETE /api/v1/tenants/{id}). Admin-only."""
        self._t.request("DELETE", f"/api/v1/tenants/{quote_segment(tenant_id)}")

    def usage(self, tenant_id: str) -> TenantUsage:
        """Tenant usage stats (GET /api/v1/tenants/{id}/usage)."""
        res = self._t.request("GET", f"/api/v1/tenants/{quote_segment(tenant_id)}/usage")
        return TenantUsage.from_dict(res.data)

    def suspend(self, tenant_id: str) -> None:
        """Suspend a tenant (POST /api/v1/tenants/{id}/suspend). Admin-only."""
        self._t.request("POST", f"/api/v1/tenants/{quote_segment(tenant_id)}/suspend")

    def activate(self, tenant_id: str) -> None:
        """Activate a tenant (POST /api/v1/tenants/{id}/activate). Admin-only."""
        self._t.request("POST", f"/api/v1/tenants/{quote_segment(tenant_id)}/activate")
