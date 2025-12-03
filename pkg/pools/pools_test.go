package pools

import (
	"sync"
	"testing"
)

func TestBytePool_Get(t *testing.T) {
	pool := NewBytePool()

	tests := []struct {
		name     string
		size     int
		minCap   int
		maxCap   int
	}{
		{"tiny", 8, 8, TinySize},
		{"tiny_exact", TinySize, TinySize, TinySize},
		{"small", 32, 32, SmallSize},
		{"small_exact", SmallSize, SmallSize, SmallSize},
		{"medium", 128, 128, MediumSize},
		{"medium_exact", MediumSize, MediumSize, MediumSize},
		{"large", 512, 512, LargeSize},
		{"large_exact", LargeSize, LargeSize, LargeSize},
		{"huge", 2048, 2048, HugeSize},
		{"huge_exact", HugeSize, HugeSize, HugeSize},
		{"oversized", 10000, 10000, 10000}, // Allocated directly
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := pool.Get(tt.size)
			if len(b) != 0 {
				t.Errorf("Get(%d) length = %d, want 0", tt.size, len(b))
			}
			if cap(b) < tt.minCap {
				t.Errorf("Get(%d) capacity = %d, want >= %d", tt.size, cap(b), tt.minCap)
			}
		})
	}
}

func TestBytePool_GetSized(t *testing.T) {
	pool := NewBytePool()

	b := pool.GetSized(100)
	if len(b) != 100 {
		t.Errorf("GetSized(100) length = %d, want 100", len(b))
	}
	if cap(b) < 100 {
		t.Errorf("GetSized(100) capacity = %d, want >= 100", cap(b))
	}
}

func TestBytePool_PutAndReuse(t *testing.T) {
	pool := NewBytePool()

	// Get and return multiple buffers
	for i := 0; i < 10; i++ {
		b := pool.Get(64)
		b = append(b, "test data"...)
		pool.Put(b)
	}

	// Get again and verify it's clean
	b := pool.Get(64)
	if len(b) != 0 {
		t.Errorf("After Put, Get returned slice with length %d, want 0", len(b))
	}
}

func TestBytePool_OversizedNotPooled(t *testing.T) {
	pool := NewBytePool()

	// Large buffer should not cause issues
	large := make([]byte, MaxPool+1000)
	pool.Put(large) // Should not panic or error
}

func TestDefaultBytePool(t *testing.T) {
	b := GetBytes(100)
	if cap(b) < 100 {
		t.Errorf("GetBytes(100) capacity = %d, want >= 100", cap(b))
	}
	PutBytes(b)

	b2 := GetBytesSized(50)
	if len(b2) != 50 {
		t.Errorf("GetBytesSized(50) length = %d, want 50", len(b2))
	}
	PutBytes(b2)
}

func TestUint64Pool_Get(t *testing.T) {
	pool := NewUint64Pool()

	tests := []struct {
		name   string
		size   int
		minCap int
	}{
		{"small", 8, 8},
		{"small_max", 16, 16},
		{"medium", 32, 32},
		{"medium_max", 64, 64},
		{"large", 128, 128},
		{"large_max", 256, 256},
		{"oversized", 1000, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := pool.Get(tt.size)
			if len(s) != 0 {
				t.Errorf("Get(%d) length = %d, want 0", tt.size, len(s))
			}
			if cap(s) < tt.minCap {
				t.Errorf("Get(%d) capacity = %d, want >= %d", tt.size, cap(s), tt.minCap)
			}
		})
	}
}

func TestUint64Pool_PutAndReuse(t *testing.T) {
	pool := NewUint64Pool()

	for i := 0; i < 10; i++ {
		s := pool.Get(16)
		s = append(s, 1, 2, 3, 4, 5)
		pool.Put(s)
	}

	s := pool.Get(16)
	if len(s) != 0 {
		t.Errorf("After Put, Get returned slice with length %d, want 0", len(s))
	}
}

func TestDefaultUint64Pool(t *testing.T) {
	s := GetUint64s(32)
	if cap(s) < 32 {
		t.Errorf("GetUint64s(32) capacity = %d, want >= 32", cap(s))
	}
	PutUint64s(s)
}

func TestStringMapPool_Get(t *testing.T) {
	pool := NewStringMapPool()

	m := pool.Get()
	if m == nil {
		t.Error("Get() returned nil")
	}
	if len(m) != 0 {
		t.Errorf("Get() returned map with length %d, want 0", len(m))
	}
}

