from __future__ import annotations

from typing import Any, AsyncIterator, Iterator

from langchain_core.document_loaders import BaseLoader
from langchain_core.documents import Document


def _node_to_document(node: Any, content_key: str) -> Document:
    props = dict(node.properties)
    content = str(props.pop(content_key, ""))
    metadata: dict[str, Any] = {"node_id": node.id, "labels": node.labels}
    metadata.update(props)
    return Document(page_content=content, metadata=metadata)


class GraphDBLoader(BaseLoader):
    """LangChain document loader: streams graphdb nodes as Documents.

    ``page_content`` comes from ``content_key`` (default ``"text"``); the node id,
    labels, and remaining properties become metadata. Pass an ``aclient``
    (AsyncGraphDBClient) to use ``alazy_load``.
    """

    def __init__(
        self,
        client: Any,
        *,
        label: str | None = None,
        content_key: str = "text",
        aclient: Any = None,
    ) -> None:
        self._client = client
        self._aclient = aclient
        self._label = label
        self._content_key = content_key

    def lazy_load(self) -> Iterator[Document]:
        for node in self._client.nodes.list(label=self._label):
            yield _node_to_document(node, self._content_key)

    async def alazy_load(self) -> AsyncIterator[Document]:
        if self._aclient is None:
            raise ValueError("GraphDBLoader requires an `aclient` for alazy_load")
        async for node in self._aclient.nodes.list(label=self._label):
            yield _node_to_document(node, self._content_key)
