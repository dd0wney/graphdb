package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const respondJSONEncodeFailedText = "The response could not be encoded as JSON. The operation may be complete. Read the state before you send the request again."

// respondJSON does not read any Server field, so a zero Server is enough.
func TestRespondJSON_UnencodableMapValueAnswers500(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()

	s.respondJSON(w, http.StatusOK, map[string]any{"x": math.Inf(-1)})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (body %q)", err, w.Body.String())
	}
	if body["message"] != respondJSONEncodeFailedText {
		t.Errorf("message = %v, want %q", body["message"], respondJSONEncodeFailedText)
	}
}

func TestRespondJSON_NaNInsideSliceAnswers500(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()

	s.respondJSON(w, http.StatusCreated, []any{math.NaN()})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (body %q)", err, w.Body.String())
	}
	if body["message"] != respondJSONEncodeFailedText {
		t.Errorf("message = %v, want %q", body["message"], respondJSONEncodeFailedText)
	}
}

func TestRespondJSON_SuccessKeepsStatusHeaderAndBytes(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()

	s.respondJSON(w, http.StatusCreated, map[string]any{"a": 1})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := w.Body.String(); got != "{\"a\":1}\n" {
		t.Errorf("body = %q, want %q", got, "{\"a\":1}\n")
	}
}

func TestRespondError_StillWritesStatusAndCode(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()

	s.respondError(w, http.StatusNotFound, "x")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if !strings.Contains(w.Body.String(), `"code":404`) {
		t.Errorf("body = %q, want it to contain \"code\":404", w.Body.String())
	}
}
