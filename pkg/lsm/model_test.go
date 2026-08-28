package lsm

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
)

// opKind is one operation in a generated sequence.
type opKind int

const (
	opPut opKind = iota
	opDelete
	opSync
	opReopen
)

// modelOp is one generated operation. It is a value rather than a closure so a
// failing sequence can be printed and replayed by hand.
type modelOp struct {
	kind  opKind
	key   string
	value string
}

func (o modelOp) String() string {
	switch o.kind {
	case opPut:
		return fmt.Sprintf("Put(%q, %q)", o.key, o.value)
	case opDelete:
		return fmt.Sprintf("Delete(%q)", o.key)
	case opSync:
		return "Sync()"
	case opReopen:
		return "Reopen()"
	}
	return "?"
}

// generate builds a sequence biased towards collisions: a small key space, so
// the same key is written, deleted and rewritten across several flushes. A
// uniform key space over a large alphabet almost never revisits a key, and a
// tombstone that is never followed by a read of the same key proves nothing.
func generate(rng *rand.Rand, n int) []modelOp {
	ops := make([]modelOp, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%d", rng.Intn(modelKeySpace))
		switch roll := rng.Intn(100); {
		case roll < 45:
			ops = append(ops, modelOp{kind: opPut, key: key, value: fmt.Sprintf("v%d", i)})
		case roll < 70:
			ops = append(ops, modelOp{kind: opDelete, key: key})
		case roll < 92:
			ops = append(ops, modelOp{kind: opSync})
		default:
			ops = append(ops, modelOp{kind: opReopen})
		}
	}
	return ops
}

const (
	modelKeySpace = 8
	modelSeeds    = 32
	modelOps      = 120
)

// Get and Scan must answer the same question the same way. This needs no
// reference model: it is a property of the store against itself, and it is the
// cheapest test that would have caught the disagreement about which SSTable is
// newer.
func TestGetAndScanAgree(t *testing.T) {
	for seed := int64(0); seed < modelSeeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			ops := generate(rng, modelOps)

			dir := t.TempDir()
			l := openModelStore(t, dir)

			for i, op := range ops {
				l = applyModelOp(t, l, dir, op)

				scanned, err := l.Scan([]byte("k0"), []byte("l"))
				if err != nil {
					t.Fatalf("after %d ops (%v): Scan: %v", i+1, op, err)
				}
				for k := 0; k < modelKeySpace; k++ {
					key := fmt.Sprintf("k%d", k)
					got, present := l.Get([]byte(key))
					want, inScan := scanned[key]

					if present != inScan {
						t.Fatalf("after %d ops (last: %v): Get(%q) present=%v but Scan present=%v\nsequence: %v",
							i+1, op, key, present, inScan, ops[:i+1])
					}
					if present && string(got) != string(want) {
						t.Fatalf("after %d ops (last: %v): Get(%q)=%q but Scan gives %q\nsequence: %v",
							i+1, op, key, got, want, ops[:i+1])
					}
				}
			}
			_ = l.Close()
		})
	}
}

// The store must behave like a map. This is the property that catches a
// tombstone which fails to hide an older value, whichever access path finds it.
func TestReadPathMatchesReferenceMap(t *testing.T) {
	for seed := int64(0); seed < modelSeeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed + 1000))
			ops := generate(rng, modelOps)

			dir := t.TempDir()
			l := openModelStore(t, dir)
			model := make(map[string]string)

			for i, op := range ops {
				switch op.kind {
				case opPut:
					model[op.key] = op.value
				case opDelete:
					delete(model, op.key)
				}
				l = applyModelOp(t, l, dir, op)

				for k := 0; k < modelKeySpace; k++ {
					key := fmt.Sprintf("k%d", k)
					got, present := l.Get([]byte(key))
					want, inModel := model[key]

					if present != inModel {
						t.Fatalf("after %d ops (last: %v): Get(%q) present=%v, the model says %v\nsequence: %v",
							i+1, op, key, present, inModel, ops[:i+1])
					}
					if present && string(got) != want {
						t.Fatalf("after %d ops (last: %v): Get(%q)=%q, the model says %q\nsequence: %v",
							i+1, op, key, got, want, ops[:i+1])
					}
				}

				scanned, err := l.Scan([]byte("k0"), []byte("l"))
				if err != nil {
					t.Fatalf("after %d ops (%v): Scan: %v", i+1, op, err)
				}
				if len(scanned) != len(model) {
					t.Fatalf("after %d ops (last: %v): Scan returned %d keys, the model has %d\nscan: %v\nmodel: %v\nsequence: %v",
						i+1, op, len(scanned), len(model), sortedKeys(scanned), sortedModelKeys(model), ops[:i+1])
				}
			}
			_ = l.Close()
		})
	}
}

func openModelStore(t *testing.T, dir string) *LSMStorage {
	t.Helper()
	l, err := NewLSMStorage(LSMOptions{
		DataDir:      dir,
		MemTableSize: 128, // small, so the sequence crosses many flushes
		// The workers are off: this test is about the read path, not about
		// timing. A background flush would make a failure hard to replay.
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: false,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	return l
}

// applyModelOp performs one operation and returns the store to keep using,
// which differs from the one passed in only for a reopen.
func applyModelOp(t *testing.T, l *LSMStorage, dir string, op modelOp) *LSMStorage {
	t.Helper()
	switch op.kind {
	case opPut:
		if err := l.Put([]byte(op.key), []byte(op.value)); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opDelete:
		if err := l.Delete([]byte(op.key)); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opSync:
		if err := l.Sync(); err != nil {
			t.Fatalf("%v: %v", op, err)
		}
	case opReopen:
		if err := l.Close(); err != nil {
			t.Fatalf("close before reopen: %v", err)
		}
		return openModelStore(t, dir)
	}
	return l
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedModelKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
