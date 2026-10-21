package storage

import (
	"errors"
	"strings"
	"testing"
)

const upTenant = "upsert-tenant"

func newUpsertStore(t *testing.T) *GraphStorage {
	t.Helper()
	cfg := DefaultStorageConfig(t.TempDir())
	cfg.UseMmapSnapshot = false
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func registerRule(t *testing.T, gs *GraphStorage, name, label, key string) {
	t.Helper()
	if err := gs.RegisterUniquenessRule(UniquenessRule{Name: name, Label: label, PropertyKey: key}); err != nil {
		t.Fatalf("RegisterUniquenessRule(%s): %v", name, err)
	}
}

func mustUpsert(t *testing.T, gs *GraphStorage, tenant string, labels []string, props map[string]Value, keyLabel, keyProp string) (*Node, bool) {
	t.Helper()
	n, created, err := gs.UpsertNodeForTenant(tenant, labels, props, keyLabel, keyProp)
	if err != nil {
		t.Fatalf("UpsertNodeForTenant: %v", err)
	}
	if n == nil {
		t.Fatal("UpsertNodeForTenant returned a nil node and no error")
	}
	return n, created
}

func countNodes(t *testing.T, gs *GraphStorage, tenant string) int {
	t.Helper()
	all, err := gs.GetAllNodesForTenant(tenant)
	if err != nil {
		t.Fatalf("GetAllNodesForTenant: %v", err)
	}
	return len(all)
}

func TestUpsertNode_CreateThenMatchMerges(t *testing.T) {
	gs := newUpsertStore(t)
	first, created := mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "os": StringValue("linux")}, "Host", "ip")
	if !created {
		t.Fatal("first upsert: created = false, want true")
	}

	second, created := mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "os": StringValue("bsd"), "owner": StringValue("ops")}, "Host", "ip")
	if created {
		t.Fatal("second upsert: created = true, want false")
	}
	if second.ID != first.ID {
		t.Fatalf("second upsert returned node %d, want %d", second.ID, first.ID)
	}
	if got, _ := second.Properties["os"].AsString(); got != "bsd" {
		t.Errorf("os = %q, want the request value bsd", got)
	}
	if got, _ := second.Properties["owner"].AsString(); got != "ops" {
		t.Errorf("owner = %q, want ops", got)
	}

	// An absent request property stays.
	third, _ := mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1")}, "Host", "ip")
	if got, _ := third.Properties["os"].AsString(); got != "bsd" {
		t.Errorf("os = %q after an upsert that omits it, want bsd kept", got)
	}
	if n := countNodes(t, gs, upTenant); n != 1 {
		t.Errorf("node count = %d, want 1", n)
	}
}

func TestUpsertNode_ReturnsOwnClone(t *testing.T) {
	gs := newUpsertStore(t)
	mustUpsert(t, gs, upTenant, []string{"Host"}, map[string]Value{"ip": StringValue("a")}, "Host", "ip")
	n, _ := mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("a"), "x": IntValue(1)}, "Host", "ip")
	n.Properties["x"] = IntValue(99)
	n.Labels[0] = "Mutated"

	stored, err := gs.GetNodeForTenant(n.ID, upTenant)
	if err != nil {
		t.Fatalf("GetNodeForTenant: %v", err)
	}
	if v, _ := stored.Properties["x"].AsInt(); v != 1 {
		t.Errorf("stored x = %d after the caller mutated its copy, want 1", v)
	}
	if stored.Labels[0] != "Host" {
		t.Errorf("stored label = %q after the caller mutated its copy, want Host", stored.Labels[0])
	}
}

