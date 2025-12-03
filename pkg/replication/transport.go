package replication

import (
	"io"
	"time"
)

// Socket represents a messaging socket that can send and receive messages.
// This interface abstracts the underlying transport (NNG, ZMQ, or mock for testing).
type Socket interface {
	io.Closer
	Send([]byte) error
	Recv() ([]byte, error)
	SetRecvDeadline(d time.Duration) error
	SetSendDeadline(d time.Duration) error
}

// ListenSocket is a socket that can bind to an address and accept connections.
type ListenSocket interface {
	Socket
	Listen(addr string) error
}

// DialSocket is a socket that can connect to a remote address.
type DialSocket interface {
	Socket
	Dial(addr string) error
}

// SubscribeSocket is a SUB socket that can subscribe to topics.
type SubscribeSocket interface {
	DialSocket
	Subscribe(topic []byte) error
}

// SurveySocket is a SURVEYOR socket with survey timeout configuration.
type SurveySocket interface {
	ListenSocket
	SetSurveyTime(d time.Duration) error
}

// SocketFactory creates sockets for different messaging patterns.
// Implementations can provide real NNG sockets or mocks for testing.
type SocketFactory interface {
	// Publishers
	NewPubSocket() (ListenSocket, error)
	NewSubSocket() (SubscribeSocket, error)

	// Request/Response
	NewSurveyorSocket() (SurveySocket, error)
	NewRespondentSocket() (DialSocket, error)

	// Push/Pull
	NewPushSocket() (DialSocket, error)
	NewPullSocket() (ListenSocket, error)
}

// TransportConfig holds addresses for replication transport.
type TransportConfig struct {
	WALPublishAddr   string // e.g., "tcp://*:9090"
	HealthSurveyAddr string // e.g., "tcp://*:9091"
	WriteBufferAddr  string // e.g., "tcp://*:9092"
}

// DefaultTransportConfig returns the default transport configuration.
func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		WALPublishAddr:   "tcp://*:9090",
		HealthSurveyAddr: "tcp://*:9091",
		WriteBufferAddr:  "tcp://*:9092",
	}
}

// StorageStats holds basic storage statistics.
type StorageStats struct {
	NodeCount int
	EdgeCount int
}

// StorageReader provides read access to graph storage statistics.
// This is the minimal interface needed by replication components.
type StorageReader interface {
	GetStatistics() StorageStats
	GetCurrentLSN() uint64
}

// StorageWriter provides write access to graph storage.
type StorageWriter interface {
	CreateNode(labels []string, properties map[string]interface{}) (interface{}, error)
	CreateEdge(from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (interface{}, error)
}

// Storage combines read and write access for replication.
type Storage interface {
	StorageReader
	StorageWriter
}

// WriteOperation represents a buffered write operation for replication.
type WriteOperation struct {
	Type       string                 `json:"type"` // "create_node", "create_edge"
	Labels     []string               `json:"labels,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	FromNodeID uint64                 `json:"from_node_id,omitempty"`
	ToNodeID   uint64                 `json:"to_node_id,omitempty"`
	EdgeType   string                 `json:"edge_type,omitempty"`
	Weight     float64                `json:"weight,omitempty"`
}
