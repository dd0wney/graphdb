package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// EmbeddingsRequest mirrors the OpenAI /v1/embeddings request shape so that
// clients which already speak OpenAI (LangChain, Vercel AI SDK, llamaindex,
// etc.) can drop in by setting api_base. The OpenAI fields not listed below
// (e.g. `user`) are accepted and silently ignored.
//
// Input accepts either a single string OR an array of strings — both shapes
// are valid per the OpenAI spec, decoded by the embeddingsInput type.
//
// The Model field is recorded but not validated. The only available backend
// is the per-tenant LSA index; pass any string. The model returned in the
// response is always lsaModelName regardless of the request's Model.
//
// EncodingFormat must be "float" or empty (base64 is not supported in v1).
//
// Dimensions, if non-zero, must match the LSA index's actual dimension or
// the request is rejected. This catches client misconfigurations that would
// otherwise produce silently-wrong-shape vectors.
type EmbeddingsRequest struct {
	Input          embeddingsInput `json:"input"`
	Model          string          `json:"model,omitempty"`
	EncodingFormat string          `json:"encoding_format,omitempty"`
	Dimensions     int             `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"` // ignored; tenant from context
}

// embeddingsInput is the OpenAI-shaped input field that accepts either a
// single string or an array of strings. The custom UnmarshalJSON handles both.
type embeddingsInput []string

func (e *embeddingsInput) UnmarshalJSON(data []byte) error {
	// Try array first.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*e = arr
		return nil
	}
	// Fall back to single string.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("input must be a string or array of strings")
	}
	*e = []string{s}
	return nil
}

// EmbeddingsResponse mirrors the OpenAI /v1/embeddings response shape.
type EmbeddingsResponse struct {
	Object string          `json:"object"` // always "list"
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingsUsage `json:"usage"`
}

// EmbeddingData is one embedding result.
type EmbeddingData struct {
	Object    string    `json:"object"` // always "embedding"
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"` // matches the position in the request's input array
}

// EmbeddingsUsage approximates OpenAI's token-usage accounting. Since LSA
// doesn't have an OpenAI-equivalent tokenizer, prompt_tokens is the count
// of LSA tokens after stemming/stop-word removal; both fields are equal
// because there are no completion tokens for an embeddings request.
type EmbeddingsUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// lsaModelName is the model identifier returned in /v1/embeddings responses.
// Versioned because the LSA configuration (Dims=200, Seed=42, etc.) is part
// of the contract — if those values change the model name should bump.
const lsaModelName = "graphdb-lsa-v1"

// handleEmbeddings serves POST /v1/embeddings.
//
// Backend: per-tenant LSA index. Trade-offs (documented for the caller):
//   - Vocabulary-bound. Out-of-vocab queries return 400, not a zero vector.
//     OpenAI's neural models always return *some* vector; LSA cannot.
//   - Scale ceiling ~100K-500K docs at the default 200 dims (audit perf finding).
//   - LSA must be built first via POST /hybrid-search/lsa-index. Calling
//     /v1/embeddings before the index exists returns 503.
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	start := time.Now()

	var req EmbeddingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Input) == 0 {
		s.respondError(w, http.StatusBadRequest, "input is required and must be non-empty")
		return
	}

	if req.EncodingFormat != "" && req.EncodingFormat != "float" {
		s.respondError(w, http.StatusBadRequest,
			fmt.Sprintf("encoding_format %q not supported (only \"float\")", req.EncodingFormat))
		return
	}

	tenantID := getTenantFromContext(r)
	lsa := s.lsaIndexes.Get(tenantID)
	if lsa == nil {
		s.respondError(w, http.StatusServiceUnavailable,
			"LSA index not built for this tenant; build via POST /hybrid-search/lsa-index")
		return
	}

	// Catch dimension misconfigurations early. Without this check, a client
	// expecting 1536-dim OpenAI vectors would silently get 200-dim LSA
	// vectors and dot-product math downstream would produce wrong-shape errors
	// far from the request boundary.
	if req.Dimensions > 0 && req.Dimensions != lsa.Dimensions() {
		s.respondError(w, http.StatusBadRequest,
			fmt.Sprintf("dimensions=%d does not match this tenant's LSA index dimension %d",
				req.Dimensions, lsa.Dimensions()))
		return
	}

	data := make([]EmbeddingData, 0, len(req.Input))
	totalTokens := 0

	for i, input := range req.Input {
		if input == "" {
			s.respondError(w, http.StatusBadRequest,
				fmt.Sprintf("input[%d] is empty", i))
			return
		}

		vec, tokens, err := lsa.FoldQuery(input)
		if err != nil {
			// FoldQuery error means out-of-vocab or zero-vector projection.
			// Don't echo the input string in the response (caller already
			// has it; we'd just be wasting bytes). Don't leak FoldQuery's
			// internal phrasing either — keep the client-facing message stable.
			log.Printf("embeddings: tenant=%s input[%d]: %v", tenantID, i, err)
			s.respondError(w, http.StatusBadRequest,
				fmt.Sprintf("input[%d]: out of vocabulary or zero-vector projection (LSA is vocabulary-bound; ensure the index covers your query terms)", i))
			return
		}

		data = append(data, EmbeddingData{
			Object:    "embedding",
			Embedding: vec,
			Index:     i,
		})
		totalTokens += len(tokens)
	}

	log.Printf("embeddings: tenant=%s n=%d took_ms=%d",
		tenantID, len(req.Input), time.Since(start).Milliseconds())

	s.respondJSON(w, http.StatusOK, EmbeddingsResponse{
		Object: "list",
		Data:   data,
		Model:  lsaModelName,
		Usage: EmbeddingsUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	})
}
