package storage

import (
	"fmt"
	"os"
	"time"
)

// loadProfiler times the phases of loadFromDisk when GRAPHDB_LOAD_PROFILE is
// set to any non-empty value. It exists because reopen cost is dominated by a
// handful of distinct O(N) phases (blob decode vs. derived-index rebuild), and
// knowing the split is what decides whether a faster on-disk format is
// sufficient or whether the derived indexes must themselves be persisted.
//
// Disabled (the default), the only cost is a single os.Getenv at construction
// and a branch per mark — no time.Now calls — so it is safe to leave wired into
// the hot path for ad-hoc production diagnosis of a slow restart.
type loadProfiler struct {
	enabled bool
	start   time.Time
	last    time.Time
	rows    []loadPhase
}

type loadPhase struct {
	name string
	d    time.Duration
}

func newLoadProfiler() *loadProfiler {
	if os.Getenv("GRAPHDB_LOAD_PROFILE") == "" {
		return &loadProfiler{}
	}
	now := time.Now()
	return &loadProfiler{enabled: true, start: now, last: now}
}

// mark records the time elapsed since the previous mark (or construction) under
// name. No-op when profiling is disabled.
func (p *loadProfiler) mark(name string) {
	if !p.enabled {
		return
	}
	now := time.Now()
	p.rows = append(p.rows, loadPhase{name: name, d: now.Sub(p.last)})
	p.last = now
}

// report prints the phase breakdown to stderr. No-op when profiling is disabled.
func (p *loadProfiler) report() {
	if !p.enabled {
		return
	}
	total := time.Since(p.start)
	fmt.Fprintf(os.Stderr, "\n[GRAPHDB_LOAD_PROFILE] loadFromDisk phase breakdown:\n")
	for _, r := range p.rows {
		pct := 0.0
		if total > 0 {
			pct = 100 * float64(r.d) / float64(total)
		}
		fmt.Fprintf(os.Stderr, "  %-34s %10s  %5.1f%%\n", r.name, r.d.Round(time.Millisecond), pct)
	}
	fmt.Fprintf(os.Stderr, "  %-34s %10s\n", "TOTAL loadFromDisk", total.Round(time.Millisecond))
}
