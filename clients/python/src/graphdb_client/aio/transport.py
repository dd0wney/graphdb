from __future__ import annotations

import asyncio
from typing import Any, Mapping

import httpx

from .._retry import RetryConfig, compute_delay, is_retryable, parse_retry_after
from .._transport import ApiResult, _safe_json, build_auth_headers
from ..errors import from_response


class AsyncTransport:
    """Async choke point for every HTTP request: auth, error mapping, raw access.

    Mirrors graphdb_client._transport.Transport with httpx.AsyncClient and awaited
    I/O; the auth/login/refresh/retry/error-map contract is identical.
    """

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
        # trust_env=False + follow_redirects=False — see the sync Transport
        # for the rationale (security audit M-12 / L-9): don't honor env
        # proxies that could exfiltrate the auth header, and never follow a
        # redirect carrying that header.
        self._http = httpx.AsyncClient(
            base_url=base_url.rstrip("/"),
            timeout=timeout,
            trust_env=False,
            follow_redirects=False,
        )
        self._retries = retries if retries is not None else RetryConfig()

    def _has_credentials(self) -> bool:
        return self._username is not None and self._password is not None

    async def _login(self) -> None:
        resp = await self._http.post(
            "/auth/login", json={"username": self._username, "password": self._password}
        )
        if resp.status_code >= 400:
            raise from_response(resp.status_code, _safe_json(resp), "POST", "/auth/login")
        data = _safe_json(resp) or {}
        self._token = data.get("access_token")
        self._refresh_token = data.get("refresh_token")

    async def _refresh(self) -> bool:
        if self._refresh_token:
            resp = await self._http.post(
                "/auth/refresh", json={"refresh_token": self._refresh_token}
            )
            if resp.status_code < 400:
                self._token = (_safe_json(resp) or {}).get("access_token")
                return True
        if self._has_credentials():
            await self._login()
            return True
        return False

    async def _attempt(
        self,
        method: str,
        path: str,
        *,
        json: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> httpx.Response:
        if self._token is None and self._has_credentials():
            await self._login()

        resp = await self._http.request(
            method, path, json=json, params=params,
            headers=build_auth_headers(self._token, self._api_key),
        )

        if resp.status_code == 401 and (self._refresh_token or self._has_credentials()):
            if await self._refresh():
                resp = await self._http.request(
                    method, path, json=json, params=params,
                    headers=build_auth_headers(self._token, self._api_key),
                )
        return resp

    async def request(
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
                resp = await self._attempt(method, path, json=json, params=params)
            except httpx.TransportError as exc:
                if attempt < self._retries.max_retries and is_retryable(
                    method, None, exc, self._retries
                ):
                    await asyncio.sleep(compute_delay(attempt, self._retries, None))
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
                await asyncio.sleep(compute_delay(attempt, self._retries, retry_after))
                attempt += 1
                continue

            if resp.status_code >= 400:
                raise from_response(resp.status_code, _safe_json(resp), method, path)
            return ApiResult(data=_safe_json(resp), headers=resp.headers)

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "AsyncTransport":
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.aclose()
