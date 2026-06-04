from __future__ import annotations

import os
import uuid

import pytest

from graphdb_client import GraphDBClient

pytestmark = pytest.mark.skipif(
    os.environ.get("GRAPHDB_SDK_IT") != "1",
    reason="integration test; set GRAPHDB_SDK_IT=1 (+ GRAPHDB_SDK_URL/TOKEN) to run",
)


@pytest.fixture
def client():
    url = os.environ.get("GRAPHDB_SDK_URL", "http://localhost:8080")
    token = os.environ.get("GRAPHDB_SDK_TOKEN")
    with GraphDBClient(url, token=token) as c:
        yield c


def test_smoke_batch_list_traverse(client):
    run = uuid.uuid4().hex[:8]
    label = f"SDKSmoke_{run}"
    created = client.nodes.batch_create([
        {"labels": [label], "properties": {"_key": f"{run}:a"}},
        {"labels": [label], "properties": {"_key": f"{run}:b"}},
    ])
    by_key = {n.properties["_key"]: n.id for n in created}
    assert set(by_key) == {f"{run}:a", f"{run}:b"}

    listed = list(client.nodes.list(label=label, page_size=1))
    assert {n.properties["_key"] for n in listed} == {f"{run}:a", f"{run}:b"}

    client.edges.create(by_key[f"{run}:a"], by_key[f"{run}:b"], "LINKS")
    neighbours = client.traverse(by_key[f"{run}:a"], max_depth=1)
    assert by_key[f"{run}:b"] in {n.id for n in neighbours}
