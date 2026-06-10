"""URL path-segment encoding for safe interpolation into request paths."""

from __future__ import annotations

from urllib.parse import quote


def quote_segment(value: str) -> str:
    """Percent-encode ``value`` for use as a single URL path segment.

    Security audit H-10: resource methods f-string caller-supplied strings
    (``tenant_id``, ``property_name``, ``key_id``, ``tenant``) directly into
    request paths. httpx normalizes ``../`` sequences before sending, so an
    unencoded value such as ``"../admin"`` silently retargets the request to
    a different endpoint. ``quote(..., safe="")`` encodes the path separator
    (and any other reserved characters), so the value can only ever be one
    segment — a traversal value becomes the literal segment ``..%2Fadmin``.
    """
    return quote(value, safe="")
