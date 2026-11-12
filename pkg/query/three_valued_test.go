package query

import "testing"

func TestTri_KleeneTables(t *testing.T) {
	vals := []tri{triFalse, triTrue, triUnknown}
	name := func(v tri) string { return [...]string{"false", "true", "unknown"}[v] }

	// Rows and columns follow vals: false, true, unknown.
	and := [3][3]tri{
		{triFalse, triFalse, triFalse},
		{triFalse, triTrue, triUnknown},
		{triFalse, triUnknown, triUnknown},
	}
	or := [3][3]tri{
		{triFalse, triTrue, triUnknown},
		{triTrue, triTrue, triTrue},
		{triUnknown, triTrue, triUnknown},
	}
	for i, l := range vals {
		for j, r := range vals {
			if got := triAnd(l, r); got != and[i][j] {
				t.Errorf("%s AND %s = %s, want %s", name(l), name(r), name(got), name(and[i][j]))
			}
			if got := triOr(l, r); got != or[i][j] {
				t.Errorf("%s OR %s = %s, want %s", name(l), name(r), name(got), name(or[i][j]))
			}
		}
	}
	for l, want := range map[tri]tri{triFalse: triTrue, triTrue: triFalse, triUnknown: triUnknown} {
		if got := triNot(l); got != want {
			t.Errorf("NOT %s = %s, want %s", name(l), name(got), name(want))
		}
	}
}
