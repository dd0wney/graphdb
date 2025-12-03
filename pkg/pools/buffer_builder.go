package pools

// BufferBuilder provides a convenient way to build byte slices with pooling.
type BufferBuilder struct {
	buf  []byte
	pool *BytePool
}

// NewBufferBuilder creates a new buffer builder with the given initial capacity.
func NewBufferBuilder(initialCap int) *BufferBuilder {
	return &BufferBuilder{
		buf:  defaultBytePool.Get(initialCap),
		pool: defaultBytePool,
	}
}

// Write appends bytes to the buffer.
func (b *BufferBuilder) Write(p []byte) {
	b.buf = append(b.buf, p...)
}

// WriteByte appends a single byte.
func (b *BufferBuilder) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

// WriteUint64BE appends a uint64 in big-endian order.
func (b *BufferBuilder) WriteUint64BE(v uint64) {
	b.buf = append(b.buf,
		byte(v>>56),
		byte(v>>48),
		byte(v>>40),
		byte(v>>32),
		byte(v>>24),
		byte(v>>16),
		byte(v>>8),
		byte(v),
	)
}

// WriteUint32BE appends a uint32 in big-endian order.
func (b *BufferBuilder) WriteUint32BE(v uint32) {
	b.buf = append(b.buf,
		byte(v>>24),
		byte(v>>16),
		byte(v>>8),
		byte(v),
	)
}

// WriteString appends a string.
func (b *BufferBuilder) WriteString(s string) {
	b.buf = append(b.buf, s...)
}

// Bytes returns the built buffer. After calling Bytes, the builder should not be used.
func (b *BufferBuilder) Bytes() []byte {
	return b.buf
}

// Len returns the current length of the buffer.
func (b *BufferBuilder) Len() int {
	return len(b.buf)
}

// Reset resets the buffer for reuse.
func (b *BufferBuilder) Reset() {
	b.buf = b.buf[:0]
}

// Release returns the buffer to the pool. After Release, the builder should not be used.
func (b *BufferBuilder) Release() {
	if b.pool != nil && b.buf != nil {
		b.pool.Put(b.buf)
	}
	b.buf = nil
}
