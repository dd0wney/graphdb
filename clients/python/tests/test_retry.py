from __future__ import annotations

import httpx

from graphdb_client._retry import (
    RetryConfig,
    coerce_retry_config,
    compute_delay,
    is_retryable,
    parse_retry_after,
)


def test_coerce_retry_config():
    assert coerce_retry_config(None).max_retries == 0
    assert coerce_retry_config(0).max_retries == 0
    assert coerce_retry_config(5).max_retries == 5
    cfg = RetryConfig(max_retries=3)
    assert coerce_retry_config(cfg) is cfg


def test_is_retryable_transport_errors():
    cfg = RetryConfig()
    # connect failures never reached the server -> safe on ANY method
    assert is_retryable("POST", None, httpx.ConnectError("x"), cfg) is True
    # other transport errors (read timeout) may have been processed -> idempotent only
    assert is_retryable("POST", None, httpx.ReadTimeout("x"), cfg) is False
    assert is_retryable("GET", None, httpx.ReadTimeout("x"), cfg) is True


def test_is_retryable_statuses():
    cfg = RetryConfig()
    # 429: the server processed the request (it had to, to rate-limit it),
    # so a write-POST may have committed — idempotent methods only (M-11).
    assert is_retryable("POST", 429, None, cfg) is False
    assert is_retryable("GET", 429, None, cfg) is True
    assert is_retryable("GET", 503, None, cfg) is True
    assert is_retryable("POST", 503, None, cfg) is False  # 5xx idempotent-only
    assert is_retryable("GET", 500, None, cfg) is False   # 500 not in default retry_statuses
    assert is_retryable("GET", 404, None, cfg) is False
    assert is_retryable("GET", None, None, cfg) is False


def test_compute_delay_bounds_and_zero():
    zero = RetryConfig(backoff_factor=0.0, max_backoff=0.0)
    assert compute_delay(0, zero, None) == 0.0
    cfg = RetryConfig(backoff_factor=0.5, max_backoff=30.0)
    for attempt in range(4):
        d = compute_delay(attempt, cfg, None)
        assert 0.0 <= d <= min(30.0, 0.5 * (2 ** attempt))


def test_compute_delay_retry_after_wins_and_clamps():
    cfg = RetryConfig(backoff_factor=10.0, max_backoff=5.0)
    assert compute_delay(0, cfg, 2.0) == 2.0      # honored over computed backoff
    assert compute_delay(0, cfg, 99.0) == 5.0      # clamped to max_backoff


def test_parse_retry_after():
    assert parse_retry_after(None) is None
    assert parse_retry_after("") is None
    assert parse_retry_after("3") == 3.0
    assert parse_retry_after("not-a-date") is None
    # an HTTP-date in the past -> 0.0 (never negative)
    assert parse_retry_after("Wed, 21 Oct 2015 07:28:00 GMT") == 0.0
