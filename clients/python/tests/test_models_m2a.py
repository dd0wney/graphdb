from __future__ import annotations

from graphdb_client.models import (
    AlgorithmResult,
    EmbeddingsResult,
    HybridSearchResult,
    QueryResult,
    RetrieveDocument,
    RetrieveResult,
    SearchHit,
    ShortestPath,
    VectorIndex,
)


def test_search_hit_fulltext_has_no_ranks():
    h = SearchHit.from_dict({"node_id": 7, "score": 1.5, "snippet": "hi"})
    assert h.node_id == 7 and h.score == 1.5 and h.snippet == "hi"
    assert h.fts_rank is None and h.lsa_rank is None and h.node is None


def test_search_hit_hybrid_carries_ranks_and_node():
    h = SearchHit.from_dict({
        "node_id": 3, "score": 0.9, "fts_rank": 1, "lsa_rank": 2,
        "node": {"id": 3, "labels": ["Doc"], "properties": {"t": "x"}},
    })
    assert h.fts_rank == 1 and h.lsa_rank == 2
    assert h.node is not None and h.node.id == 3


def test_hybrid_result_maps_results_and_degraded():
    r = HybridSearchResult.from_dict({
        "results": [{"node_id": 1, "score": 0.5}],
        "count": 1, "took_ms": 4, "degraded": "no-lsa-index",
    })
    assert len(r.hits) == 1 and r.count == 1 and r.took_ms == 4
    assert r.degraded == "no-lsa-index"


def test_hybrid_result_degraded_absent_is_none():
    r = HybridSearchResult.from_dict({"results": [], "count": 0, "took_ms": 1})
    assert r.degraded is None


def test_vector_index():
    vi = VectorIndex.from_dict(
        {"property_name": "embedding", "dimensions": 384, "metric": "cosine"}
    )
    assert vi.property_name == "embedding" and vi.dimensions == 384 and vi.metric == "cosine"


def test_retrieve_document_flattens_metadata_and_keeps_path():
    doc = RetrieveDocument.from_dict({
        "page_content": "chunk",
        "metadata": {
            "node_id": 9, "score": 0.8,
            "source": {"node_id": 5, "label": "Doc", "path": [5, 7, 9]},
            "node": {"id": 9, "labels": ["Doc"], "properties": {}},
        },
    })
    assert doc.page_content == "chunk" and doc.node_id == 9 and doc.score == 0.8
    assert doc.source.path == [5, 7, 9] and doc.source.label == "Doc"
    assert doc.node is not None and doc.node.id == 9


def test_retrieve_result():
    r = RetrieveResult.from_dict({
        "documents": [{"page_content": "c", "metadata": {"node_id": 1, "score": 0.1,
                       "source": {"node_id": 1, "path": [1]}}}],
        "took_ms": 12, "degraded": "query-out-of-vocabulary",
    })
    assert len(r.documents) == 1 and r.took_ms == 12
    assert r.degraded == "query-out-of-vocabulary"


def test_embeddings_result_orders_vectors_by_index():
    r = EmbeddingsResult.from_dict({
        "object": "list", "model": "lsa",
        "data": [
            {"object": "embedding", "embedding": [0.2], "index": 1},
            {"object": "embedding", "embedding": [0.1], "index": 0},
        ],
        "usage": {"prompt_tokens": 3, "total_tokens": 3},
    })
    assert r.vectors == [[0.1], [0.2]]  # reordered by index
    assert r.model == "lsa" and r.usage["total_tokens"] == 3


def test_query_result_rows_stay_dicts():
    r = QueryResult.from_dict({"columns": ["n"], "rows": [{"n": 1}], "count": 1, "time": "1ms"})
    assert r.columns == ["n"] and r.rows == [{"n": 1}] and r.count == 1


def test_algorithm_result_freeform():
    r = AlgorithmResult.from_dict({"algorithm": "pagerank", "results": {"1": 0.15}, "time": "2ms"})
    assert r.algorithm == "pagerank" and r.results == {"1": 0.15}


def test_shortest_path_found_flag_distinct_from_empty():
    r = ShortestPath.from_dict({"path": [], "length": 0, "found": False})
    assert r.found is False and r.path == []
