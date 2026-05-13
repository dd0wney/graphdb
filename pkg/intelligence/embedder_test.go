package intelligence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestErrNoIndexForTenant_Message pins the error's user-facing format —
// includes the tenant ID for actionability without leaking other tenants.
func TestErrNoIndexForTenant_Message(t *testing.T) {
	err := ErrNoIndexForTenant{TenantID: "acme"}
	msg := err.Error()
	if !strings.Contains(msg, "acme") {
		t.Errorf("Error() = %q; expected tenantID %q to appear", msg, "acme")
	}
	if !strings.Contains(strings.ToLower(msg), "embedder") {
		t.Errorf("Error() = %q; expected subsystem prefix %q to appear", msg, "embedder")
	}
	if !strings.Contains(strings.ToLower(msg), "no index") {
		t.Errorf("Error() = %q; expected reason %q to appear", msg, "no index")
	}
}

// TestErrNoIndexForTenant_TenantIDPreserved pins that the TenantID field
// is preserved on the struct value, so callers can use errors.As to pull
// it for logs / metrics without parsing the message string.
func TestErrNoIndexForTenant_TenantIDPreserved(t *testing.T) {
	want := "tenant-42"
	err := ErrNoIndexForTenant{TenantID: want}
	if err.TenantID != want {
		t.Errorf("TenantID = %q, want %q", err.TenantID, want)
	}
}

// TestErrNoIndexForTenant_ErrorsAs pins errors.As compatibility. Callers
// that branch on this condition use errors.As to extract the typed error
// from a wrapped chain.
func TestErrNoIndexForTenant_ErrorsAs(t *testing.T) {
	original := ErrNoIndexForTenant{TenantID: "tenant-X"}
	wrapped := fmt.Errorf("dispatch: %w", original)

	var extracted ErrNoIndexForTenant
	if !errors.As(wrapped, &extracted) {
		t.Fatalf("errors.As did not extract ErrNoIndexForTenant from %v", wrapped)
	}
	if extracted.TenantID != "tenant-X" {
		t.Errorf("extracted.TenantID = %q, want %q", extracted.TenantID, "tenant-X")
	}
}

// TestEmbedderInterfaceCompiles pins the contract: a value implementing
// Embed(ctx, tenantID, text) ([]float32, error) satisfies Embedder. This
// is a compile-time check disguised as a test — if the interface signature
// drifts, the test stops compiling.
func TestEmbedderInterfaceCompiles(t *testing.T) {
	var _ Embedder = fakeEmbedder{}
}

// fakeEmbedder is a minimal Embedder used to pin the interface signature.
// It is NOT a usable implementation — it returns the empty-tenant error
// for any call, exercising both the typed-error pattern and the contract
// that mock/placeholder vectors are forbidden alongside a nil error.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(_ context.Context, tenantID string, _ string) ([]float32, error) {
	return nil, ErrNoIndexForTenant{TenantID: tenantID}
}

// TestFakeEmbedder_ReturnsTypedError demonstrates the canonical
// non-mock-fallback pattern: the embedder returns (nil, typed-error)
// rather than ([]float32{...fake values...}, nil). This is the
// structural prevention of S11 spike §1.3's "facade risk."
func TestFakeEmbedder_ReturnsTypedError(t *testing.T) {
	var e Embedder = fakeEmbedder{}
	vec, err := e.Embed(context.Background(), "acme", "hello")
	if vec != nil {
		t.Errorf("Embed() vec = %v, want nil (no mock fallback)", vec)
	}
	if err == nil {
		t.Fatal("Embed() err = nil, want ErrNoIndexForTenant")
	}
	var typed ErrNoIndexForTenant
	if !errors.As(err, &typed) {
		t.Errorf("Embed() err = %v, not ErrNoIndexForTenant", err)
	}
	if typed.TenantID != "acme" {
		t.Errorf("typed.TenantID = %q, want \"acme\"", typed.TenantID)
	}
}
