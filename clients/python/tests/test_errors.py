from __future__ import annotations

import pytest

from graphdb_client.errors import (
    AuthError,
    ConflictError,
    GraphDBError,
    NotFoundError,
    RateLimitError,
    ServerError,
    ValidationError,
    from_response,
)


@pytest.mark.parametrize("status,cls", [
    (400, ValidationError), (401, AuthError), (404, NotFoundError),
    (409, ConflictError), (429, RateLimitError), (500, ServerError), (503, ServerError),
])
def test_from_response_maps_status(status, cls):
    err = from_response(status, {"error": "boom"}, "GET", "/nodes/1")
    assert isinstance(err, cls)
    assert isinstance(err, GraphDBError)
    assert err.status_code == status
    assert err.method == "GET"
    assert err.path == "/nodes/1"
    assert "boom" in str(err)


def test_unmapped_4xx_is_base_error():
    err = from_response(418, {}, "GET", "/x")
    assert type(err) is GraphDBError
    assert err.status_code == 418
