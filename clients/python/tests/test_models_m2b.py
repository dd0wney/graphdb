from __future__ import annotations

from graphdb_client.models import APIKey, CreatedAPIKey, Tenant, TenantUsage


def test_tenant():
    t = Tenant.from_dict({
        "id": "acme", "name": "Acme", "status": "active", "description": "d",
        "quota": {"max_nodes": 100}, "metadata": {"tier": "gold"},
        "created_at": 111, "updated_at": 222,
    })
    assert t.id == "acme" and t.name == "Acme" and t.status == "active"
    assert t.description == "d" and t.quota == {"max_nodes": 100}
    assert t.metadata == {"tier": "gold"} and t.created_at == 111 and t.updated_at == 222


def test_tenant_optional_fields_default():
    t = Tenant.from_dict({"id": "x", "name": "X", "status": "active"})
    assert t.description is None and t.quota is None and t.metadata == {}
    assert t.created_at == 0 and t.updated_at == 0


def test_tenant_usage():
    u = TenantUsage.from_dict({
        "tenant_id": "acme", "node_count": 5, "edge_count": 7,
        "storage_bytes": 1024, "quota_usage": {"nodes_pct": 0.5}, "last_updated": 99,
    })
    assert u.tenant_id == "acme" and u.node_count == 5 and u.edge_count == 7
    assert u.storage_bytes == 1024 and u.quota_usage == {"nodes_pct": 0.5} and u.last_updated == 99


def test_api_key_list_item():
    k = APIKey.from_dict({
        "id": "k1", "name": "ci", "prefix": "gdb_live_",
        "permissions": ["read", "write"], "created": "2026-06-06T00:00:00Z",
        "expires": None, "last_used": "2026-06-06T01:00:00Z", "revoked": False,
    })
    assert k.id == "k1" and k.prefix == "gdb_live_" and k.permissions == ["read", "write"]
    assert k.created == "2026-06-06T00:00:00Z" and k.expires is None
    assert k.last_used == "2026-06-06T01:00:00Z" and k.revoked is False


def test_created_api_key_carries_plaintext_key():
    c = CreatedAPIKey.from_dict({
        "key": "gdb_live_secret", "id": "k1", "name": "ci",
        "prefix": "gdb_live_", "created": "2026-06-06T00:00:00Z", "expires": None,
    })
    assert c.key == "gdb_live_secret" and c.id == "k1" and c.prefix == "gdb_live_"
    assert c.expires is None
