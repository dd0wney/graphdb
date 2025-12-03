package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// WALPublisher handles publishing WAL entries to subscribers.
// Single responsibility: fan-out WAL entries to replicas.
type WALPublisher struct {
	socket    ListenSocket
	addr      string
	stream    chan *wal.Entry
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   bool
	runningMu sync.Mutex
}

// WALPublisherConfig configures the WAL publisher.
type WALPublisherConfig struct {
	Address    string
	BufferSize int
}

// NewWALPublisher creates a new WAL publisher.
func NewWALPublisher(factory SocketFactory, config WALPublisherConfig) (*WALPublisher, error) {
	socket, err := factory.NewPubSocket()
	if err != nil {
		return nil, fmt.Errorf("failed to create PUB socket: %w", err)
	}

	bufSize := config.BufferSize
	if bufSize <= 0 {
		bufSize = 1000
	}

	return &WALPublisher{
		socket: socket,
		addr:   config.Address,
		stream: make(chan *wal.Entry, bufSize),
		stopCh: make(chan struct{}),
	}, nil
}

// Start begins publishing WAL entries.
func (p *WALPublisher) Start() error {
	p.runningMu.Lock()
	defer p.runningMu.Unlock()

	if p.running {
		return fmt.Errorf("WAL publisher already running")
	}

	if err := p.socket.Listen(p.addr); err != nil {
		return fmt.Errorf("failed to bind PUB socket to %s: %w", p.addr, err)
	}

	p.running = true
	p.wg.Add(1)
	go p.publishLoop()

	log.Printf("WAL publisher started on %s", p.addr)
	return nil
}

// Stop stops the publisher.
func (p *WALPublisher) Stop() error {
	p.runningMu.Lock()
	defer p.runningMu.Unlock()

	if !p.running {
		return nil
	}

	close(p.stopCh)
	p.running = false
	p.wg.Wait()

	if err := p.socket.Close(); err != nil {
		log.Printf("Warning: Failed to close WAL publisher socket: %v", err)
	}

	log.Printf("WAL publisher stopped")
	return nil
}

// Publish queues a WAL entry for publishing.
func (p *WALPublisher) Publish(entry *wal.Entry) error {
	select {
	case p.stream <- entry:
		return nil
	case <-p.stopCh:
		return fmt.Errorf("WAL publisher stopped")
	}
}

func (p *WALPublisher) publishLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopCh:
			return
		case entry := <-p.stream:
			data, err := json.Marshal(entry)
			if err != nil {
				log.Printf("Failed to marshal WAL entry: %v", err)
				continue
			}

			// Prepend topic for filtering
			msg := append([]byte("WAL:"), data...)
			if err := p.socket.Send(msg); err != nil {
				log.Printf("Failed to publish WAL entry: %v", err)
			}
		}
	}
}
