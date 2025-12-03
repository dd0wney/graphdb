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
)

// Entry represents a single WAL entry
type Entry struct {
	LSN       uint64 // Log Sequence Number
	OpType    OpType
	Data      []byte
	Checksum  uint32
	Timestamp int64
}
