package storage

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

const upsertGoroutines = 32

func TestUpsertNode_Concurrent32OneKeyMakesOneNode(t *testing.T) {
	gs := newUpsertStore(t)

	var createdCount atomic.Int32
	ids := make([]uint64, upsertGoroutines)
	errs := make([]error, upsertGoroutines)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := 0; i < upsertGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			n, created, err := gs.UpsertNodeForTenant(upTenant, []string{"Host"},
				map[string]Value{"ip": StringValue("10.0.0.1"), fmt.Sprintf("w%d", i): IntValue(int64(i))}, "Host", "ip")
			errs[i] = err
			if err != nil {
				return
			}
			ids[i] = n.ID
			if created {
				createdCount.Add(1)
			}
		}()
	}
	start.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if got := createdCount.Load(); got != 1 {
		t.Errorf("%d calls returned created = true, want exactly 1", got)
	}
	if n := countNodes(t, gs, upTenant); n != 1 {
		t.Fatalf("node count = %d, want 1", n)
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Errorf("goroutine %d got node %d, goroutine 0 got %d", i, id, ids[0])
		}
	}
	// Every writer's own property survived the merges.
	stored, err := gs.GetNodeForTenant(ids[0], upTenant)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < upsertGoroutines; i++ {
		if _, ok := stored.Properties[fmt.Sprintf("w%d", i)]; !ok {
			t.Errorf("property w%d was lost in a merge", i)
		}
	}
}

func TestUpsertNode_ConcurrentWithPlainCreateOfRuleKey(t *testing.T) {
	gs := newUpsertStore(t)
	registerRule(t, gs, "host_ip", "Host", "ip")

	var createdByUpsert atomic.Int32
	var createdByCreate atomic.Int32
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := 0; i < upsertGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			props := map[string]Value{"ip": StringValue("10.0.0.1")}
			if i%2 == 0 {
				_, created, err := gs.UpsertNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, props)
				if err != nil {
					t.Errorf("upsert %d: %v", i, err)
					return
				}
				if created {
					createdByUpsert.Add(1)
				}
				return
			}
			_, err := gs.CreateNodeWithUniquenessRulesForTenant(upTenant, []string{"Host"}, props)
			switch {
			case err == nil:
				createdByCreate.Add(1)
			case errors.Is(err, ErrUniqueConstraintViolation):
			default:
				t.Errorf("create %d: %v", i, err)
			}
		}()
	}
	start.Done()
	wg.Wait()

	if n := countNodes(t, gs, upTenant); n != 1 {
		t.Fatalf("node count = %d, want 1 (AG3)", n)
	}
	if got := createdByUpsert.Load() + createdByCreate.Load(); got != 1 {
		t.Errorf("%d calls created the node, want exactly 1", got)
	}
}