func TestUpsertNode_ArgumentChecks(t *testing.T) {
	gs := newUpsertStore(t)
	props := map[string]Value{"ip": StringValue("a")}
	cases := []struct {
		name              string
		labels            []string
		props             map[string]Value
		keyLabel, keyProp string
	}{
		{"empty key label", []string{"Host"}, props, "", "ip"},
		{"empty key property", []string{"Host"}, props, "Host", ""},
		{"key label not in labels", []string{"Host"}, props, "Asset", "ip"},
		{"key property absent", []string{"Host"}, map[string]Value{"os": StringValue("x")}, "Host", "ip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, created, err := gs.UpsertNodeForTenant(upTenant, tc.labels, tc.props, tc.keyLabel, tc.keyProp)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if n != nil || created {
				t.Errorf("got node %v created %v with an error", n, created)
			}
		})
	}
	if n := countNodes(t, gs, upTenant); n != 0 {
		t.Errorf("node count = %d after refused upserts, want 0", n)
	}
	// Positive control: a well-formed call on the same store works, so the
	// refusals above come from the checks and not from a broken method.
	if _, created := mustUpsert(t, gs, upTenant, []string{"Host"}, props, "Host", "ip"); !created {
		t.Error("well-formed upsert: created = false, want true")
	}
}

func TestUpsertNode_CrossTenant(t *testing.T) {
	gs := newUpsertStore(t)
	props := map[string]Value{"ip": StringValue("10.0.0.1")}
	a, createdA := mustUpsert(t, gs, "tenant-a", []string{"Host"}, props, "Host", "ip")
	b, createdB := mustUpsert(t, gs, "tenant-b", []string{"Host"}, props, "Host", "ip")
	if !createdA || !createdB {
		t.Fatalf("created = %v/%v, want true/true", createdA, createdB)
	}
	if a.ID == b.ID {
		t.Fatalf("two tenants share node %d", a.ID)
	}
	if countNodes(t, gs, "tenant-a") != 1 || countNodes(t, gs, "tenant-b") != 1 {
		t.Error("each tenant must hold exactly one node")
	}
}

func TestUpsertNode_AmbiguousKeyRefusesAndWritesNothing(t *testing.T) {
	gs := newUpsertStore(t)
	props := map[string]Value{"ip": StringValue("10.0.0.1"), "v": IntValue(1)}
	// Written before any rule exists, so the plain create allows both.
	d1, err := gs.CreateNodeWithTenant(upTenant, []string{"Host"}, props)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := gs.CreateNodeWithTenant(upTenant, []string{"Host"}, props)
	if err != nil {
		t.Fatal(err)
	}
	lsn := gs.GetCurrentLSN()

	n, created, err := gs.UpsertNodeForTenant(upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "v": IntValue(2)}, "Host", "ip")
	if !errors.Is(err, ErrUpsertAmbiguous) {
		t.Fatalf("err = %v, want ErrUpsertAmbiguous", err)
	}
	if n != nil || created {
		t.Errorf("got node %v created %v with an ambiguity error", n, created)
	}
	var amb *UpsertAmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("err %T is not *UpsertAmbiguousError", err)
	}
	if amb.Label != "Host" || amb.PropertyKey != "ip" || len(amb.NodeIDs) != 2 {
		t.Errorf("error fields = %+v, want Host/ip with two IDs (not just the first match)", amb)
	}
	got := map[uint64]bool{amb.NodeIDs[0]: true, amb.NodeIDs[1]: true}
	if !got[d1.ID] || !got[d2.ID] {
		t.Errorf("NodeIDs = %v, want %d and %d", amb.NodeIDs, d1.ID, d2.ID)
	}
	if gs.GetCurrentLSN() != lsn {
		t.Error("the refused upsert wrote a WAL entry")
	}
	for _, id := range []uint64{d1.ID, d2.ID} {
		stored, _ := gs.GetNodeForTenant(id, upTenant)
		if v, _ := stored.Properties["v"].AsInt(); v != 1 {
			t.Errorf("node %d v = %d, want 1 (unchanged)", id, v)
		}
	}
}

