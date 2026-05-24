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
	// OpAddNodeLabels records the addition of one or more labels to an
	// existing node (post-create label mutation). Distinct from
	// OpUpdateNode because that op's payload only carries Properties —
	// extending it to optionally carry Labels would conflate two
	// semantically different mutations (property merge vs. label set
	// union) and complicate replay. New op + new replay handler keeps
	// each mutation's intent legible in the WAL.
	OpAddNodeLabels
	// OpRemoveNodeLabel records the removal of a single label from a
	// node. Single-label payload (vs. a slice) matches the consumer
	// surface — `DELETE /nodes/{id}/labels/{label}` removes one label
	// per call. Batch removal can be modeled as multiple ops at the
	// caller layer without losing replay clarity.
	OpRemoveNodeLabel
)

// Entry represents a single WAL entry
type Entry struct {
	LSN       uint64 // Log Sequence Number
	OpType    OpType
	Data      []byte
	Checksum  uint32
	Timestamp int64
}
