from __future__ import annotations

import httpx
import respx

from graphdb_client import GraphDBClient


def _c(base_url):
    return GraphDBClient(base_url, token="tok")


@respx.mock
def test_query_maps_columns_rows(base_url):
    route = respx.post(f"{base_url}/query").mock(return_value=httpx.Response(200, json={
        "columns": ["n.name"], "rows": [{"n.name": "Alice"}], "count": 1, "time": "1ms"}))
    r = _c(base_url).query("MATCH (n) RETURN n.name", parameters={"x": 1})
    assert r.columns == ["n.name"] and r.rows == [{"n.name": "Alice"}] and r.count == 1
    assert b'"parameters"' in route.calls.last.request.read()


@respx.mock
def test_graphql_returns_raw_dict_including_errors(base_url):
    respx.post(f"{base_url}/graphql").mock(return_value=httpx.Response(200, json={
        "data": None, "errors": [{"message": "boom"}]}))
    out = _c(base_url).graphql("{ x }")
    assert out["errors"][0]["message"] == "boom"


@respx.mock
def test_graphql_sends_operation_name_and_variables(base_url):
    route = respx.post(f"{base_url}/graphql").mock(
        return_value=httpx.Response(200, json={"data": {}}))
    _c(base_url).graphql("query Q($a:Int){x}", variables={"a": 1}, operation_name="Q")
    body = route.calls.last.request.read()
    assert b'"operationName"' in body and b'"variables"' in body