func TestUpsertNode_D3_LabelsMustBeSubset(t *testing.T) {
	gs := newUpsertStore(t)
	stored, _ := mustUpsert(t, gs, upTenant, []string{"Host", "Asset"},
		map[string]Value{"ip": StringValue("a"), "v": IntValue(1)}, "Host", "ip")
	lsn := gs.GetCurrentLSN()

	// A label the stored node lacks is refused and nothing is written.
	n, created, err := gs.UpsertNodeForTenant(upTenant, []string{"Host", "Server"},
		map[string]Value{"ip": StringValue("a"), "v": IntValue(2)}, "Host", "ip")
	if !errors.Is(err, ErrUpsertLabelsDiffer) {
		t.Fatalf("err = %v, want ErrUpsertLabelsDiffer", err)
	}
	if n != nil || created {
		t.Errorf("got node %v created %v with a labels error", n, created)
	}
	var ld *UpsertLabelsDifferError
	if !errors.As(err, &ld) || ld.NodeID != stored.ID || len(ld.Missing) != 1 || ld.Missing[0] != "Server" {
		t.Fatalf("error = %#v, want node %d missing [Server]", ld, stored.ID)
	}
	if gs.GetCurrentLSN() != lsn {
		t.Error("the refused upsert wrote a WAL entry")
	}
	after, _ := gs.GetNodeForTenant(stored.ID, upTenant)
	if v, _ := after.Properties["v"].AsInt(); v != 1 {
		t.Errorf("v = %d after a refused upsert, want 1", v)
	}

	// A subset (fewer labels than stored) is accepted and drops no label.
	n, created = mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("a"), "v": IntValue(3)}, "Host", "ip")
	if created || n.ID != stored.ID {
		t.Fatalf("subset upsert: created %v node %d, want match of %d", created, n.ID, stored.ID)
	}
	if len(n.Labels) != 2 {
		t.Errorf("labels = %v, want both stored labels kept", n.Labels)
	}
}

func TestUpsertNode_D4_NoChangeWritesNothing(t *testing.T) {
	gs := newUpsertStore(t)
	obs := &recordingObserver{}
	gs.AddObserver(obs)

	props := map[string]Value{"ip": StringValue("a"), "os": StringValue("linux")}
	first, _ := mustUpsert(t, gs, upTenant, []string{"Host"}, props, "Host", "ip")
	lsn := gs.GetCurrentLSN()
	if lsn == 0 {
		t.Fatal("create wrote no WAL entry, so the WAL count is not measuring anything")
	}
	obs.mu.Lock()
	updatedBefore := len(obs.updated)
	obs.mu.Unlock()

	// Same values, subset of stored properties.
	again, created := mustUpsert(t, gs, upTenant, []string{"Host"}, map[string]Value{"ip": StringValue("a")}, "Host", "ip")
	if created || again.ID != first.ID {
		t.Fatalf("no-change upsert: created %v node %d, want match of %d", created, again.ID, first.ID)
	}
	if got := gs.GetCurrentLSN(); got != lsn {
		t.Errorf("WAL LSN %d -> %d, want no entry for a no-change match", lsn, got)
	}
	obs.mu.Lock()
	if got := len(obs.updated) - updatedBefore; got != 0 {
		t.Errorf("observer got %d update notifications for a no-change match, want 0", got)
	}
	obs.mu.Unlock()

	// Positive control: a changing match does write and notify.
	mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("a"), "os": StringValue("bsd")}, "Host", "ip")
	if got := gs.GetCurrentLSN(); got != lsn+1 {
		t.Errorf("WAL LSN = %d after a changing match, want %d", got, lsn+1)
	}
	obs.mu.Lock()
	if got := len(obs.updated) - updatedBefore; got != 1 {
		t.Errorf("observer got %d update notifications for a changing match, want 1", got)
	}
	obs.mu.Unlock()
}

