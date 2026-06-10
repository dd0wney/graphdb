from __future__ import annotations

import random
from dataclasses import dataclass
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime

import httpx


@dataclass(frozen=True)
class RetryConfig:
    max_retries: int = 2
    backoff_factor: float = 0.5
    max_backoff: float = 30.0
    retry_statuses: frozenset[int] = frozenset({429, 502, 503, 504})
    retry_methods: frozenset[str] = frozenset({"GET", "PUT", "DELETE", "HEAD", "OPTIONS"})
    respect_retry_after: bool = True


def coerce_retry_config(retries: "RetryConfig | int | None") -> RetryConfig:
    """Normalize the client-facing ``retries`` param. None/0 -> disabled."""
    if retries is None:
        return RetryConfig(max_retries=0)
    if isinstance(retries, int):
        return RetryConfig(max_retries=retries)
    return retries


def is_retryable(
    method: str,
    status: int | None,
    exc: BaseException | None,
    config: RetryConfig,
) -> bool:
    """Decide whether a failed attempt should be retried (before checking attempt budget)."""
    if exc is not None:
        # Connection never established -> request not processed -> safe on any method.
        if isinstance(exc, httpx.ConnectError):
            return True
        # Other transport errors (e.g. read/write timeout) may have been applied
        # server-side -> only retry idempotent methods.
        return method.upper() in config.retry_methods
    if status is None:
        return False
    if status == 429:
        # Rate-limited. The server received and processed the request far
        # enough to identify the rate-limit condition, so for a
        # non-idempotent method (POST/PATCH) it may have committed a write
        # before returning 429 — only retry idempotent methods. (graphdb
        # uses POST for some reads, but the SDK can't distinguish those
        # from write-POSTs, so it errs safe.) Security audit M-11.
        return method.upper() in config.retry_methods
    if status in config.retry_statuses:
        return method.upper() in config.retry_methods
    return False


def compute_delay(attempt: int, config: RetryConfig, retry_after: float | None) -> float:
    """Seconds to wait before the next attempt. Retry-After wins (clamped); else full-jitter exp."""
    if retry_after is not None:
        return min(retry_after, config.max_backoff)
    upper = min(config.max_backoff, config.backoff_factor * (2 ** attempt))
    if upper <= 0:
        return 0.0
    return random.uniform(0, upper)


def parse_retry_after(value: str | None) -> float | None:
    """Parse a Retry-After header value (delta-seconds or HTTP-date) into seconds, or None."""
    if not value:
        return None
    value = value.strip()
    if value.isdigit():
        return float(value)
    try:
        dt = parsedate_to_datetime(value)
    except (TypeError, ValueError):
        return None
    if dt is None:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return max(0.0, (dt - datetime.now(timezone.utc)).total_seconds())
