package queryutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A reader for the Gherkin subset that the openCypher TCK files use. It reads
// the file into scenarios and expands each Scenario Outline example row into
// one scenario. It does not give a meaning to the steps: tck_run_test.go does
// that. A line that this reader does not know is an error, never a skip, so a
// construct that the files add later cannot vanish without a sound.

// tckStep is one Given, When, Then, And or But step with its attachments.
type tckStep struct {
	text   string     // the step text after the keyword
	doc    string     // the docstring, when hasDoc is set
	hasDoc bool       //
	table  [][]string // the data table, raw cells, header row included
}

// tckScenario is one scenario after the outline expansion.
type tckScenario struct {
	feature string // the feature file name without the extension
	name    string // unique inside the feature file
	steps   []tckStep
}

type rawScenario struct {
	name     string
	outline  bool
	steps    []tckStep
	examples [][][]string // one table for each Examples block
}

var tckKeywords = []string{"Given ", "When ", "Then ", "And ", "But ", "* "}

// splitGherkinRow splits one table row on the pipes that are not escaped and
// unescapes \| , \\ and \n in each cell, as Cucumber does.
func splitGherkinRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			switch line[i+1] {
			case '|':
				cur.WriteByte('|')
				i++
				continue
			case '\\':
				cur.WriteByte('\\')
				i++
				continue
			case 'n':
				cur.WriteByte('\n')
				i++
				continue
			}
		}
		if c == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	// A row ends with its last pipe. Text after it is not a cell.
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		cells = append(cells, rest)
	}
	return cells
}

// parseFeatureText reads one feature file's text.
func parseFeatureText(feature, text string) ([]*tckScenario, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	var raws []*rawScenario
	var cur *rawScenario
	inExamples := false
	inDoc := false
	docIndent := 0
	var docLines []string

	for n, ln := range lines {
		where := func(msg string) error { return fmt.Errorf("%s.feature:%d: %s: %q", feature, n+1, msg, ln) }
		tr := strings.TrimSpace(ln)

		if inDoc {
			if tr == `"""` {
				last := &cur.steps[len(cur.steps)-1]
				last.doc, last.hasDoc = strings.Join(docLines, "\n"), true
				inDoc, docLines = false, nil
				continue
			}
			docLines = append(docLines, trimIndent(ln, docIndent))
			continue
		}
		switch {
		case tr == "" || strings.HasPrefix(tr, "#") || strings.HasPrefix(tr, "@"):
			continue
		case strings.HasPrefix(tr, "Feature:"):
			continue
		case strings.HasPrefix(tr, "Scenario Outline:") || strings.HasPrefix(tr, "Scenario Template:"):
			cur = &rawScenario{name: strings.TrimSpace(tr[strings.Index(tr, ":")+1:]), outline: true}
			raws, inExamples = append(raws, cur), false
		case strings.HasPrefix(tr, "Scenario:"):
			cur = &rawScenario{name: strings.TrimSpace(strings.TrimPrefix(tr, "Scenario:"))}
			raws, inExamples = append(raws, cur), false
		case cur == nil:
			continue // the feature description
		case strings.HasPrefix(tr, "Examples:") || strings.HasPrefix(tr, "Scenarios:"):
			if !cur.outline {
				return nil, where("Examples in a scenario that is not an outline")
			}
			inExamples = true
			cur.examples = append(cur.examples, nil)
		case strings.HasPrefix(tr, `"""`):
			if tr != `"""` || inExamples || len(cur.steps) == 0 {
				return nil, where("unsupported docstring")
			}
			inDoc, docIndent = true, len(ln)-len(strings.TrimLeft(ln, " \t"))
		case strings.HasPrefix(tr, "|"):
			row := splitGherkinRow(tr)
			if inExamples {
				cur.examples[len(cur.examples)-1] = append(cur.examples[len(cur.examples)-1], row)
				continue
			}
			if len(cur.steps) == 0 {
				return nil, where("table before any step")
			}
			last := &cur.steps[len(cur.steps)-1]
			last.table = append(last.table, row)
		default:
			step, ok := tckParseStepLine(tr)
			if !ok || inExamples {
				return nil, where("line not understood")
			}
			cur.steps = append(cur.steps, step)
		}
	}
	if inDoc {
		return nil, fmt.Errorf("%s.feature: docstring not closed", feature)
	}
	return expandScenarios(feature, raws)
}

func tckParseStepLine(tr string) (tckStep, bool) {
	for _, kw := range tckKeywords {
		if strings.HasPrefix(tr, kw) {
			return tckStep{text: strings.TrimSpace(tr[len(kw):])}, true
		}
	}
	return tckStep{}, false
}

func trimIndent(s string, n int) string {
	i := 0
	for i < n && i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[i:]
}

// expandScenarios turns outlines into plain scenarios and makes each name
// unique inside the file.
func expandScenarios(feature string, raws []*rawScenario) ([]*tckScenario, error) {
	var out []*tckScenario
	for _, r := range raws {
		if !r.outline {
			out = append(out, &tckScenario{feature: feature, name: r.name, steps: r.steps})
			continue
		}
		idx := 0
		for _, ex := range r.examples {
			if len(ex) < 1 {
				return nil, fmt.Errorf("%s.feature: outline %q has an empty Examples table", feature, r.name)
			}
			header := ex[0]
			for _, row := range ex[1:] {
				idx++
				if len(row) != len(header) {
					return nil, fmt.Errorf("%s.feature: outline %q example %d has %d cells for %d columns", feature, r.name, idx, len(row), len(header))
				}
				sub := func(s string) string {
					for i, h := range header {
						s = strings.ReplaceAll(s, "<"+h+">", row[i])
					}
					return s
				}
				sc := &tckScenario{feature: feature, name: fmt.Sprintf("%s {ex %d}", sub(r.name), idx)}
				for _, st := range r.steps {
					ns := tckStep{text: sub(st.text), doc: sub(st.doc), hasDoc: st.hasDoc}
					for _, tr := range st.table {
						nr := make([]string, len(tr))
						for i, c := range tr {
							nr[i] = sub(c)
						}
						ns.table = append(ns.table, nr)
					}
					sc.steps = append(sc.steps, ns)
				}
				out = append(out, sc)
			}
		}
	}
	seen := map[string]int{}
	for _, sc := range out {
		seen[sc.name]++
		if k := seen[sc.name]; k > 1 {
			sc.name = fmt.Sprintf("%s (dup %d)", sc.name, k)
		}
	}
	return out, nil
}

// loadTCKDir reads every .feature file in dir, in name order.
func loadTCKDir(dir string) ([]*tckScenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.feature"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .feature file in %s", dir)
	}
	var all []*tckScenario
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(p), ".feature")
		scs, err := parseFeatureText(name, string(data))
		if err != nil {
			return nil, err
		}
		all = append(all, scs...)
	}
	return all, nil
}