func TestUpsertNode_D1_ExplicitKeyMustMatchRegisteredRule(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "host_ip", "Host", "ip")
	lsn := gs.GetCurrentLSN()

	props := map[string]Value{"ip": StringValue("10.0.0.1"), "hostname": StringValue("h1")}
	n, created, err := gs.UpsertNodeForTenant(upTenant, []string{"Host"}, props, "Host", "hostname")
	if !errors.Is(err, ErrUpsertKeyNotRegistered) {
		t.Fatalf("err = %v, want ErrUpsertKeyNotRegistered", err)
	}
	if n != nil || created {
		t.Errorf("got node %v created %v with a key error", n, created)
	}
	var ke *UpsertKeyNotRegisteredError
	if !errors.As(err, &ke) || ke.KeyLabel != "Host" || ke.KeyProperty != "hostname" {
		t.Errorf("error = %#v, want the caller's own key Host.hostname", ke)
	}
	if msg := err.Error(); strings.Contains(msg, "host_ip") || strings.Contains(msg, "ip\"") {
		t.Errorf("error text %q names the rule", msg)
	}
	if gs.GetCurrentLSN() != lsn || countNodes(t, gs, upTenant) != 0 {
		t.Error("the refused upsert wrote something")
	}

	// Keyed on the registered key, the upsert proceeds.
	_, created = mustUpsert(t, gs, upTenant, []string{"Host"}, props, "Host", "ip")
	if !created {
		t.Error("upsert keyed on the registered key: created = false, want true")
	}
}

// With two rules covering the labels, an upsert keyed on one of them used to
// proceed, and its create branch never checked the other rule: a second node
// could then hold a hostname another node already holds. Every covering rule
// must be the explicit key, which with two distinct rules is never true, the
// same answer the create gives (ErrMultipleUniquenessRules).
func TestUpsertNode_D1_EveryCoveringRuleMustBeTheKey(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "host_ip", "Host", "ip")
	registerRule(t, gs, "host_name", "Host", "hostname")

	first := map[string]Value{"ip": StringValue("10.0.0.1"), "hostname": StringValue("h1")}
	if _, err := gs.CreateNodeWithUniquePropertyForTenant(upTenant, []string{"Host"}, first, "Host", "ip"); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	lsn := gs.GetCurrentLSN()

	second := map[string]Value{"ip": StringValue("10.0.0.2"), "hostname": StringValue("h1")}
	_, _, err := gs.UpsertNodeForTenant(upTenant, []string{"Host"}, second, "Host", "ip")
	if !errors.Is(err, ErrUpsertKeyNotRegistered) {
		t.Fatalf("err = %v, want ErrUpsertKeyNotRegistered", err)
	}
	if gs.GetCurrentLSN() != lsn || countNodes(t, gs, upTenant) != 1 {
		t.Errorf("the refused upsert wrote something: nodes = %d", countNodes(t, gs, upTenant))
	}
}

func TestUpsertNode_D6_MergeMustNotBreakRuleOnStoredLabel(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "host_ip", "Host", "ip")
	registerRule(t, gs, "asset_serial", "Asset", "serial")

	// Another Asset holds serial S2.
	holder, _, err := gs.UpsertNodeForTenant(upTenant, []string{"Asset"},
		map[string]Value{"serial": StringValue("S2")}, "Asset", "serial")
	if err != nil {
		t.Fatal(err)
	}
	// The stored node is a Host and an Asset. A plain create, because an upsert
	// naming both labels is refused: two rules cover them (D1).
	stored, err := gs.CreateNodeWithTenant(upTenant, []string{"Host", "Asset"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "serial": StringValue("S1")})
	if err != nil {
		t.Fatal(err)
	}
	lsn := gs.GetCurrentLSN()

	// The request names Host only, matches by ip, and sets serial to S2.
	n, created, err := gs.UpsertNodeForTenant(upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "serial": StringValue("S2")}, "Host", "ip")
	if !errors.Is(err, ErrUniqueConstraintViolation) {
		t.Fatalf("err = %v, want a unique constraint violation", err)
	}
	var uc *UniqueConstraintError
	if !errors.As(err, &uc) || uc.ConflictingNodeID != holder.ID || uc.Label != "Asset" || uc.PropertyKey != "serial" {
		t.Errorf("error = %#v, want Asset.serial held by node %d", uc, holder.ID)
	}
	if n != nil || created {
		t.Errorf("got node %v created %v with a constraint error", n, created)
	}
	after, _ := gs.GetNodeForTenant(stored.ID, upTenant)
	if s, _ := after.Properties["serial"].AsString(); s != "S1" {
		t.Errorf("serial = %q after a refused upsert, want S1 (unchanged)", s)
	}
	if gs.GetCurrentLSN() != lsn {
		t.Error("the refused upsert wrote a WAL entry")
	}

	// Positive control: a free serial merges.
	n, created = mustUpsert(t, gs, upTenant, []string{"Host"},
		map[string]Value{"ip": StringValue("10.0.0.1"), "serial": StringValue("S3")}, "Host", "ip")
	if created || n.ID != stored.ID {
		t.Fatalf("free serial: created %v node %d, want match of %d", created, n.ID, stored.ID)
	}
	if s, _ := n.Properties["serial"].AsString(); s != "S3" {
		t.Errorf("serial = %q, want S3", s)
	}
}

