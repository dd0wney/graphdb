package query

import (
	"fmt"
	"sync"
	"testing"
)

// One parsed query must be safe to run from several goroutines. The executor
// used to write the bindings of the next segment into the parsed Query.
func TestSegmentRuns_ParsedQueryIsSafeFromManyGoroutines(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       string
	}{
		{"WITH chain", `MATCH (a:Person) WITH a MATCH (b:Person {id: 2}) RETURN b.name AS n`, "map[n:Bob]|map[n:Bob]|map[n:Bob]"},
		{"two WITH", `MATCH (a:Person) WITH a MATCH (b:Person {id: 2}) WITH a, b MATCH (c:Person {id: 7}) RETURN c.name AS n`, "map[n:Gus]|map[n:Gus]|map[n:Gus]"},
		{"UNION", `MATCH (p:Person {id: 1}) RETURN p.name AS n UNION MATCH (q:Person {id: 7}) RETURN q.name AS n`, "map[n:Ann]|map[n:Gus]"},
		{"UNION of WITH chains", `MATCH (a:Person {id: 1}) WITH a MATCH (b:Person {id: 2}) RETURN b.name AS n UNION MATCH (a:Person {id: 7}) WITH a MATCH (b:Person {id: 7}) RETURN b.name AS n`, "map[n:Bob]|map[n:Gus]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := patternParamsStore(t)
			q := parsePatternQuery(t, tc.text)
			var wg sync.WaitGroup
			errs := make(chan string, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 20; j++ {
						res, err := ex.Execute(q)
						if err != nil {
							errs <- err.Error()
							return
						}
						if got := rowSignature(res); got != tc.want {
							errs <- fmt.Sprintf("rows = %s, want %s", got, tc.want)
							return
						}
					}
				}()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				t.Error(e)
			}
		})
	}
}
