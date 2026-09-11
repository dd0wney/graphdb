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


@respx.mock
def test_graphql_after_cursor_walks_pages_and_stops_at_short_page(base_url):
    # Server contract (PR #585): `after` is the id of the last item on the
    # previous page; a page shorter than `limit` is the last page.
    page1 = httpx.Response(200, json={"data": {"persons": [{"id": "1"}, {"id": "2"}]}})
    page2 = httpx.Response(200, json={"data": {"persons": [{"id": "3"}]}})
    route = respx.post(f"{base_url}/graphql").mock(side_effect=[page1, page2])

    client = _c(base_url)
    query = "query($after: ID) { persons(limit: 2, after: $after) { id } }"
    limit = 2
    after: str | None = None
    all_ids: list[str] = []
    while True:
        out = client.graphql(query, variables={"after": after})
        page = out["data"]["persons"]
        all_ids.extend(p["id"] for p in page)
        if len(page) < limit:
            break
        after = page[-1]["id"]

    assert all_ids == ["1", "2", "3"]
    assert route.calls.call_count == 2
    first_body = route.calls[0].request.read()
    second_body = route.calls[1].request.read()
    assert b'"after": null' in first_body or b'"after":null' in first_body
    assert b'"after": "2"' in second_body or b'"after":"2"' in second_body
