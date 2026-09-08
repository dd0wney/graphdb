package main

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestParseRequiredUniquenessRules_Empty pins that an unset/empty
// GRAPHDB_REQUIRED_UNIQUENESS_RULES is zero rules, not an error — most
// deployments never set this variable.
func TestParseRequiredUniquenessRules_Empty(t *testing.T) {
	rules, err := parseRequiredUniquenessRules("")
	if err != nil {
		t.Fatalf("empty value should not error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("rules = %+v, want empty", rules)
	}
}

// TestParseRequiredUniquenessRules_SingleAndMultiple pins the documented
// format: name=Label[,name=Label,...] (R8: underscore names, e.g.
// claim_for_task).
func TestParseRequiredUniquenessRules_SingleAndMultiple(t *testing.T) {
	rules, err := parseRequiredUniquenessRules("claim_for_task=Claim")
	if err != nil {
		t.Fatalf("single entry: %v", err)
	}
	want := []storage.RequiredUniquenessRule{{Name: "claim_for_task", Label: "Claim"}}
	if len(rules) != 1 || rules[0] != want[0] {
		t.Fatalf("rules = %+v, want %+v", rules, want)
	}

	rules, err = parseRequiredUniquenessRules("claim_for_task=Claim,invoice_number=Invoice")
	if err != nil {
		t.Fatalf("multiple entries: %v", err)
	}
	wantMulti := []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
		{Name: "invoice_number", Label: "Invoice"},
	}
	if len(rules) != len(wantMulti) {
		t.Fatalf("rules = %+v, want %+v", rules, wantMulti)
	}
	for i := range wantMulti {
		if rules[i] != wantMulti[i] {
			t.Errorf("rules[%d] = %+v, want %+v", i, rules[i], wantMulti[i])
		}
	}
}

// TestParseRequiredUniquenessRules_MalformedIsAnError pins that a
// malformed value is a startup error, not a warning: a missing "=", an
// empty name, an empty label, and an empty entry between commas must all
// fail, so an operator's typo cannot silently start the server with a
// weaker required list than intended.
func TestParseRequiredUniquenessRules_MalformedIsAnError(t *testing.T) {
	cases := []string{
		"claim_for_task",           // no "="
		"=Claim",                   // empty name
		"claim_for_task=",          // empty label
		"claim_for_task=Claim,,",   // empty entry
		"claim_for_task=Claim,bad", // second entry malformed
		"claim-for-task=Claim",     // hyphenated name (R8: underscores, not hyphens)
		"1claim_for_task=Claim",    // name starting with a digit
	}
	for _, raw := range cases {
		if _, err := parseRequiredUniquenessRules(raw); err == nil {
			t.Errorf("parseRequiredUniquenessRules(%q) should error, got nil", raw)
		}
	}
}
