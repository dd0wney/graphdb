package queryutil

import (
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// The value model of the TCK harness. A TCK cell and a Go result value both
// become a tckVal. The comparison has three answers, not two: equal, not
// equal, and unknown. "Unknown" is how the harness says that it did not fully
// compare, for example for a node literal. The runner never turns unknown into
// a pass.

type tckKind int

const (
	kNull tckKind = iota
	kInt
	kFloat
	kString
	kBool
	kList
	kMap
	kNode   // a node value from the executor
	kEdge   // a relationship value from the executor
	kOpaque // text or a Go value that the model cannot represent
)

type tckVal struct {
	kind tckKind
	i    int64
	f    float64
	s    string
	b    bool
	list []tckVal
	m    map[string]tckVal
}

type tckTri int

const (
	triEq tckTri = iota
	triNe
	triUnknown
)

// combineTri merges the answers for the parts of one value: not equal wins,
// then unknown, then equal.
func combineTri(a, b tckTri) tckTri {
	if a == triNe || b == triNe {
		return triNe
	}
	if a == triUnknown || b == triUnknown {
		return triUnknown
	}
	return triEq
}

// tckParseValue reads the text of a TCK value: integer, float, string,
// boolean, null, list and map. A node, relationship or path literal, and any
// text that it does not read to the end, give an opaque value.
func tckParseValue(text string) tckVal {
	p := &valParser{s: strings.TrimSpace(text)}
	v, ok := p.value()
	p.skip()
	if !ok || p.pos != len(p.s) {
		return tckVal{kind: kOpaque, s: text}
	}
	return v
}

type valParser struct {
	s   string
	pos int
}

func (p *valParser) skip() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *valParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

var (
	tckIntRe   = regexp.MustCompile(`^-?[0-9]+$`)
	tckFloatRe = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

func (p *valParser) value() (tckVal, bool) {
	p.skip()
	switch c := p.peek(); {
	case c == '\'':
		s, ok := p.str()
		return tckVal{kind: kString, s: s}, ok
	case c == '[':
		return p.list()
	case c == '{':
		return p.mapValue()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.s) && isIdentByte(p.s[p.pos]) {
			p.pos++
		}
		switch p.s[start:p.pos] {
		case "null":
			return tckVal{kind: kNull}, true
		case "true":
			return tckVal{kind: kBool, b: true}, true
		case "false":
			return tckVal{kind: kBool}, true
		case "NaN":
			return tckVal{kind: kFloat, f: math.NaN()}, true
		case "Infinity":
			return tckVal{kind: kFloat, f: math.Inf(1)}, true
		}
	}
	return tckVal{}, false // includes ( and < : node, relationship and path literals
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func (p *valParser) number() (tckVal, bool) {
	if strings.HasPrefix(p.s[p.pos:], "-Infinity") {
		p.pos += len("-Infinity")
		return tckVal{kind: kFloat, f: math.Inf(-1)}, true
	}
	start := p.pos
	for p.pos < len(p.s) && strings.IndexByte("0123456789.eE+-", p.s[p.pos]) >= 0 {
		p.pos++
	}
	tok := p.s[start:p.pos]
	switch {
	case tckIntRe.MatchString(tok):
		n, err := strconv.ParseInt(tok, 10, 64)
		return tckVal{kind: kInt, i: n}, err == nil
	case tckFloatRe.MatchString(tok):
		f, err := strconv.ParseFloat(tok, 64)
		return tckVal{kind: kFloat, f: f}, err == nil
	}
	return tckVal{}, false
}

func (p *valParser) str() (string, bool) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		switch c {
		case '\'':
			return b.String(), true
		case '\\':
			if p.pos >= len(p.s) {
				return "", false
			}
			e := p.s[p.pos]
			p.pos++
			switch e {
			case '\\', '\'', '"':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", false // an escape that the harness does not read
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", false
}

func (p *valParser) list() (tckVal, bool) {
	p.pos++
	out := tckVal{kind: kList}
	p.skip()
	if p.peek() == ']' {
		p.pos++
		return out, true
	}
	for {
		v, ok := p.value()
		if !ok {
			return tckVal{}, false
		}
		out.list = append(out.list, v)
		p.skip()
		switch p.peek() {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return out, true
		default:
			return tckVal{}, false
		}
	}
}

func (p *valParser) mapValue() (tckVal, bool) {
	p.pos++
	out := tckVal{kind: kMap, m: map[string]tckVal{}}
	p.skip()
	if p.peek() == '}' {
		p.pos++
		return out, true
	}
	for {
		p.skip()
		start := p.pos
		for p.pos < len(p.s) && isIdentByte(p.s[p.pos]) {
			p.pos++
		}
		key := p.s[start:p.pos]
		if key == "" {
			return tckVal{}, false
		}
		p.skip()
		if p.peek() != ':' {
			return tckVal{}, false
		}
		p.pos++
		v, ok := p.value()
		if !ok {
			return tckVal{}, false
		}
		if _, dup := out.m[key]; dup {
			return tckVal{}, false
		}
		out.m[key] = v
		p.skip()
		switch p.peek() {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return out, true
		default:
			return tckVal{}, false
		}
	}
}

// tckGoValue maps a Go value from a result to a tckVal. A Go integer type
// gives kInt and a Go float type gives kFloat. The harness never merges the two.
func tckGoValue(v any) tckVal {
	switch x := v.(type) {
	case nil:
		return tckVal{kind: kNull}
	case *storage.Node:
		return tckVal{kind: kNode}
	case *storage.Edge:
		return tckVal{kind: kEdge}
	case string:
		return tckVal{kind: kString, s: x}
	case bool:
		return tckVal{kind: kBool, b: x}
	case []byte:
		return tckVal{kind: kOpaque}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return tckVal{kind: kInt, i: rv.Int()}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rv.Uint() > math.MaxInt64 {
			return tckVal{kind: kOpaque}
		}
		return tckVal{kind: kInt, i: int64(rv.Uint())}
	case reflect.Float32, reflect.Float64:
		return tckVal{kind: kFloat, f: rv.Float()}
	case reflect.Slice, reflect.Array:
		out := tckVal{kind: kList}
		for i := 0; i < rv.Len(); i++ {
			out.list = append(out.list, tckGoValue(rv.Index(i).Interface()))
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return tckVal{kind: kOpaque}
		}
		out := tckVal{kind: kMap, m: map[string]tckVal{}}
		for _, k := range rv.MapKeys() {
			out.m[k.String()] = tckGoValue(rv.MapIndex(k).Interface())
		}
		return out
	}
	return tckVal{kind: kOpaque}
}

// tckCompare compares an expected value with an actual value, strictly.
// An integer never equals a float. A list compares by order. A map compares by
// key. NaN equals NaN, because the TCK writes NaN as a result value.
func tckCompare(want, got tckVal) tckTri {
	if want.kind == kOpaque || got.kind == kOpaque {
		return triUnknown
	}
	if got.kind == kNode || got.kind == kEdge {
		if want.kind == got.kind {
			return triUnknown // the harness reads no node or relationship content
		}
		return triNe
	}
	if want.kind != got.kind {
		return triNe
	}
	switch want.kind {
	case kNull:
		return triEq
	case kInt:
		return boolTri(want.i == got.i)
	case kFloat:
		if math.IsNaN(want.f) || math.IsNaN(got.f) {
			return boolTri(math.IsNaN(want.f) && math.IsNaN(got.f))
		}
		return boolTri(want.f == got.f)
	case kString:
		return boolTri(want.s == got.s)
	case kBool:
		return boolTri(want.b == got.b)
	case kList:
		if len(want.list) != len(got.list) {
			return triNe
		}
		res := triEq
		for i := range want.list {
			res = combineTri(res, tckCompare(want.list[i], got.list[i]))
		}
		return res
	case kMap:
		if len(want.m) != len(got.m) {
			return triNe
		}
		res := triEq
		for k, wv := range want.m {
			gv, ok := got.m[k]
			if !ok {
				return triNe
			}
			res = combineTri(res, tckCompare(wv, gv))
		}
		return res
	}
	return triUnknown
}

func boolTri(b bool) tckTri {
	if b {
		return triEq
	}
	return triNe
}
