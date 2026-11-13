package query

import "testing"

// PROFILE ran the first segment only: PROFILE MATCH (a) WITH a RETURN a.name
// answered affected=N where the query without PROFILE answers the names, and
// PROFILE of a UNION returned no profile.
func TestProfile_ReturnsTheRowsOfTheQueryWithoutProfile(t *testing.T) {
	shapes := []struct{ name, text string }{
		{"one segment", `MATCH (p:Person) RETURN p.name AS n`},
		{"two segments", `MATCH (a:Person) WITH a RETURN a.name AS n`},
		{"three segments", `MATCH (a:Person) WITH a MATCH (b:Person {id: 2}) WITH a, b RETURN a.name AS n, b.name AS m`},
		{"UNION", `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION MATCH (q:Person {id: 7}) RETURN q.name AS n`},
		{"UNION with WITH", `MATCH (a:Person {id: 1}) WITH a RETURN a.name AS n UNION MATCH (b:Person {id: 7}) RETURN b.name AS n`},
		{"write after WITH", `MATCH (a:Person {id: 1}) WITH a CREATE (:Made {k: 1}) RETURN a.name AS n`},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			plainEx := patternParamsStore(t)
			plain, err := plainEx.Execute(parsePatternQuery(t, sh.text))
			if err != nil {
				t.Fatalf("without PROFILE: %v", err)
			}
			if plain.Count == 0 {
				t.Fatalf("no rows without PROFILE")
			}

			profEx := patternParamsStore(t)
			prof, err := profEx.Execute(parsePatternQuery(t, "PROFILE "+sh.text))
			if err != nil {
				t.Fatalf("with PROFILE: %v", err)
			}
			if got, want := rowSignature(prof), rowSignature(plain); got != want {
				t.Errorf("rows with PROFILE: %s\n  rows without:      %s", got, want)
			}
			if len(prof.Profile) == 0 {
				t.Errorf("PROFILE returned no profile")
			}
			for i, sp := range prof.Profile {
				if sp.StepName == "" {
					t.Errorf("profile step %d has no name", i)
				}
			}

			// A write must happen once, as it does without PROFILE.
			a, _ := plainEx.graph.GetNodesByLabelForTenant("default", "Made")
			b, _ := profEx.graph.GetNodesByLabelForTenant("default", "Made")
			if len(a) != len(b) {
				t.Errorf("Made nodes: %d without PROFILE, %d with", len(a), len(b))
			}
		})
	}
}

// A profile covers every segment: a chain of two segments has more steps than
// its first segment.
func TestProfile_CoversEverySegment(t *testing.T) {
	ex := patternParamsStore(t)
	one, err := ex.Execute(parsePatternQuery(t, `PROFILE MATCH (a:Person) RETURN a.name AS n`))
	if err != nil {
		t.Fatal(err)
	}
	two, err := ex.Execute(parsePatternQuery(t, `PROFILE MATCH (a:Person) WITH a MATCH (b:Person) RETURN b.name AS n`))
	if err != nil {
		t.Fatal(err)
	}
	if len(two.Profile) <= len(one.Profile) {
		t.Errorf("profile steps: %d for two segments, %d for one", len(two.Profile), len(one.Profile))
	}
}

// PROFILE reads the parameters in every segment.
func TestProfile_ReadsParametersInEverySegment(t *testing.T) {
	ex := patternParamsStore(t)
	res, err := ex.ExecuteWithParams(parsePatternQuery(t,
		`PROFILE MATCH (a:Person {id: 1}) WITH a MATCH (b:Person) WHERE b.id = $v RETURN b.name AS n`),
		map[string]any{"v": int64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowSignature(res); got != "map[n:Bob]" {
		t.Errorf("rows = %s, want Bob", got)
	}
}
