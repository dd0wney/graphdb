from __future__ import annotations

import time
from dataclasses import dataclass
from typing import Any, Mapping

import httpx

from ._retry import RetryConfig, compute_delay, is_retryable, parse_retry_after
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
        retries: RetryConfig | None = None,
    ) -> None:
        if (username is None) != (password is None):
            raise ValueError("username and password must be provided together")
        self._token = token
        self._api_key = api_key
        self._username = username
        self._password = password
        self._refresh_token: str | None = None
        self._http = httpx.Client(base_url=base_url.rstrip("/"), timeout=timeout)
        self._retries = retries if retries is not None else RetryConfig()

    def _auth_headers(self) -> dict[str, str]:
        return build_auth_headers(self._token, self._api_key)

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

    def _attempt(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> httpx.Response:
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
        return resp

    def request(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> ApiResult:
        attempt = 0
        while True:
            try:
                resp = self._attempt(method, path, json=json, params=params)
            except httpx.TransportError as exc:
                if attempt < self._retries.max_retries and is_retryable(
                    method, None, exc, self._retries
                ):
                    time.sleep(compute_delay(attempt, self._retries, None))
                    attempt += 1
                    continue
                raise

            if (
                resp.status_code >= 400
                and attempt < self._retries.max_retries
                and is_retryable(method, resp.status_code, None, self._retries)
            ):
                retry_after = (
                    parse_retry_after(resp.headers.get("Retry-After"))
                    if self._retries.respect_retry_after
                    else None
                )
                time.sleep(compute_delay(attempt, self._retries, retry_after))
                attempt += 1
                continue

            if resp.status_code >= 400:
                raise from_response(resp.status_code, _safe_json(resp), method, path)
            return ApiResult(data=_safe_json(resp), headers=resp.headers)

    def close(self) -> None:
        self._http.close()


def build_auth_headers(token: str | None, api_key: str | None) -> dict[str, str]:
    headers: dict[str, str] = {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    elif api_key:
        headers["X-API-Key"] = api_key
    return headers


def _safe_json(resp: httpx.Response) -> Any:
    if not resp.content:
        return None
    try:
        return resp.json()
    except ValueError:
        return resp.text
