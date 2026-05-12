# Code Quality Audit: graphdb

**Date**: 2026-05-06  
**Scope**: 77K LOC production, 102K LOC tests, 42 packages  
**Context**: Post-Track A lint cleanup (errcheck, ineffassign, unused-append closed). Focus: design, naming, error handling, test patterns, API hygiene.

---

## Strengths (Calibration)

1. **`sanitizeError` helper well-applied across handlers**: `/pkg/api/handler_helper.go:17` defines a consistent pattern—internal errors logged, generic message to client. Used throughout `handlers_*.go` with correct operation context.

2. **Fluent request decoder pattern**: `/pkg/api/handler_helper.go:40-115` (`requestDecoder`) and method router (`methodRouter`) provide clean abstraction for validation chains, reducing boilerplate and improving readability vs. nested if-statements.

---

## Findings

### HIGH

**1. fmt.Errorf wrapping inconsistency in handler helper functions**  
**File**: `/pkg/api/handlers_algorithms_generic.go:170, 186, 201, 265, 294, 309`  
**Issue**: Functions like `executePageRank` and `executeBetweenness` return `fmt.Errorf("%s", sanitizeError(...))`, which defeats error wrapping. The `%s` verb stringifies the error, losing the original error chain. Later code cannot use `errors.Is` or `errors.As`.  
**Impact**: Errors are converted to strings prematurely; debugging and testing become harder. Inconsistent with successful wraps elsewhere (e.g., `handler_helper.go:55`).  
**Fix**: Return `fmt.Errorf("operation failed: %w", err)` directly; let caller decide sanitization.

---

### HIGH

**2. Duplicate path-extraction logic without helper**  
**Files**: `/pkg/api/handlers_vectors.go:199-200, 220-221`; `/pkg/api/handlers_apikeys.go:65-66`; `/pkg/api/handlers_tenant.go:299, 355, 402, 477`  
**Issue**: `strings.TrimPrefix(r.URL.Path, prefix) + strings.TrimSuffix(..., "/")` repeated 8+ times. Already has `NewPathExtractor` utility in `handler_helper.go` for uint64 IDs but not for string paths.  
**Impact**: Error-prone (easy to forget TrimSuffix); violates DRY. Maintenance burden if URL path convention changes.  
**Fix**: Add `ExtractString(prefix)` method to `pathIDExtractor` or create a thin wrapper `s.ExtractPathSegment(r, prefix)`.

---

### MEDIUM

**3. Test file size: handlers_vectors_test.go at 1346 LOC**  
**File**: `/pkg/api/handlers_vectors_test.go`  
**Issue**: Single file with 19+ test funcs. Tests like `TestVectorSearch_PropertyFilter_*` (lines 1042+) repeat setup boilerplate. Helper `vectorSearchPropertyFilter` exists but only for one case family.  
**Impact**: Hard to find specific test, difficult to refactor; slow feedback on failures in one func. Elsewhere (handlers_search_test.go, handlers_hybrid_search_test.go) are 279 and 297 lines—good pattern.  
**Fix**: Extract property-filter tests into `handlers_vectors_property_filter_test.go`; consolidate HNSW parameter tests into a table-driven helper.

---

### MEDIUM

**4. Error messages constructed by string concatenation**  
**Files**: `/pkg/api/handlers_vectors.go:178, 208, 229, 291` (and similar in other handlers)  
**Issue**: `"Vector index already exists for property: " + req.PropertyName` and similar; some handlers use `fmt.Sprintf` (e.g., `handlers_vectors.go:284`), others use `+`.  
**Impact**: Inconsistent style; harder to grep/refactor error messages; no structured logging hook point.  
**Fix**: Standardize on `fmt.Sprintf("vector index already exists: %s", req.PropertyName)` everywhere, or introduce an `errorMsg()` helper.

---

### MEDIUM

**5. Missing godocs on exported helper types**  
**Files**: `/pkg/api/handler_helper.go:31-37` (`requestDecoder`), line 118-122 (`pathIDExtractor`), line 153 (`propertyConverter`)  
**Issue**: Public types in handler_helper (used by generated code / tests) lack package-level comments. Exported methods documented inline but types not.  
**Impact**: IDE tooltip and `godoc` don't explain purpose of these helpers to new developers.  
**Fix**: Add one-line godoc before each type: `// requestDecoder decodes and validates HTTP request bodies.`

---

### MEDIUM

**6. Unexported field exposed via method: PropertyConverter.Data**  
**File**: `/pkg/api/handler_helper.go:160-168`  
**Issue**: `ConvertAndSanitize` method on `propertyConverter` is unexported but the helper is instantiated inline in handlers. If the type becomes public, the signature forces callers to pass a converter func, but no way to get the Converter state back.  
**Impact**: Minor API design smell—either truly internal (lowercase) or document the converter contract clearly.  
**Fix**: Clarify intent: keep private if only used in one file, or export with full godoc if it becomes a general pattern.

---

### LOW

**7. Magic numbers scattered without named constants**  
**Files**: `/pkg/api/handlers_vectors.go:161` (`maxDimensions = 4096`), line 78 (`maxK = 1000`); multiple handlers use hardcoded limits  
**Issue**: Some limits are const-ified (good), but other handlers (e.g., handlers_search.go:40 `searchSnippetRunes = 160`) define in one place. No centralized config for policy limits.  
**Impact**: Limits buried in handler code; hard to enforce consistency across services; accidental double-definition of same limit.  
**Fix**: Create `pkg/api/config.go` with a `HandlerDefaults` struct (maxVectorDimensions, maxSearchLimit, snippetLen, etc.). Reference during init, not inline in handlers.

---

### LOW

**8. Naming: handleVectorSearch is a routing pass-through; vectorSearch is the logic**  
**File**: `/pkg/api/handlers_vectors.go:123-128` vs. line 243  
**Issue**: Router `handleVectorSearch` only checks method and delegates to `vectorSearch`. Pattern is used in handlers_vectors (good for consistency), but handlers_search.go:46 (`handleSearch`) implements logic directly. No naming convention enforced.  
**Impact**: Inconsistent reader expectations; unclear which functions are routing vs. logic without grep.  
**Fix**: Document pattern in comment or enforce naming: routing funcs always use `handle*`, logic always uses `*`. Apply uniformly or remove routing layer for simple endpoints.

---

## Summary Table

| Category | Count | Severity |
|----------|-------|----------|
| Error handling (wrapping, messages) | 2 | HIGH/MEDIUM |
| Code duplication (path extraction) | 1 | HIGH |
| Test organization | 1 | MEDIUM |
| Documentation (godocs) | 2 | MEDIUM/LOW |
| Design consistency (naming, config) | 2 | LOW |

---

## Recommended Action Priority

1. **Fix HIGH #1 + #2** before next merge (error wrapping and path helper)—both affect debuggability and maintenance across multiple handlers.
2. **Refactor test file** when touching vector search logic next.
3. **Standardize error messages** as part of the next error-handling pass.

All findings are isolated to `pkg/api`; storage and query layers show no systemic issues in this audit.
