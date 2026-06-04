package wal

// OpType represents the type of operation in the WAL
type OpType uint8

const (
	OpCreateNode OpType = iota
	OpUpdateNode
	OpDeleteNode
	OpCreateEdge
	OpUpdateEdge
	OpDeleteEdge
	OpCreatePropertyIndex
	OpDropPropertyIndex
	// Appended after the original set so existing WAL files (which encode
	// OpType as a single byte) keep replaying their stored ops correctly —
	// never renumber the values above.
	OpCreateVectorIndex
	OpDropVectorIndex
)

// Entry represents a single WAL entry
type Entry struct {
	LSN       uint64 // Log Sequence Number
	OpType    OpType
	Data      []byte
	Checksum  uint32
	Timestamp int64
}
