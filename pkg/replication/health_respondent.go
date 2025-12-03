package replication

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// HealthRespondent responds to health surveys from the primary.
// Single responsibility: respond to health surveys.
type HealthRespondent struct {
	socket        DialSocket
	primaryAddr   string
	stateProvider StateProvider
	recvTimeout   time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	running       bool
	runningMu     sync.Mutex
}

// HealthRespondentConfig configures the health respondent.
type HealthRespondentConfig struct {
	PrimaryAddr string
	RecvTimeout time.Duration
}

// NewHealthRespondent creates a new health respondent.
func NewHealthRespondent(
	factory SocketFactory,
	config HealthRespondentConfig,
	stateProvider StateProvider,
) (*HealthRespondent, error) {
	socket, err := factory.NewRespondentSocket()
	if err != nil {
		return nil, err
	}

	timeout := config.RecvTimeout
	if timeout <= 0 {
		timeout = 1 * time.Second
	}

	return &HealthRespondent{
		socket:        socket,
		primaryAddr:   config.PrimaryAddr,
		stateProvider: stateProvider,
		recvTimeout:   timeout,
		stopCh:        make(chan struct{}),
	}, nil
}

// Start begins responding to health surveys.
func (r *HealthRespondent) Start() error {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()

	if r.running {
		return nil
	}

	if err := r.socket.Dial(r.primaryAddr); err != nil {
		return err
	}

	if err := r.socket.SetRecvDeadline(r.recvTimeout); err != nil {
		r.socket.Close()
		return err
	}

	r.running = true
	r.wg.Add(1)
	go r.respondLoop()

	log.Printf("Health respondent connected to %s", r.primaryAddr)
	return nil
}

// Stop stops the respondent.
func (r *HealthRespondent) Stop() error {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()

	if !r.running {
		return nil
	}

	close(r.stopCh)
	r.running = false
	r.wg.Wait()
	r.socket.Close()

	log.Printf("Health respondent stopped")
	return nil
}

func (r *HealthRespondent) respondLoop() {
	defer r.wg.Done()

	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		// Wait for survey
		_, err := r.socket.Recv()
		if err != nil {
			continue // Timeout
		}

		// Build response
		response := HeartbeatMessage{
			From:       r.stateProvider.GetID(),
			CurrentLSN: r.stateProvider.GetCurrentLSN(),
			NodeCount:  r.stateProvider.GetNodeCount(),
			EdgeCount:  r.stateProvider.GetEdgeCount(),
		}

		data, err := json.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal health response: %v", err)
			continue
		}

		if err := r.socket.Send(data); err != nil {
			log.Printf("Failed to send health response: %v", err)
		}
	}
}
