package tenantid

import (
	"encoding/json"
	"testing"
)

func TestEmptyAndDefault(t *testing.T) {
	if Empty != "" {
		t.Errorf("Empty: want zero value, got %q", Empty)
	}
	if !Empty.IsEmpty() {
		t.Error("Empty.IsEmpty() must return true")
	}
	if Default != "default" {
		t.Errorf("Default: want \"default\", got %q", Default)
	}
	if Default.IsEmpty() {
		t.Error("Default.IsEmpty() must return false")
	}
}

func TestString(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		got := TenantID("acme").String()
		if got != "acme" {
			t.Errorf("want %q, got %q", "acme", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := Empty.String(); got != "" {
			t.Errorf("want empty string, got %q", got)
		}
	})
}

// TestJSONRoundTrip pins the JSON wire format. TenantID flows through
// audit events, HTTP request/response bodies, and persisted state — a
// silent change in encoding would be a cross-cutting bug. The audit
// advisor flagged this as a 5-line test worth having.
func TestJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		id   TenantID
		want string
	}{
		{"non-empty", TenantID("acme-corp"), `"acme-corp"`},
		{"default", Default, `"default"`},
		{"empty", Empty, `""`},
		{"with hyphens", TenantID("a-b-c"), `"a-b-c"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.id)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if got := string(data); got != tc.want {
				t.Errorf("Marshal: want %s, got %s", tc.want, got)
			}

			var back TenantID
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if back != tc.id {
				t.Errorf("round-trip: want %q, got %q", tc.id, back)
			}
		})
	}
}

// TestMapKey pins the property the architecture audit (MED-3) relies on:
// TenantID must work as a map key. The internal tenant-keyed maps in
// pkg/storage and pkg/tenant migrate to map[TenantID]X in this PR; if
// Go ever stopped allowing defined string types as map keys this test
// would fail to compile (and so would the whole codebase).
func TestMapKey(t *testing.T) {
	m := map[TenantID]int{
		Default:                  1,
		TenantID("acme"):         2,
		TenantID("globex-corp"):  3,
	}

	if got := m[Default]; got != 1 {
		t.Errorf("Default lookup: want 1, got %d", got)
	}
	if got := m[TenantID("acme")]; got != 2 {
		t.Errorf("acme lookup: want 2, got %d", got)
	}
	if _, ok := m[TenantID("nonexistent")]; ok {
		t.Error("nonexistent lookup: want absent, got present")
	}

	// Confirm string-typed value can be used to look up via explicit conversion.
	rawID := "acme"
	if got := m[TenantID(rawID)]; got != 2 {
		t.Errorf("conversion lookup: want 2, got %d", got)
	}
}