func TestUpsertNodeWithRules(t *testing.T) {
	gs := newUpsertStore(t)
	props := map[string]Value{"ip": StringValue("10.0.0.1")}

	// 0 rules.
	_, _, err := gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, props)
	if !errors.Is(err, ErrNoUniquenessRule) {
		t.Fatalf("no rule: err = %v, want ErrNoUniquenessRule", err)
	}
	if countNodes(t, gs, upTenant) != 0 {
		t.Error("no rule: a node was created")
	}

	registerRule(t, gs, "host_ip", "Host", "ip")

	// Property absent.
	_, _, err = gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, map[string]Value{"os": StringValue("x")})
	if !errors.Is(err, ErrUniquenessRulePropertyMissing) {
		t.Fatalf("property absent: err = %v, want ErrUniquenessRulePropertyMissing", err)
	}

	// 1 rule: create then match.
	n1, created, err := gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, props)
	if err != nil || !created {
		t.Fatalf("first: created %v err %v, want created", created, err)
	}
	n2, created, err := gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, props)
	if err != nil || created || n2.ID != n1.ID {
		t.Fatalf("second: created %v node %v err %v, want match of %d", created, n2, err, n1.ID)
	}

	// 2 rules.
	registerRule(t, gs, "asset_serial", "Asset", "serial")
	_, _, err = gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host", "Asset"},
		map[string]Value{"ip": StringValue("x"), "serial": StringValue("y")})
	if !errors.Is(err, ErrMultipleUniquenessRules) {
		t.Fatalf("two rules: err = %v, want ErrMultipleUniquenessRules", err)
	}
}

func TestUpsertNodeWithRules_RequiredRuleMissing(t *testing.T) {
	cfg := DefaultStorageConfig(t.TempDir())
	cfg.UseMmapSnapshot = false
	cfg.RequiredUniquenessRules = []RequiredUniquenessRule{{Name: "host_ip", Label: "Host"}}
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gs.Close() }()

	_, _, err = gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, map[string]Value{"ip": StringValue("a")})
	var miss *RequiredRuleMissingError
	if !errors.As(err, &miss) {
		t.Fatalf("err = %v, want *RequiredRuleMissingError", err)
	}
}

func TestUpsertNode_LockPanicReleasesLock(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "UpsertNodeForTenant",
		func() {
			_, _, _ = gs.UpsertNodeForTenant("", []string{"P"}, map[string]Value{"k": IntValue(1)}, "P", "k")
		},
		updateAgain(gs, n))
}

// The interface must carry the methods, so a caller that holds a Storage can
// reach them.
var _ interface {
	UpsertNodeForTenant(string, []string, map[string]Value, string, string) (*Node, bool, error)
	UpsertNodeWithUniquenessRulesForTenant(string, []string, map[string]Value) (*Node, bool, error)
} = Storage(nil)
