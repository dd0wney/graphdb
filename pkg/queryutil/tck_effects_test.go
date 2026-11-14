package queryutil

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Side effect measurement. The harness reads every node and relationship of
// the store before and after the query under test, and compares the two reads.

type tckEntity struct {
	labels []string          // nodes only, sorted
	props  map[string]string // property name to type and bytes
	from   uint64            // relationships only
	to     uint64
	typ    string
}

// tckState holds the stored graph. A key is "n<id>" or "r<id>".
type tckState map[string]tckEntity

func tckSnapshot(g *storage.GraphStorage) (tckState, error) {
	st := tckState{}
	for _, n := range g.GetAllNodesAcrossTenants() {
		labels := slices.Clone(n.Labels)
		slices.Sort(labels)
		st[fmt.Sprintf("n%d", n.ID)] = tckEntity{labels: labels, props: tckProps(n.Properties)}
	}
	edges, err := g.GetAllEdgesForTenant(storage.DefaultTenantID)
	if err != nil {
		return nil, fmt.Errorf("read relationships: %w", err)
	}
	for _, e := range edges {
		st[fmt.Sprintf("r%d", e.ID)] = tckEntity{props: tckProps(e.Properties), from: e.FromNodeID, to: e.ToNodeID, typ: e.Type}
	}
	return st, nil
}

func tckProps(p map[string]storage.Value) map[string]string {
	out := make(map[string]string, len(p))
	for k, v := range p {
		out[k] = fmt.Sprintf("%d:%x", v.Type, v.Data)
	}
	return out
}

// tckFingerprint is a string that is equal for two equal states.
func tckFingerprint(st tckState) string {
	keys := slices.Sorted(maps.Keys(st))
	var b strings.Builder
	for _, k := range keys {
		e := st[k]
		fmt.Fprintf(&b, "%s|%v|%d>%d|%s|", k, e.labels, e.from, e.to, e.typ)
		for _, pk := range slices.Sorted(maps.Keys(e.props)) {
			fmt.Fprintf(&b, "%s=%s,", pk, e.props[pk])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// tckEffects counts the changes between two states. A property whose value
// changes counts as one added and one removed, as the TCK counts it. A label
// is counted for each node that carries it.
func tckEffects(before, after tckState) map[string]int {
	c := map[string]int{}
	for k, a := range after {
		b, existed := before[k]
		isNode := strings.HasPrefix(k, "n")
		if !existed {
			if isNode {
				c["+nodes"]++
				c["+labels"] += len(a.labels)
			} else {
				c["+relationships"]++
			}
			c["+properties"] += len(a.props)
			continue
		}
		for _, l := range a.labels {
			if !slices.Contains(b.labels, l) {
				c["+labels"]++
			}
		}
		for _, l := range b.labels {
			if !slices.Contains(a.labels, l) {
				c["-labels"]++
			}
		}
		for pk, av := range a.props {
			if bv, had := b.props[pk]; !had || bv != av {
				c["+properties"]++
			}
		}
		for pk, bv := range b.props {
			if av, has := a.props[pk]; !has || bv != av {
				c["-properties"]++
			}
		}
	}
	for k, b := range before {
		if _, kept := after[k]; kept {
			continue
		}
		if strings.HasPrefix(k, "n") {
			c["-nodes"]++
			c["-labels"] += len(b.labels)
		} else {
			c["-relationships"]++
		}
		c["-properties"] += len(b.props)
	}
	return c
}

var tckCounters = []string{"+nodes", "-nodes", "+relationships", "-relationships", "+properties", "-properties", "+labels", "-labels"}

func tckCheckSideEffects(g *storage.GraphStorage, sp *tckSpec, before tckState, beforeErr error) tckResult {
	if beforeErr != nil {
		return tckResult{outUnsupported, "cannot measure side effects: " + beforeErr.Error()}
	}
	after, err := tckSnapshot(g)
	if err != nil {
		return tckResult{outUnsupported, "cannot measure side effects: " + err.Error()}
	}
	got := tckEffects(before, after)
	if sp.sideTable == nil {
		// no side effects: the counts and the full fingerprint must hold.
		if tckFingerprint(before) != tckFingerprint(after) || len(got) != 0 {
			return tckResult{outFail, fmt.Sprintf("expected no side effects, got %v", got)}
		}
		return tckResult{outPass, ""}
	}
	for name := range sp.sideTable {
		if !slices.Contains(tckCounters, name) {
			return tckResult{outUnsupported, "side effect counter that the harness cannot measure: " + name}
		}
	}
	for _, name := range tckCounters {
		if got[name] != sp.sideTable[name] {
			return tckResult{outFail, fmt.Sprintf("side effects %v, want %v", got, sp.sideTable)}
		}
	}
	return tckResult{outPass, ""}
}
