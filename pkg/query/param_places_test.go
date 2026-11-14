package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

// One parsed query that runs two times through ExecuteWithParams must use the
// value of the second map in the second run. This holds for a WHERE in a later
// segment and in a UNION branch, also when the first part of the query has no row.
func TestExecuteWithParams_SecondRunUsesSecondValue(t *testing.T) {
	cases := []struct {
		query string
		runs  []struct {
			v    int64
			rows int
		}
	}{
		{
			"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.k < $v RETURN p.pid AS r",
			[]struct {
				v    int64
				rows int
			}{{3, 2}, {5, 4}, {2, 1}},
		},
		{
			"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION ALL MATCH (q:T) WHERE q.k < $v RETURN q.pid AS r",
			[]struct {
				v    int64
				rows int
			}{{3, 3}, {5, 5}, {2, 2}},
		},
		{
			"MATCH (a:Nope) WITH a MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION ALL MATCH (q:T) WHERE q.k < $v RETURN q.pid AS r",
			[]struct {
				v    int64
				rows int
			}{{3, 2}, {5, 4}, {2, 1}},
		},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			gs := seedIndexedWriteStore(t, false)
			ex := NewExecutor(gs)
			parsed := parseCallInput(t, c.query)
			for i, run := range c.runs {
				res, err := ex.ExecuteWithParams(parsed, map[string]any{"v": run.v})
				if err != nil {
					t.Fatalf("run %d: %v", i+1, err)
				}
				if len(res.Rows) != run.rows {
					t.Errorf("run %d with v=%d: want %d rows, got %d (%v)", i+1, run.v, run.rows, len(res.Rows), res.Rows)
				}
			}
		})
	}
}

// A parameter that the caller supplies must give the result of the literal form
// in each place where an expression can hold it.
func TestParameterInEachExpressionPlace_EqualsLiteral(t *testing.T) {
	places := []string{
		"MATCH (p:T) WHERE p.pid = 1 AND NOT ($two IS NULL) SET p.x = 1",
		"MATCH (p:T) WHERE p.pid = 1 AND NOT ($two IS NOT NULL) DELETE p",
		"MATCH (p:T) WHERE p.pid = 1 AND $two + 1 > 2 SET p.x = 1",
		"MATCH (p:T) WHERE p.pid = 1 AND p.k = $two * 0 + 1 SET p.x = 1",
		"MATCH (p:T) WHERE p.pid = 1 WITH p, $two AS w WHERE w = 2 SET p.x = w",
		"MATCH (p:T) WHERE p.k < 4 RETURN p.pid AS r ORDER BY p.pid",
		"MATCH (p:T) WHERE p.pid = 1 SET p.k = $two + 1",
		"MATCH (p:T) WHERE p.pid = 1 SET p.k = $two",
		"MERGE (m:M {id: 1}) ON CREATE SET m.v = $two",
		"MERGE (m:T {pid: 1}) ON MATCH SET m.v = $two + 1",
	}
	var cases []string
	for _, place := range places {
		cases = append(cases, place, "MATCH (a:T) WHERE a.k = 1 WITH a "+place)
	}
	cases = append(cases,
		"MATCH (a:T) WHERE a.k = 1 WITH a MATCH (p:T) WHERE p.pid = 1 AND NOT ($two IS NULL) SET p.x = 1",
		"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 2 AND NOT ($two IS NULL) RETURN q.pid AS r",
		"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = $two + 0 RETURN q.pid AS r",
		"MATCH (p:T) WHERE p.pid = 1 RETURN p.pid AS r UNION MATCH (q:T) WHERE q.pid = 2 WITH q, $two AS w WHERE w = 2 RETURN q.pid AS r",
	)
	params := map[string]any{"two": int64(2)}
	for _, q := range cases {
		literal := strings.ReplaceAll(q, "$two", "2")
		for _, withIndex := range []bool{false, true} {
			name := q
			if withIndex {
				name += " [index]"
			}
			t.Run(name, func(t *testing.T) {
				if tokens, lerr := NewLexer(q).Tokenize(); lerr != nil {
					t.Skipf("does not lex: %v", lerr)
				} else if _, perr := NewParser(tokens).Parse(); perr != nil {
					t.Skipf("does not parse: %v", perr)
				}
				want := runIndexedWrite(t, withIndex, indexedCase{query: literal})
				got := runIndexedWrite(t, withIndex, indexedCase{query: q, params: params})
				if want.err != "" {
					t.Skipf("the literal form gives an error, so the case proves nothing: %s", want.err)
				}
				want.usedIdx, got.usedIdx = false, false
				if !reflect.DeepEqual(want, got) {
					t.Errorf("parameter and literal differ\nliteral:   %+v\nparameter: %+v", want, got)
				}
			})
		}
	}
}

