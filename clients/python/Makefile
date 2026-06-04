.PHONY: install generate lint typecheck test
install:
	uv sync
generate:
	uv run datamodel-codegen --input ../../docs/internals/openapi.yaml --input-file-type openapi \
	  --output-model-type dataclasses.dataclass \
	  --output src/graphdb_client/_generated/models.py
lint:
	uv run ruff check src tests
typecheck:
	uv run mypy
test:
	uv run pytest
