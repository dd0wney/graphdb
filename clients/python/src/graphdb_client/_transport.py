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
    ) -> None:
        if (username is None) != (password is None):
            raise ValueError("username and password must be provided together")
        self._token = token
        self._api_key = api_key
        self._username = username
        self._password = password
        self._refresh_token: str | None = None
        self._http = httpx.Client(base_url=base_url.rstrip("/"), timeout=timeout)

    def _auth_headers(self) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self._token:
            headers["Authorization"] = f"Bearer {self._token}"
        elif self._api_key:
            headers["X-API-Key"] = self._api_key
        return headers

    def _has_credentials(self) -> bool:
        return self._username is not None and self._password is not None

    def _login(self) -> None:
        resp = self._http.post(
            "/auth/login", json={"username": self._username, "password": self._password}
        )
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), "POST", "/auth/login")
        data = _safe_json(resp) or {}
        self._token = data.get("access_token")
        self._refresh_token = data.get("refresh_token")

    def _refresh(self) -> bool:
        """Return True if the token was refreshed. Falls back to re-login."""
        if self._refresh_token:
            resp = self._http.post("/auth/refresh", json={"refresh_token": self._refresh_token})
            if resp.status_code < 400:
                self._token = (_safe_json(resp) or {}).get("access_token")
                return True
        if self._has_credentials():
            self._login()
            return True
        return False

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        if self._token is None and self._has_credentials():
            self._login()

        resp = self._http.request(
            method, path, json=json, params=params, headers=self._auth_headers()
        )

        if resp.status_code == 401 and (self._refresh_token or self._has_credentials()):
            if self._refresh():
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
