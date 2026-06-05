from __future__ import annotations

import httpx
import respx

from graphdb_client._transport import Transport
from graphdb_client.resources.algorithms import AlgorithmsResource


def _res(base_url):
    return AlgorithmsResource(Transport(base_url, token="tok"))


@respx.mock
def test_run_maps_results(base_url):
    route = respx.post(f"{base_url}/algorithms").mock(return_value=httpx.Response(200, json={
        "algorithm": "pagerank", "results": {"1": 0.15, "2": 0.85}, "time": "2ms"}))
    r = _res(base_url).run("pagerank", parameters={"iterations": 20})
    assert r.algorithm == "pagerank" and r.results["2"] == 0.85
    assert b'"iterations"' in route.calls.last.request.read()


@respx.mock
def test_run_omits_parameters_when_none(base_url):
    route = respx.post(f"{base_url}/algorithms").mock(return_value=httpx.Response(
        200, json={"algorithm": "louvain", "results": {}, "time": "1ms"}))
    _res(base_url).run("louvain")
    assert b'"parameters"' not in route.calls.last.request.read()


@respx.mock
def test_shortest_path_found(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(200, json={
        "path": [1, 2, 3], "length": 3, "found": True, "time": "1ms"}))
    sp = _res(base_url).shortest_path(1, 3, max_depth=5)
    assert sp.found is True and sp.path == [1, 2, 3] and sp.length == 3


@respx.mock
def test_shortest_path_not_found(base_url):
    respx.post(f"{base_url}/shortest-path").mock(return_value=httpx.Response(200, json={
        "path": [], "length": 0, "found": False, "time": "1ms"}))
    sp = _res(base_url).shortest_path(1, 99)
    assert sp.found is False and sp.path == []
