package masking

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestPolicy_ApplyToStorageValues(t *testing.T) {
	tests := []struct {
		name   string
		policy *Policy
		props  map[string]storage.Value
		wantFn func(t *testing.T, out map[string]storage.Value)
	}{
		{
			name:   "nil policy passes through unchanged",
			policy: nil,
			props: map[string]storage.Value{
				"email": storage.StringValue("alice@example.com"),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				got, _ := out["email"].AsString()
				if got != "alice@example.com" {
					t.Errorf("nil policy mutated value: got %q", got)
				}
			},
		},
		{
			name: "empty properties returns empty",
			policy: &Policy{
				Properties: map[string]MaskingStrategy{"email": StrategyPartial},
			},
			props: map[string]storage.Value{},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				if len(out) != 0 {
					t.Errorf("empty input produced non-empty output: %v", out)
				}
			},
		},
		{
			name: "explicit policy property is masked, others pass through",
			policy: &Policy{
				Properties: map[string]MaskingStrategy{"email": StrategyPartial},
			},
			props: map[string]storage.Value{
				"email": storage.StringValue("alice@example.com"),
				"name":  storage.StringValue("Alice"),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				if out["email"].Type != storage.TypeString {
					t.Errorf("masked email lost TypeString: got %v", out["email"].Type)
				}
				email, _ := out["email"].AsString()
				if email == "alice@example.com" {
					t.Errorf("email was not masked: got %q", email)
				}
				if out["name"].Type != storage.TypeString {
					t.Errorf("name should pass through TypeString: got %v", out["name"].Type)
				}
				name, _ := out["name"].AsString()
				if name != "Alice" {
					t.Errorf("non-policy property mutated: got %q want %q", name, "Alice")
				}
			},
		},
		{
			name: "non-string value with explicit policy gets coerced to TypeString",
			policy: &Policy{
				Properties: map[string]MaskingStrategy{"salary": StrategyFull},
			},
			props: map[string]storage.Value{
				"salary": storage.IntValue(100000),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				if out["salary"].Type != storage.TypeString {
					t.Errorf("masked TypeInt should become TypeString: got %v", out["salary"].Type)
				}
				s, _ := out["salary"].AsString()
				if s == "100000" {
					t.Errorf("salary value was not masked: got %q", s)
				}
			},
		},
		{
			name: "auto-detect masks email-named property",
			policy: &Policy{
				AutoDetect: true,
			},
			props: map[string]storage.Value{
				"email":   storage.StringValue("alice@example.com"),
				"comment": storage.StringValue("hello world"),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				email, _ := out["email"].AsString()
				if email == "alice@example.com" {
					t.Errorf("auto-detect did not mask email: got %q", email)
				}
				comment, _ := out["comment"].AsString()
				if comment != "hello world" {
					t.Errorf("non-sensitive field mutated: got %q want %q", comment, "hello world")
				}
			},
		},
		{
			name: "auto-detect skips non-string typed values",
			policy: &Policy{
				AutoDetect: true,
			},
			props: map[string]storage.Value{
				"age": storage.IntValue(30),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				if out["age"].Type != storage.TypeInt {
					t.Errorf("TypeInt should pass through under auto-detect: got %v", out["age"].Type)
				}
				i, _ := out["age"].AsInt()
				if i != 30 {
					t.Errorf("age value mutated: got %d want %d", i, 30)
				}
			},
		},
		{
			name: "StrategyNone is a no-op even for named property",
			policy: &Policy{
				Properties: map[string]MaskingStrategy{"email": StrategyNone},
			},
			props: map[string]storage.Value{
				"email": storage.StringValue("alice@example.com"),
			},
			wantFn: func(t *testing.T, out map[string]storage.Value) {
				email, _ := out["email"].AsString()
				if email != "alice@example.com" {
					t.Errorf("StrategyNone mutated value: got %q", email)
				}
			},
		},
	}

	masker := NewMasker(DefaultMaskingConfig())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.policy.ApplyToStorageValues(tc.props, masker)
			tc.wantFn(t, out)
		})
	}
}

func TestPolicy_ApplyToStorageValues_DoesNotMutateInput(t *testing.T) {
	policy := &Policy{
		Properties: map[string]MaskingStrategy{"email": StrategyPartial},
	}
	masker := NewMasker(DefaultMaskingConfig())
	original := map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	}
	_ = policy.ApplyToStorageValues(original, masker)

	got, _ := original["email"].AsString()
	if got != "alice@example.com" {
		t.Errorf("input map was mutated: email is now %q", got)
	}
}

func TestPolicy_ApplyToStorageValues_NilMasker_PassesThrough(t *testing.T) {
	policy := &Policy{
		Properties: map[string]MaskingStrategy{"email": StrategyPartial},
	}
	props := map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	}
	out := policy.ApplyToStorageValues(props, nil)
	got, _ := out["email"].AsString()
	if got != "alice@example.com" {
		t.Errorf("nil masker should pass through (audit-observable gap), got %q", got)
	}
}
