package query

import (
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// storedArrayStore holds one T node per stored array type. Node 1 has a
// float32 vector, node 2 a float array, node 3 an integer array, node 4 a
// string array and node 5 a boolean array.
func storedArrayStore(t *testing.T) *Executor {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "embedding": storage.VectorValue([]float32{0.1, 0.25, 1})},
		{"id": storage.IntValue(2), "arr": storage.FloatArrayValue([]float64{0.1, 0.25, 1})},
		{"id": storage.IntValue(3), "arr": storage.IntArrayValue([]int64{1, 2, 3})},
		{"id": storage.IntValue(4), "arr": storage.StringArrayValue([]string{"a", "b"})},
		{"id": storage.IntValue(5), "arr": storage.BoolArrayValue([]bool{true, false})},
	} {
		if _, err := gs.CreateNode([]string{"T"}, props); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}
	return NewExecutor(gs)
}

// extractStorageValue returns a typed slice for each stored array, and the
// equality of a list must read all of them.
func TestStoredArrayGoTypes(t *testing.T) {
	tests := []struct {
		name string
		val  storage.Value
		want any
	}{
		{"vector", storage.VectorValue([]float32{1}), []float32{}},
		{"float array", storage.FloatArrayValue([]float64{1}), []float64{}},
		{"int array", storage.IntArrayValue([]int64{1}), []int64{}},
		{"string array", storage.StringArrayValue([]string{"a"}), []string{}},
		{"bool array", storage.BoolArrayValue([]bool{true}), []bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractStorageValue(tt.val); reflect.TypeOf(got) != reflect.TypeOf(tt.want) {
				t.Errorf("extractStorageValue = %T, want %T", got, tt.want)
			}
		})
	}
}

func TestEqualityStoredVector_Where(t *testing.T) {
	tests := []struct {
		name  string
		where string
		param any
		want  []int64
	}{
		{"float64 JSON list equals", "p.embedding = $v", list{0.1, 0.25, 1.0}, []int64{1}},
		{"different element", "p.embedding = $v", list{0.1, 0.25, 2.0}, []int64{}},
		{"null element is unknown", "p.embedding = $v", list{0.1, nil, 1.0}, []int64{}},
		{"null element, different value is false", "p.embedding <> $v", list{0.1, nil, 2.0}, []int64{1}},
		{"IN with a float32 element", "$v IN p.embedding", 0.25, []int64{1}},
		{"IN with 0.1", "$v IN p.embedding", 0.1, []int64{1}},
		{"IN absent", "$v IN p.embedding", 0.3, []int64{}},
		{"integer against float32", "p.embedding = $v", list{0.1, 0.25, int64(1)}, []int64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := storedArrayStore(t)
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE "+tt.where+" RETURN p.id AS id", map[string]any{"v": tt.param})
			if err != nil {
				t.Fatal(err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WHERE %s with %#v: ids = %v, want %v", tt.where, tt.param, got, tt.want)
			}
		})
	}
}

func TestEqualityStoredArrays_JSONStyleParameter(t *testing.T) {
	tests := []struct {
		name  string
		param any
		want  []int64
	}{
		{"float array", list{0.1, 0.25, 1.0}, []int64{1, 2}},
		{"int array as float64", list{1.0, 2.0, 3.0}, []int64{3}},
		{"string array", list{"a", "b"}, []int64{4}},
		{"bool array", list{true, false}, []int64{5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := storedArrayStore(t)
			// node 1 stores its list under embedding, so compare arr only.
			rs, err := runWithParams(t, ex, "MATCH (p:T) WHERE p.arr = $v OR p.embedding = $v RETURN p.id AS id", map[string]any{"v": tt.param})
			if err != nil {
				t.Fatal(err)
			}
			if got := sortedIDs(t, rs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ids = %v, want %v", got, tt.want)
			}
		})
	}
}

// Two float32 values compare directly, a float32 against a float64 or an
// integer at float32 precision, and NaN is not equal to NaN.
func TestEqualTri_Float32(t *testing.T) {
	nan32 := float32(0) / float32(zeroFloat)
	tests := []struct {
		name        string
		left, right any
		want        tri
	}{
		{"f32 = f32", []float32{0.1}, []float32{0.1}, triTrue},
		{"f32 = f64", []float32{0.1}, list{0.1}, triTrue},
		{"f32 = int", []float32{1}, list{int64(1)}, triTrue},
		{"f32 != f64", []float32{0.1}, list{0.2}, triFalse},
		{"f32 NaN", []float32{nan32}, []float32{nan32}, triFalse},
		{"f32 null", []float32{0.1}, list{nil}, triUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalTri(tt.left, tt.right); got != tt.want {
				t.Errorf("equalTri(%#v, %#v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// zeroFloat keeps the NaN out of constant folding, which rejects 0/0.
var zeroFloat float64
