package storage

import (
	"fmt"
	"testing"
)

// legacySliceRemove reproduces the pre-Path-C label-index removal: a linear
// scan for the id followed by swap-with-last truncation. It is O(K) in the
// bucket size K. Kept here (test-only) purely as the benchmark baseline the
// set form is measured against.
func legacySliceRemove(ids []uint64, target uint64) []uint64 {
	for i, id := range ids {
		if id == target {
			ids[i] = ids[len(ids)-1]
			return ids[:len(ids)-1]
		}
	}
	return ids
}

// BenchmarkLabelIndexRemoval isolates the exact operation Path C changed —
// removing one id from a label bucket of size K — on a shared per-size fixture,
// comparing the new set form against the legacy slice form. It demonstrates the
// M3 asymptote: the set is O(1) and stays flat as K grows; the legacy slice is
// O(K) and scales with the bucket. DeleteNode hits this once per label on the
// global index and once on the per-tenant index, so a bulk delete of N nodes
// sharing a label was O(N^2) on the label-index component and is now O(N).
//
// Each iteration removes then restores the same id, so the bucket stays size K
// and the measured cost is a single remove (+ a cheap re-add) rather than a
// drain. For LegacySlice, swap-with-last + re-append relocates the id to the
// tail after the first iteration, so iterations 2..N scan the full bucket —
// the LegacySlice numbers therefore reflect a full O(K) linear scan (the cost
// of locating an arbitrary id in an unsorted bucket), not a midpoint scan. The
// set form is a hash lookup, unaffected by position. The O(1)-vs-O(K)
// asymptote is what this demonstrates; treat the absolute multiples as
// full-scan, not average-case.
func BenchmarkLabelIndexRemoval(b *testing.B) {
	sizes := []int{64, 1024, 16384}

	for _, k := range sizes {
		mid := uint64(k / 2)

		b.Run(fmt.Sprintf("Set/K=%d", k), func(b *testing.B) {
			bucket := make(map[uint64]struct{}, k)
			for i := 0; i < k; i++ {
				bucket[uint64(i)] = struct{}{}
			}
			idx := labelIndex{"L": bucket}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				removeFromLabelIndexKeepEmpty(idx, "L", mid)
				idx["L"][mid] = struct{}{} // restore to hold K constant
			}
		})

		b.Run(fmt.Sprintf("LegacySlice/K=%d", k), func(b *testing.B) {
			ids := make([]uint64, k)
			for i := 0; i < k; i++ {
				ids[i] = uint64(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ids = legacySliceRemove(ids, mid)
				ids = append(ids, mid) // restore to hold K constant
			}
		})
	}
}
