package masking

import (
	"errors"
	"sort"
	"testing"
	"time"
)

func TestPolicyStore_GetMissing(t *testing.T) {
	store := NewPolicyStore()
	_, err := store.Get("tenant-a")
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("expected ErrPolicyNotFound, got %v", err)
	}
}

func TestPolicyStore_SetThenGet(t *testing.T) {
	store := NewPolicyStore()
	p := &Policy{
		Properties: map[string]MaskingStrategy{
			"email": StrategyPartial,
			"ssn":   StrategyFull,
		},
		AutoDetect: true,
	}
	store.Set("tenant-a", p)

	got, err := store.Get("tenant-a")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("TenantID = %q, want %q", got.TenantID, "tenant-a")
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt is zero; Set should stamp it")
	}
	if got.Properties["email"] != StrategyPartial {
		t.Errorf("email strategy = %q, want %q", got.Properties["email"], StrategyPartial)
	}
	if !got.AutoDetect {
		t.Errorf("AutoDetect = false, want true")
	}
}

func TestPolicyStore_TenantIDOverriddenBySetArg(t *testing.T) {
	// Set must enforce the tenantID arg as authoritative — caller
	// can't accidentally store policy-for-A under key-B.
	store := NewPolicyStore()
	p := &Policy{TenantID: "tenant-b", Properties: map[string]MaskingStrategy{"x": StrategyFull}}
	store.Set("tenant-a", p)

	got, err := store.Get("tenant-a")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.TenantID != "tenant-a" {
		t.Errorf("Set should rewrite TenantID to arg; got %q, want %q", got.TenantID, "tenant-a")
	}

	_, err = store.Get("tenant-b")
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Errorf("expected tenant-b to have no policy (Set's tenantID arg should win), got %v", err)
	}
}

func TestPolicyStore_GetReturnsClone(t *testing.T) {
	// Mutating the returned policy must not affect the store's copy.
	store := NewPolicyStore()
	store.Set("tenant-a", &Policy{Properties: map[string]MaskingStrategy{"email": StrategyPartial}})

	got1, _ := store.Get("tenant-a")
	got1.Properties["email"] = StrategyFull
	got1.AutoDetect = true

	got2, _ := store.Get("tenant-a")
	if got2.Properties["email"] != StrategyPartial {
		t.Errorf("store's copy was mutated; email = %q, want %q", got2.Properties["email"], StrategyPartial)
	}
	if got2.AutoDetect {
		t.Errorf("store's copy AutoDetect was mutated to true")
	}
}

func TestPolicyStore_Delete(t *testing.T) {
	store := NewPolicyStore()
	store.Set("tenant-a", &Policy{Properties: map[string]MaskingStrategy{"email": StrategyPartial}})

	if !store.Delete("tenant-a") {
		t.Errorf("Delete returned false; expected true (policy was present)")
	}
	if store.Delete("tenant-a") {
		t.Errorf("Delete returned true on second call; expected false (already absent)")
	}
	_, err := store.Get("tenant-a")
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Errorf("Get after Delete returned %v, want ErrPolicyNotFound", err)
	}
}

func TestPolicyStore_Tenants(t *testing.T) {
	store := NewPolicyStore()
	store.Set("tenant-c", &Policy{})
	store.Set("tenant-a", &Policy{})
	store.Set("tenant-b", &Policy{})

	got := store.Tenants()
	sort.Strings(got)
	want := []string{"tenant-a", "tenant-b", "tenant-c"}
	if len(got) != len(want) {
		t.Fatalf("len(Tenants()) = %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("Tenants()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestPolicyStore_UpdatedAtMonotonic(t *testing.T) {
	store := NewPolicyStore()
	store.Set("tenant-a", &Policy{Properties: map[string]MaskingStrategy{"email": StrategyPartial}})
	first, _ := store.Get("tenant-a")

	time.Sleep(2 * time.Millisecond)

	store.Set("tenant-a", &Policy{Properties: map[string]MaskingStrategy{"email": StrategyFull}})
	second, _ := store.Get("tenant-a")

	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("UpdatedAt did not advance on second Set: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestPolicy_Clone(t *testing.T) {
	original := &Policy{
		TenantID:   "tenant-a",
		Properties: map[string]MaskingStrategy{"email": StrategyPartial},
		AutoDetect: true,
		UpdatedAt:  time.Now(),
	}
	cp := original.Clone()
	cp.Properties["email"] = StrategyFull
	cp.AutoDetect = false

	if original.Properties["email"] != StrategyPartial {
		t.Errorf("Clone shared the Properties map; original mutated")
	}
	if !original.AutoDetect {
		t.Errorf("Clone shared scalar AutoDetect somehow (impossible) or original is wrong")
	}
}

func TestPolicy_CloneNil(t *testing.T) {
	var p *Policy
	if p.Clone() != nil {
		t.Errorf("Clone on nil *Policy should return nil")
	}
}
