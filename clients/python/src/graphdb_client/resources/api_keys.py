from __future__ import annotations

from typing import Any, Sequence

from .._transport import Transport
from ..models import APIKey, CreatedAPIKey


class ApiKeysResource:
    def __init__(self, transport: Transport) -> None:
        self._t = transport

    def create(
        self,
        name: str,
        *,
        permissions: Sequence[str] | None = None,
        expires_in: int | None = None,
        environment: str | None = None,
    ) -> CreatedAPIKey:
        """Create an API key (POST /api/v1/apikeys). Admin-only.

        The returned CreatedAPIKey.key is the plaintext key — it is shown ONCE
        and cannot be retrieved again; store it securely. `expires_in` is seconds
        (0/None = never).
        """
        body: dict[str, Any] = {"name": name}
        if permissions is not None:
            body["permissions"] = list(permissions)
        if expires_in is not None:
            body["expires_in"] = expires_in
        if environment is not None:
            body["environment"] = environment
        res = self._t.request("POST", "/api/v1/apikeys", json=body)
        return CreatedAPIKey.from_dict(res.data)

    def list(self) -> list[APIKey]:
        """List API keys (GET /api/v1/apikeys). Admin-only. The plaintext key is
        never returned here — only metadata."""
        res = self._t.request("GET", "/api/v1/apikeys")
        return [APIKey.from_dict(d) for d in (res.data.get("keys") or [])]

    def revoke(self, key_id: str) -> None:
        """Revoke an API key (DELETE /api/v1/apikeys/{id}). Admin-only."""
        self._t.request("DELETE", f"/api/v1/apikeys/{key_id}")
