"""Lift a 202 Accepted response into a NotDurable report.

The server answers 202 when it applied a write in memory but its write-ahead-log
append failed. The body is the ordinary body plus five flat fields. These two
helpers are the only place that knows that, so every write path reports a 202
the same way.
"""

from __future__ import annotations

from typing import TypeVar

from ._transport import ApiResult
from .models import DeleteResult, NotDurable

T = TypeVar("T")


def attach(entity: T, res: ApiResult) -> T:
    """Put the 202 report on an entity and return it.

    The entity comes back whole either way, so a caller that never reads
    `not_durable` keeps the behaviour it had before this report existed.
    """
    entity.not_durable = NotDurable.from_response(res.status_code, res.data)  # type: ignore[attr-defined]
    return entity


def delete_result(res: ApiResult) -> DeleteResult:
    """Build the result of a delete, empty on the ordinary 204."""
    return DeleteResult(not_durable=NotDurable.from_response(res.status_code, res.data))
