"""Security-hardening pins for the 2026-06-10 audit (H-10, M-10, M-11, M-12)."""

from __future__ import annotations

from typing import Any, Mapping

from graphdb_client._caching import cache_key
from graphdb_client._path import quote_segment
from graphdb_client._retry import RetryConfig, is_retryable
from graphdb_client._transport import Transport
from graphdb_client.resources.tenants import TenantsResource


def test_quote_segment_encodes_path_separators() -> None:
    # H-10: the path separator must be encoded so a value can never span
    # more than one segment. Dots are unreserved, but with the slash
    # encoded "../admin" can no longer traverse.
    assert quote_segment("../admin") == "..%2Fadmin"
    assert quote_segment("a/b/c") == "a%2Fb%2Fc"
    assert quote_segment("plain-tenant_01") == "plain-tenant_01"


class _RecordingTransport:
    """Minimal duck-typed transport that records the path it was handed."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, str]] = []

    def request(
        self, method: str, path: str, *, json: Any = None, params: Mapping[str, Any] | None = None
    ) -> None:
        self.calls.append((method, path))
        return None


def test_tenants_resource_encodes_traversal_value() -> None:
    # H-10: a caller-supplied tenant id containing a traversal sequence
    # must reach the transport as an encoded single segment, never as a
    # path that resolves to a different endpoint.
    fake = _RecordingTransport()
    TenantsResource(fake).delete("../../api/v1/tenants")  # type: ignore[arg-type]

    assert len(fake.calls) == 1
    _, path = fake.calls[0]
    assert path == "/api/v1/tenants/..%2F..%2Fapi%2Fv1%2Ftenants"
    assert "/api/v1/tenants/../" not in path


def test_cache_key_namespaced() -> None:
    # M-10: a namespace prefixes the key so a shared backend can't serve
    # one auth context's cached response to another.
    assert cache_key("GET", "/nodes/1", None) == "GET:/nodes/1"
    assert cache_key("GET", "/nodes/1", None, "tenant-a") == "tenant-a:GET:/nodes/1"
    assert cache_key("GET", "/nodes/1", None, "tenant-a") != cache_key(
        "GET", "/nodes/1", None, "tenant-b"
    )


def test_retry_429_not_retried_for_post() -> None:
    # M-11: 429 must not be retried on a non-idempotent method — the
    # server processed the request and a write-POST may have committed.
    cfg = RetryConfig()
    assert is_retryable("POST", 429, None, cfg) is False
    assert is_retryable("PATCH", 429, None, cfg) is False
    # Idempotent methods are still retried on 429.
    assert is_retryable("GET", 429, None, cfg) is True
    assert is_retryable("DELETE", 429, None, cfg) is True


def test_transport_disables_env_proxies_and_redirects() -> None:
    # M-12 / L-9: env proxies could exfiltrate the auth header; redirects
    # could forward it to another host.
    t = Transport("http://example.invalid")
    try:
        assert t._http.trust_env is False
        assert t._http.follow_redirects is False
    finally:
        t.close()
