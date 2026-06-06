from __future__ import annotations

from typing import Any

from langchain_core.callbacks import (
    AsyncCallbackManagerForRetrieverRun,
    CallbackManagerForRetrieverRun,
)
from langchain_core.documents import Document
from langchain_core.retrievers import BaseRetriever
from pydantic import Field


def _to_document(doc: Any) -> Document:
    """Map a graphdb RetrieveDocument to a LangChain Document."""
    metadata: dict[str, Any] = {
        "node_id": doc.node_id,
        "score": doc.score,
        "path": doc.source.path,
        "label": doc.source.label,
    }
    if doc.node is not None:
        metadata["labels"] = doc.node.labels
        metadata["properties"] = doc.node.properties
    return Document(page_content=doc.page_content, metadata=metadata)


class GraphDBRetriever(BaseRetriever):
    """LangChain retriever over graphdb GraphRAG (``/v1/retrieve``).

    Provide a sync ``client`` (GraphDBClient) and/or an ``aclient``
    (AsyncGraphDBClient). ``k`` and any ``retrieve_kwargs`` pass through to
    ``client.retrieve(...)``.
    """

    client: Any = None
    aclient: Any = None
    k: int = 4
    retrieve_kwargs: dict[str, Any] = Field(default_factory=dict)

    def _get_relevant_documents(
        self, query: str, *, run_manager: CallbackManagerForRetrieverRun
    ) -> list[Document]:
        if self.client is None:
            raise ValueError("GraphDBRetriever requires a sync `client` for sync retrieval")
        result = self.client.retrieve(query, k=self.k, **self.retrieve_kwargs)
        return [_to_document(d) for d in result.documents]

    async def _aget_relevant_documents(
        self, query: str, *, run_manager: AsyncCallbackManagerForRetrieverRun
    ) -> list[Document]:
        if self.aclient is None:
            return await super()._aget_relevant_documents(query, run_manager=run_manager)
        result = await self.aclient.retrieve(query, k=self.k, **self.retrieve_kwargs)
        return [_to_document(d) for d in result.documents]
