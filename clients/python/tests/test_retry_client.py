from __future__ import annotations

import httpx
import pytest
import respx

from graphdb_client import GraphDBClient, RetryConfig
from graphdb_client.errors import ServerError

OK = {"id": 1, "labels": [], "properties": {}}
FAST = RetryConfig(max_retries=2, backoff_factor=0.0, max_backoff=0.0, respect_retry_after=False)


def test_retry_config_exported():
    assert RetryConfig(max_retries=1).max_retries == 1


@respx.mock
def test_client_default_retries_on(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    # default retries=2; force zero backoff so the test is instant
    c = GraphDBClient(base_url, token="tok", retries=FAST)
    assert c.nodes.get(1).id == 1 and route.call_count == 2


@respx.mock
def test_client_retries_zero_disables(base_url):
    route = respx.get(f"{base_url}/nodes/1").mock(side_effect=[
        httpx.Response(503), httpx.Response(200, json=OK)])
    c = GraphDBClient(base_url, token="tok", retries=0)
    with pytest.raises(ServerError):
        c.nodes.get(1)
    assert route.call_count == 1
