from __future__ import annotations

import pytest

BASE_URL = "https://graphdb.test"


@pytest.fixture
def base_url() -> str:
    return BASE_URL
