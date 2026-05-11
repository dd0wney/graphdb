package masking

import (
	"testing"
)

func TestPolicy_Apply_NilReceiver(t *testing.T) {
	var p *Policy
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"email": "alice@example.com"}
	out := p.Apply(props, masker)
	if out["email"] != "alice@example.com" {
		t.Errorf("nil Policy should pass through; got %q", out["email"])
	}
}

func TestPolicy_Apply_EmptyProps(t *testing.T) {
	p := &Policy{Properties: map[string]MaskingStrategy{"email": StrategyFull}}
	masker := NewMasker(DefaultMaskingConfig())
	out := p.Apply(map[string]any{}, masker)
	if len(out) != 0 {
		t.Errorf("empty props should produce empty out; got %v", out)
	}
}

func TestPolicy_Apply_NilMasker(t *testing.T) {
	// Defensive: missing masker should pass through, not panic.
	p := &Policy{Properties: map[string]MaskingStrategy{"email": StrategyFull}}
	props := map[string]any{"email": "alice@example.com"}
	out := p.Apply(props, nil)
	if out["email"] != "alice@example.com" {
		t.Errorf("nil masker should pass through; got %q", out["email"])
	}
}

func TestPolicy_Apply_ExplicitStrategyWins(t *testing.T) {
	p := &Policy{
		Properties: map[string]MaskingStrategy{"email": StrategyRedact},
		AutoDetect: true, // Also on — explicit must still win.
	}
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"email": "alice@example.com"}
	out := p.Apply(props, masker)
	if out["email"] != "[REDACTED]" {
		t.Errorf("StrategyRedact should produce [REDACTED]; got %q", out["email"])
	}
}

func TestPolicy_Apply_AutoDetectFiresOnly_WhenEnabled(t *testing.T) {
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"email": "alice@example.com"}

	pOff := &Policy{AutoDetect: false}
	out := pOff.Apply(props, masker)
	if out["email"] != "alice@example.com" {
		t.Errorf("AutoDetect=false should pass through; got %q", out["email"])
	}

	pOn := &Policy{AutoDetect: true}
	out = pOn.Apply(props, masker)
	if out["email"] == "alice@example.com" {
		t.Errorf("AutoDetect=true should mask known field name; got unchanged %q", out["email"])
	}
}

func TestPolicy_Apply_NonStringValue_NamedProperty(t *testing.T) {
	// Operator named "ssn" in policy; value happens to be an int.
	// Per design: coerce to string, mask.
	p := &Policy{Properties: map[string]MaskingStrategy{"ssn": StrategyFull}}
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"ssn": 123456789}
	out := p.Apply(props, masker)
	s, ok := out["ssn"].(string)
	if !ok {
		t.Fatalf("masked int should become string; got %T = %v", out["ssn"], out["ssn"])
	}
	// "123456789" → maskFull with MaskChar '*' → 9 asterisks.
	if s != "*********" {
		t.Errorf("StrategyFull on int(123456789) → %q, want 9 asterisks", s)
	}
}

func TestPolicy_Apply_NonStringValue_AutoDetect(t *testing.T) {
	// AutoDetect is regex-based; on a non-string value it should pass
	// through (no name match either).
	p := &Policy{AutoDetect: true}
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"age": 30}
	out := p.Apply(props, masker)
	if out["age"] != 30 {
		t.Errorf("AutoDetect on int should pass through; got %v", out["age"])
	}
}

func TestPolicy_Apply_DoesNotMutateInput(t *testing.T) {
	p := &Policy{Properties: map[string]MaskingStrategy{"email": StrategyFull}}
	masker := NewMasker(DefaultMaskingConfig())
	in := map[string]any{"email": "alice@example.com", "age": 30}

	out := p.Apply(in, masker)

	if in["email"] != "alice@example.com" {
		t.Errorf("input was mutated; email = %q", in["email"])
	}
	if out["email"] == "alice@example.com" {
		t.Errorf("output was not masked; email unchanged")
	}
}

func TestPolicy_Apply_StrategyNoneIsPassthrough(t *testing.T) {
	p := &Policy{Properties: map[string]MaskingStrategy{"email": StrategyNone}}
	masker := NewMasker(DefaultMaskingConfig())
	props := map[string]any{"email": "alice@example.com"}
	out := p.Apply(props, masker)
	if out["email"] != "alice@example.com" {
		t.Errorf("StrategyNone should pass through; got %q", out["email"])
	}
}

func TestPolicy_Apply_HashIsDeterministic(t *testing.T) {
	p := &Policy{Properties: map[string]MaskingStrategy{"email": StrategyHash}}
	masker := NewMasker(DefaultMaskingConfig())
	first := p.Apply(map[string]any{"email": "alice@example.com"}, masker)
	second := p.Apply(map[string]any{"email": "alice@example.com"}, masker)
	if first["email"] != second["email"] {
		t.Errorf("Hash should be deterministic; first=%q second=%q", first["email"], second["email"])
	}
	if first["email"] == "alice@example.com" {
		t.Errorf("Hash should change value; got unchanged %q", first["email"])
	}
}
