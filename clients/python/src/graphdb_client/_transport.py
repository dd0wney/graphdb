from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping

import httpx

from .errors import from_response


@dataclass
class ApiResult:
    data: Any
    headers: Mapping[str, str]


class Transport:
    """Single choke point for every HTTP request: auth, error mapping, raw access."""

    def __init__(
        self,
        base_url: str,
        *,
        token: str | None = None,
        api_key: str | None = None,
        username: str | None = None,
        password: str | None = None,
        timeout: float = 30.0,
        max_retries: int = 2,
    ) -> None:
        self._token = token
        self._api_key = api_key
        self._username = username
        self._password = password
        self._refresh_token: str | None = None
        self._max_retries = max_retries
        self._http = httpx.Client(base_url=base_url.rstrip("/"), timeout=timeout)

    def _auth_headers(self) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        elif self._api_key:
            headers["X-API-Key"] = self._api_key
        return headers

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        resp = self._http.request(
            method, path, json=json, params=params, headers=self._auth_headers()
        )
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), method, path)
        return ApiResult(data=_safe_json(resp), headers=resp.headers)

    def close(self) -> None:
        self._http.close()


def _safe_json(resp: httpx.Response) -> Any:
    if not resp.content:
        return None
    try:
        return resp.json()
    except ValueError:
        return resp.text