// expressionTypes lists each type in this package that implements Expression.
// validateExprParams must visit each child expression of each type.
// TestValidateExprParams_CoversEachExpressionType keeps the list complete.
var expressionTypes = []Expression{
	&BinaryExpression{},
	&ArithmeticExpression{},
	&UnaryExpression{},
	&PropertyExpression{},
	&LiteralExpression{},
	&FunctionCallExpression{},
	&ParameterExpression{},
	&CaseExpression{},
}

// An Expression type that someone adds later must reach this test. The test
// reads the package source for each type that has an EvalValue method, and it
// fails when the type is not in expressionTypes. Then, for each type in the
// list, it puts a missing parameter in each field that holds an Expression, and
// it checks that validateExprParams refuses it.
func TestValidateExprParams_CoversEachExpressionType(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package source: %v", err)
	}
	pkg, ok := pkgs["query"]
	if !ok {
		t.Fatalf("package query not found in the source scan")
	}
	found := map[string]bool{}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "EvalValue" {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if id, ok := star.X.(*ast.Ident); ok {
				found[id.Name] = true
			}
		}
	}
	// Positive control: the scan must see a type that we know is there.
	if !found["BinaryExpression"] {
		t.Fatalf("the source scan did not find BinaryExpression, so the scan is broken: %v", found)
	}
	known := map[string]bool{}
	for _, e := range expressionTypes {
		known[reflect.TypeOf(e).Elem().Name()] = true
	}
	for name := range found {
		if !known[name] {
			t.Errorf("%s implements Expression but is not in expressionTypes: add it, and make validateExprParams visit its child expressions", name)
		}
	}

	exprType := reflect.TypeOf((*Expression)(nil)).Elem()
	missing := func() Expression { return &ParameterExpression{Name: "gone"} }

	for _, e := range expressionTypes {
		typ := reflect.TypeOf(e).Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			// Each variant sets one Expression slot of a fresh value.
			var variants []func(reflect.Value)
			switch {
			case field.Type == exprType:
				variants = append(variants, func(f reflect.Value) { f.Set(reflect.ValueOf(missing())) })
			case field.Type == reflect.TypeOf([]Expression(nil)):
				variants = append(variants, func(f reflect.Value) {
					f.Set(reflect.ValueOf([]Expression{missing()}))
				})
			case field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Struct:
				elem := field.Type.Elem()
				for j := 0; j < elem.NumField(); j++ {
					if elem.Field(j).Type != exprType {
						continue
					}
					j := j
					variants = append(variants, func(f reflect.Value) {
						item := reflect.New(elem).Elem()
						item.Field(j).Set(reflect.ValueOf(missing()))
						f.Set(reflect.Append(reflect.MakeSlice(field.Type, 0, 1), item))
					})
				}
			}
			for _, set := range variants {
				fresh := reflect.New(typ)
				set(fresh.Elem().Field(i))
				err := validateExprParams(fresh.Interface().(Expression), map[string]any{})
				if err == nil || !strings.Contains(err.Error(), "$gone") {
					t.Errorf("validateExprParams does not visit %s.%s: got error %v", typ.Name(), field.Name, err)
				}
			}
		}
	}
}
