"""F2 GraphRAG retrieval — LangChain BaseRetriever wrapper.

Subclasses langchain_core.retrievers.BaseRetriever to make graphdb's
POST /v1/retrieve drop-in compatible with LangChain RAG pipelines.
The endpoint already returns LangChain-shape `documents`, so the
wrapper is a thin HTTP client that maps the response into
langchain_core.documents.Document instances.

The metadata.source.path field — the BFS path from contributing seed
to each chunk — is preserved in document.metadata. Downstream code
(e.g. a chain that explains *why* a citation is relevant) can read
it directly.

Example:

    from langchain_core.prompts import ChatPromptTemplate
    from langchain_openai import ChatOpenAI

    retriever = GraphDBRetriever(
        base_url="http://localhost:8080",
        token=os.environ["GRAPHDB_TOKEN"],
        k=10,
        max_hops=2,
    )

    prompt = ChatPromptTemplate.from_messages([
        ("system", "Answer using the provided context. Cite node IDs."),
        ("human", "{question}\\n\\nContext:\\n{context}"),
    ])

    chain = (
        {"context": retriever | format_docs_with_paths,
         "question": RunnablePassthrough()}
        | prompt
        | ChatOpenAI()
    )

    chain.invoke("What does Alice know about graph databases?")

This file is an example; not auto-published to PyPI. Copy it into
your project and adapt as needed.

Dependencies:
    pip install langchain-core requests
"""

from __future__ import annotations

import logging
from typing import Any, List, Optional

import requests
from langchain_core.callbacks import CallbackManagerForRetrieverRun
from langchain_core.documents import Document
from langchain_core.retrievers import BaseRetriever
from pydantic import Field

logger = logging.getLogger(__name__)


class GraphDBRetriever(BaseRetriever):
    """Graph-augmented retrieval against a graphdb POST /v1/retrieve endpoint.

    Each returned Document carries the graph signal in metadata:

        metadata = {
            "node_id": int,           # the chunk's node ID
            "score": float,           # combined seed + graph-distance score
            "source": {
                "node_id": int,       # same as top-level node_id (LangChain convention)
                "label": str,         # primary label of the node
                "path": list[int],    # BFS path: [seed, ..., node_id]
            },
            "node": {...} | None,     # full node when include_node=True
        }

    The `source.path` is the F2 spike's load-bearing graph signal —
    without it, graphdb retrieval is indistinguishable from a vector
    retriever. Inspect or pass-through downstream as you'd handle any
    other LangChain document metadata.
    """

    base_url: str
    token: str
    k: int = 10
    max_hops: int = 2
    max_tokens: int = 4096
    alpha: Optional[float] = None
    beta: Optional[float] = None
    tau: Optional[float] = None
    labels: Optional[List[str]] = None
    include_node: bool = False
    timeout: float = 30.0
    session: Any = Field(default=None, exclude=True)

    def model_post_init(self, _context: Any) -> None:
        # Reuse a single requests.Session for connection pooling.
        # The Field(exclude=True) keeps it out of pydantic's model_dump.
        if self.session is None:
            self.session = requests.Session()

    def _get_relevant_documents(
        self,
        query: str,
        *,
        run_manager: CallbackManagerForRetrieverRun,
    ) -> List[Document]:
        body = {
            "query": query,
            "k": self.k,
            "max_hops": self.max_hops,
            "max_tokens": self.max_tokens,
            "include_node": self.include_node,
        }
        # Pointer-typed coefficients on the server side: only send if
        # the caller explicitly set them. This preserves "use the
        # default" behavior when alpha/beta/tau are None.
        if self.alpha is not None:
            body["alpha"] = self.alpha
        if self.beta is not None:
            body["beta"] = self.beta
        if self.tau is not None:
            body["tau"] = self.tau
        if self.labels:
            body["labels"] = self.labels

        url = self.base_url.rstrip("/") + "/v1/retrieve"
        headers = {
            "Authorization": f"Bearer {self.token}",
            "Content-Type": "application/json",
        }

        response = self.session.post(url, json=body, headers=headers, timeout=self.timeout)
        response.raise_for_status()
        data = response.json()

        if data.get("degraded"):
            # Hybrid-search degradation modes ("no-lsa-index",
            # "query-out-of-vocabulary"). Surface as a warning; the
            # retrieval still succeeds, just with a single-stage
            # fallback. The X-GraphDB-Retrieve-Degraded response
            # header carries the same value.
            logger.warning("graphdb retrieve degraded: %s", data["degraded"])

        documents: List[Document] = []
        for doc in data.get("documents", []):
            documents.append(
                Document(
                    page_content=doc.get("page_content", ""),
                    metadata=doc.get("metadata", {}),
                )
            )
        return documents


# ---------------------------------------------------------------
# Helpers callers commonly want when wiring into a prompt.
# ---------------------------------------------------------------


def format_docs_with_paths(docs: List[Document]) -> str:
    """Render documents as a context block that preserves graph paths.

    Each chunk is annotated with `[node_id={n} path={a→b→c}]` so the
    downstream LLM can cite the path that surfaced it. This is the
    minimal use of metadata.source.path; more sophisticated formats
    (per-chunk relevance scoring, per-tenant attribution, etc.) build
    on the same field.
    """
    blocks: List[str] = []
    for doc in docs:
        meta = doc.metadata or {}
        nid = meta.get("node_id", "?")
        source = meta.get("source", {}) or {}
        path = source.get("path", [])
        path_str = "→".join(str(p) for p in path) if path else "(seed)"
        blocks.append(f"[node_id={nid} path={path_str}]\n{doc.page_content}")
    return "\n\n".join(blocks)


if __name__ == "__main__":
    # Smoke-test the wrapper. Requires GRAPHDB_URL + GRAPHDB_TOKEN
    # and a seeded corpus (run examples/retrieve-curl.sh first).
    import os
    import sys

    base_url = os.environ.get("GRAPHDB_URL", "http://localhost:8080")
    token = os.environ.get("GRAPHDB_TOKEN")
    if not token:
        sys.exit("GRAPHDB_TOKEN not set. Export a valid Bearer token before running.")

    retriever = GraphDBRetriever(
        base_url=base_url,
        token=token,
        k=5,
        max_hops=2,
    )
    docs = retriever.invoke("graph database research")

    print(f"Retrieved {len(docs)} document(s):")
    for i, d in enumerate(docs):
        meta = d.metadata or {}
        path = (meta.get("source") or {}).get("path", [])
        print(f"  [{i}] node_id={meta.get('node_id')} score={meta.get('score'):.3f} path={path}")
        snippet = d.page_content[:100].replace("\n", " ")
        print(f"      {snippet}...")