func TestStringMapPool_PutAndReuse(t *testing.T) {
	pool := NewStringMapPool()

	m := pool.Get()
	m["key1"] = "value1"
	m["key2"] = 42
	pool.Put(m)

	// Get another map - should be cleared
	m2 := pool.Get()
	if len(m2) != 0 {
		t.Errorf("After Put, Get returned map with length %d, want 0", len(m2))
	}
}

func TestStringMapPool_NilNotPooled(t *testing.T) {
	pool := NewStringMapPool()
	pool.Put(nil) // Should not panic
}

func TestDefaultStringMapPool(t *testing.T) {
	m := GetStringMap()
	if m == nil {
		t.Error("GetStringMap() returned nil")
	}
	m["test"] = "value"
	PutStringMap(m)
}

func TestBufferBuilder(t *testing.T) {
	b := NewBufferBuilder(64)
	defer b.Release()

	b.WriteByte(0x01)
	b.WriteUint32BE(0x12345678)
	b.WriteUint64BE(0xABCDEF0123456789)
	b.WriteString("hello")
	b.Write([]byte{0xFF, 0xFE})

	result := b.Bytes()

	// Verify length
	expectedLen := 1 + 4 + 8 + 5 + 2 // 20 bytes
	if len(result) != expectedLen {
		t.Errorf("Buffer length = %d, want %d", len(result), expectedLen)
	}

	// Verify first byte
	if result[0] != 0x01 {
		t.Errorf("result[0] = %02x, want 0x01", result[0])
	}

	// Verify uint32
	if result[1] != 0x12 || result[2] != 0x34 || result[3] != 0x56 || result[4] != 0x78 {
		t.Error("uint32 encoding incorrect")
	}

	// Verify uint64
	expected64 := []byte{0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89}
	for i, exp := range expected64 {
		if result[5+i] != exp {
			t.Errorf("uint64 byte %d = %02x, want %02x", i, result[5+i], exp)
		}
	}

	// Verify string
	if string(result[13:18]) != "hello" {
		t.Errorf("string = %q, want %q", string(result[13:18]), "hello")
	}

	// Verify final bytes
	if result[18] != 0xFF || result[19] != 0xFE {
		t.Error("trailing bytes incorrect")
	}
}

func TestBufferBuilder_Len(t *testing.T) {
	b := NewBufferBuilder(32)
	defer b.Release()

	if b.Len() != 0 {
		t.Errorf("Initial Len() = %d, want 0", b.Len())
	}

	b.WriteString("test")
	if b.Len() != 4 {
		t.Errorf("After write Len() = %d, want 4", b.Len())
	}
}

func TestBufferBuilder_Reset(t *testing.T) {
	b := NewBufferBuilder(32)
	defer b.Release()

	b.WriteString("test data")
	b.Reset()

	if b.Len() != 0 {
		t.Errorf("After Reset() Len() = %d, want 0", b.Len())
	}

	// Can reuse after reset
	b.WriteString("new data")
	if string(b.Bytes()) != "new data" {
		t.Errorf("After Reset and write, got %q, want %q", string(b.Bytes()), "new data")
	}
}

func TestBytePool_Concurrent(t *testing.T) {
	pool := NewBytePool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b := pool.Get(64)
				b = append(b, "concurrent test data"...)
				pool.Put(b)
			}
		}()
	}

	wg.Wait()
}

func TestUint64Pool_Concurrent(t *testing.T) {
	pool := NewUint64Pool()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s := pool.Get(32)
				s = append(s, 1, 2, 3, 4, 5, 6, 7, 8)
				pool.Put(s)
			}
		}()
	}

	wg.Wait()
}

func BenchmarkBytePool_Get(b *testing.B) {
	pool := NewBytePool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Get(128)
		pool.Put(buf)
	}
}

func BenchmarkBytePool_GetWithoutPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 0, 128)
	}
}

func BenchmarkUint64Pool_Get(b *testing.B) {
	pool := NewUint64Pool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s := pool.Get(32)
		pool.Put(s)
	}
}

func BenchmarkUint64Pool_GetWithoutPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]uint64, 0, 32)
	}
}

func BenchmarkBufferBuilder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		bb := NewBufferBuilder(64)
		bb.WriteByte(0x01)
		bb.WriteUint64BE(12345)
		bb.WriteString("test")
		_ = bb.Bytes()
		bb.Release()
	}
}

func BenchmarkBufferBuilder_WithoutPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 0, 64)
		buf = append(buf, 0x01)
		buf = append(buf, 0, 0, 0, 0, 0, 0, 0x30, 0x39) // 12345 in BE
		buf = append(buf, "test"...)
		_ = buf
	}
}
