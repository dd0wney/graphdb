package lsm

import (
	"hash/fnv"
	"math"
)

// BloomFilter is a probabilistic data structure for set membership testing
// - False positives possible (may say key exists when it doesn't)
// - False negatives impossible (if it says key doesn't exist, it definitely doesn't)
type BloomFilter struct {
	bits      []bool
	size      int
	hashCount int
}

// NewBloomFilter creates a Bloom filter optimized for the given parameters
// expectedItems: number of items to store
// falsePositiveRate: desired false positive rate (e.g., 0.01 for 1%)
func NewBloomFilter(expectedItems int, falsePositiveRate float64) *BloomFilter {
	// Validate inputs to prevent overflow/invalid calculations
	if expectedItems <= 0 {
		expectedItems = 1
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		falsePositiveRate = 0.01 // Default 1%
	}

	// Calculate optimal size and hash count
	// m = -(n * ln(p)) / (ln(2)^2)
	// k = (m/n) * ln(2)
	size := int(math.Ceil(-float64(expectedItems) * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2)))
	hashCount := int(math.Ceil((float64(size) / float64(expectedItems)) * math.Ln2))

	// Cap at reasonable limits to prevent memory exhaustion
	const maxSize = 1000000000 // 1 billion bits = ~119 MB
	if size > maxSize {
		size = maxSize
	}
	if size < 1 {
		size = 1
	}

	if hashCount < 1 {
		hashCount = 1
	}
	if hashCount > 100 {
		hashCount = 100 // Reasonable upper limit
	}

	return &BloomFilter{
		bits:      make([]bool, size),
		size:      size,
		hashCount: hashCount,
	}
}

// Add adds a key to the Bloom filter
func (bf *BloomFilter) Add(key []byte) {
	for i := 0; i < bf.hashCount; i++ {
		hash := bf.hash(key, i)
		bf.bits[hash] = true
	}
}

// MayContain checks if a key might be in the set
// Returns true if key might exist (with false positive rate)
// Returns false if key definitely doesn't exist
func (bf *BloomFilter) MayContain(key []byte) bool {
	for i := 0; i < bf.hashCount; i++ {
		hash := bf.hash(key, i)
		if !bf.bits[hash] {
			return false
		}
	}
	return true
}

// Contains is an alias for MayContain for better API usability
func (bf *BloomFilter) Contains(key []byte) bool {
	return bf.MayContain(key)
}

// hash generates the i-th hash value for a key using double hashing
// hash(key, i) = (hash1(key) + i * hash2(key)) % size
func (bf *BloomFilter) hash(key []byte, i int) int {
	// Calculate two independent hash values
	h1 := fnv.New64a()
	// Note: hash.Hash.Write never returns an error according to the interface contract
	_, _ = h1.Write(key)
	hash1 := h1.Sum64()

	h2 := fnv.New64a()
	// Note: hash.Hash.Write never returns an error according to the interface contract
	_, _ = h2.Write(key)
	_, _ = h2.Write([]byte{0xFF}) // Different seed for hash2
	hash2 := h2.Sum64()

	// Ensure hash2 is odd to avoid clustering (coprime with size)
	if hash2%2 == 0 {
		hash2++
	}

	// Double hashing: (h1 + i * h2) % size
	combinedHash := hash1 + uint64(i)*hash2
	return int(combinedHash % uint64(bf.size))
}

// Size returns the size of the filter in bits
func (bf *BloomFilter) Size() int {
	return bf.size
}

// HashCount returns the number of hash functions
func (bf *BloomFilter) HashCount() int {
	return bf.hashCount
}

// EstimateFalsePositiveRate estimates current false positive rate
func (bf *BloomFilter) EstimateFalsePositiveRate(itemCount int) float64 {
	// p = (1 - e^(-k*n/m))^k
	k := float64(bf.hashCount)
	n := float64(itemCount)
	m := float64(bf.size)

	return math.Pow(1.0-math.Exp(-k*n/m), k)
}

// Reset clears all bits in the filter
func (bf *BloomFilter) Reset() {
	for i := range bf.bits {
		bf.bits[i] = false
	}
}

// Merge combines another Bloom filter into this one (OR operation)
// Both filters must have the same size and hash count
func (bf *BloomFilter) Merge(other *BloomFilter) error {
	if bf.size != other.size || bf.hashCount != other.hashCount {
		return ErrIncompatibleFilters
	}

	for i := range bf.bits {
		bf.bits[i] = bf.bits[i] || other.bits[i]
	}

	return nil
}

// MarshalBinary serializes the Bloom filter
func (bf *BloomFilter) MarshalBinary() []byte {
	// Pack bits into bytes (8 bits per byte)
	byteCount := (bf.size + 7) / 8
	data := make([]byte, byteCount)

	for i := 0; i < bf.size; i++ {
		if bf.bits[i] {
			data[i/8] |= 1 << (i % 8)
		}
	}

	return data
}

// UnmarshalBinary deserializes the Bloom filter
func (bf *BloomFilter) UnmarshalBinary(data []byte) error {
	for i := 0; i < bf.size && i/8 < len(data); i++ {
		bf.bits[i] = (data[i/8] & (1 << (i % 8))) != 0
	}
	return nil
}

var ErrIncompatibleFilters = &BloomFilterError{"incompatible bloom filters"}

type BloomFilterError struct {
	msg string
}

func (e *BloomFilterError) Error() string {
	return e.msg
}
