#!/usr/bin/env python3
"""Deterministic OpenAI-compatible embeddings server for consumer-drive.sh.

A hashing vectorizer: tokenize, hash each token to a dimension + sign, accumulate,
L2-normalize. Deterministic (same text -> same vector) and crudely lexical, enough to drive +
assert understand-graphdb's neural search without any model or API key.

POST <any path>  body {"model":..., "input": str | [str,...]}
                 -> {"data":[{"embedding":[...]}], "model":..., "object":"list"}
Listens on 127.0.0.1:8090.
"""
import hashlib
import json
import math
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DIMS = 64
_token = re.compile(r"[A-Za-z0-9_]+")


def embed(text: str) -> list[float]:
    vec = [0.0] * DIMS
    toks = _token.findall(text.lower()) or ["__empty__"]
    for tok in toks:
        h = hashlib.md5(tok.encode()).digest()
        vec[h[0] % DIMS] += 1.0 if (h[1] & 1) else -1.0
    norm = math.sqrt(sum(x * x for x in vec)) or 1.0
    return [x / norm for x in vec]


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        inp = body.get("input", [])
        texts = [inp] if isinstance(inp, str) else list(inp)
        payload = json.dumps({
            "object": "list",
            "data": [{"object": "embedding", "index": i, "embedding": embed(t)} for i, t in enumerate(texts)],
            "model": body.get("model", "deterministic-hash-64"),
            "usage": {"prompt_tokens": 0, "total_tokens": 0},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 8090), Handler).serve_forever()
