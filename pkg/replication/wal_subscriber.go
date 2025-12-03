package replication

import (
	"bytes"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// WALApplier applies WAL entries to storage.
type WALApplier interface {
	ApplyWALEntry(entry *wal.Entry) error
}

// WALSubscriber subscribes to WAL entries from a primary.
// Single responsibility: receive and apply WAL entries.
type WALSubscriber struct {
	socket      SubscribeSocket
	primaryAddr string
	applier     WALApplier
	recvTimeout time.Duration
	stopCh      chan struct{}
	wg          sync.WaitGroup
	running     bool
	runningMu   sync.Mutex
}

// WALSubscriberConfig configures the WAL subscriber.
type WALSubscriberConfig struct {
	PrimaryAddr string
	RecvTimeout time.Duration
}

// NewWALSubscriber creates a new WAL subscriber.
func NewWALSubscriber(
	factory SocketFactory,
	config WALSubscriberConfig,
	applier WALApplier,
) (*WALSubscriber, error) {
	socket, err := factory.NewSubSocket()
	if err != nil {
		return nil, err
	}

	timeout := config.RecvTimeout
	if timeout <= 0 {
		timeout = 1 * time.Second
	}

	return &WALSubscriber{
		socket:      socket,
		primaryAddr: config.PrimaryAddr,
		applier:     applier,
		recvTimeout: timeout,
		stopCh:      make(chan struct{}),
	}, nil
}

// Start begins receiving WAL entries.
func (s *WALSubscriber) Start() error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if s.running {
		return nil
	}

	if err := s.socket.Dial(s.primaryAddr); err != nil {
		return err
	}

	if err := s.socket.Subscribe([]byte("WAL:")); err != nil {
		s.socket.Close()
		return err
	}

	if err := s.socket.SetRecvDeadline(s.recvTimeout); err != nil {
		s.socket.Close()
		return err
	}

	s.running = true
	s.wg.Add(1)
	go s.subscribeLoop()

	log.Printf("WAL subscriber connected to %s", s.primaryAddr)
	return nil
}

// Stop stops the subscriber.
func (s *WALSubscriber) Stop() error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if !s.running {
		return nil
	}

	close(s.stopCh)
	s.running = false
	s.wg.Wait()
	s.socket.Close()

	log.Printf("WAL subscriber stopped")
	return nil
}

func (s *WALSubscriber) subscribeLoop() {
	defer s.wg.Done()

	const walPrefix = "WAL:"

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		msg, err := s.socket.Recv()
		if err != nil {
			continue // Timeout
		}

		// Strip topic prefix
		if !bytes.HasPrefix(msg, []byte(walPrefix)) {
			continue
		}
		data := msg[len(walPrefix):]

		var entry wal.Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			log.Printf("Failed to unmarshal WAL entry: %v", err)
			continue
		}

		if err := s.applier.ApplyWALEntry(&entry); err != nil {
			log.Printf("Failed to apply WAL entry: %v", err)
		}
	}
}
