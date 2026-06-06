from __future__ import annotations

from typing import Any, Iterable

from langchain_core.documents import Document
from langchain_core.embeddings import Embeddings
from langchain_core.vectorstores import VectorStore


def _to_document(result: Any, content_key: str) -> Document:
    node = result.node
    props = dict(node.properties) if node is not None else {}
    content = str(props.pop(content_key, "")) if node is not None else ""
    metadata: dict[str, Any] = {
        "node_id": result.node_id,
        "score": result.score,
        "distance": result.distance,
    }
    if node is not None:
        metadata["labels"] = node.labels
        metadata.update(props)  # remaining (non-content) properties
    return Document(page_content=content, metadata=metadata)


class GraphDBVectorStore(VectorStore):
    """Read-optimized LangChain VectorStore over graphdb ``/vector-search``.

    Retrieval-only: ``similarity_search`` embeds the query (via a supplied
    LangChain ``Embeddings``, else graphdb's ``client.embeddings``) and runs a
    vector search. Ingest vectors with ``client.nodes.create`` + a vector index;
    ``add_texts``/``from_texts`` raise ``NotImplementedError``.
    """

    def __init__(
        self,
        client: Any,
        property_name: str,
        *,
        embedding: Embeddings | None = None,
        content_key: str = "text",
        aclient: Any = None,
    ) -> None:
        self._client = client
        self._aclient = aclient
        self._property_name = property_name
        self._embedding = embedding
        self._content_key = content_key

    @property
    def embeddings(self) -> Embeddings | None:
        return self._embedding

    def _embed_query(self, query: str) -> list[float]:
        if self._embedding is not None:
            return list(self._embedding.embed_query(query))
        return list(self._client.embeddings(query).vectors[0])

    def similarity_search(self, query: str, k: int = 4, **kwargs: Any) -> list[Document]:
        return self.similarity_search_by_vector(self._embed_query(query), k=k, **kwargs)

    def similarity_search_by_vector(
        self, embedding: list[float], k: int = 4, **kwargs: Any
    ) -> list[Document]:
        results = self._client.vector_search(
            self._property_name, embedding, k=k, include_nodes=True, **kwargs
        )
        return [_to_document(r, self._content_key) for r in results]

    async def asimilarity_search(self, query: str, k: int = 4, **kwargs: Any) -> list[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBVectorStore requires an `aclient` for async search")
        if self._embedding is not None:
            vector = list(await self._embedding.aembed_query(query))
        else:
            res = await self._aclient.embeddings(query)
            vector = list(res.vectors[0])
        return await self.asimilarity_search_by_vector(vector, k=k, **kwargs)

    async def asimilarity_search_by_vector(
        self, embedding: list[float], k: int = 4, **kwargs: Any
    ) -> list[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBVectorStore requires an `aclient` for async search")
        results = await self._aclient.vector_search(
            self._property_name, embedding, k=k, include_nodes=True, **kwargs
        )
        return [_to_document(r, self._content_key) for r in results]

    def add_texts(
        self,
        texts: Iterable[str],
        metadatas: list[dict[str, Any]] | None = None,
        **kwargs: Any,
    ) -> list[str]:
        raise NotImplementedError(
            "GraphDBVectorStore is retrieval-only; ingest vectors via "
            "client.nodes.create(...) + a vector index."
        )

    @classmethod
    def from_texts(
        cls,
        texts: list[str],
        embedding: Embeddings | None = None,
        metadatas: list[dict[str, Any]] | None = None,
        **kwargs: Any,
    ) -> "GraphDBVectorStore":
        raise NotImplementedError(
            "GraphDBVectorStore is retrieval-only; construct it directly with an "
            "existing graphdb client + vector index property_name."
        )
