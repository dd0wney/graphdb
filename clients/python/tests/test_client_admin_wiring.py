from __future__ import annotations

from graphdb_client import GraphDBClient
from graphdb_client.resources.api_keys import ApiKeysResource
from graphdb_client.resources.compliance import ComplianceResource
from graphdb_client.resources.security import SecurityResource
from graphdb_client.resources.tenants import TenantsResource


def test_admin_resources_wired():
    c = GraphDBClient("https://graphdb.test", token="tok")
    assert isinstance(c.tenants, TenantsResource)
    assert isinstance(c.api_keys, ApiKeysResource)
    assert isinstance(c.security, SecurityResource)
    assert isinstance(c.compliance, ComplianceResource)
