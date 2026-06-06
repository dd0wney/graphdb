"""LangChain adapters for graphdb. Requires the optional ``langchain`` extra."""
from __future__ import annotations

try:
    import langchain_core  # noqa: F401
except ImportError as exc:  # pragma: no cover - exercised via sys.modules patch in tests
    raise ImportError(
        "graphdb_client.langchain requires langchain-core: "
        "pip install 'graphdb-client[langchain]'"
    ) from exc

from .retriever import GraphDBRetriever
from .vectorstore import GraphDBVectorStore

__all__ = ["GraphDBRetriever", "GraphDBVectorStore"]
