package replication

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ReplicaTracker tracks replica health state.
type ReplicaTracker interface {
	UpdateReplica(id string, lsn uint64)
	MarkUnhealthy(id string)
	GetReplicas() []ReplicaInfo
}

// ReplicaInfo holds replica state information.
type ReplicaInfo struct {
	ReplicaID      string
	LastSeen       time.Time
	LastAppliedLSN uint64
	Healthy        bool
}

// StateProvider provides the current state for health surveys.
type StateProvider interface {
	GetID() string
	GetCurrentLSN() uint64
	GetNodeCount() uint64
	GetEdgeCount() uint64
}

// HealthSurveyor broadcasts health surveys to replicas and collects responses.
// Single responsibility: monitor replica health.
type HealthSurveyor struct {
	socket        SurveySocket
	addr          string
	stateProvider StateProvider
	tracker       ReplicaTracker
	surveyTime    time.Duration
	interval      time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	running       bool
	runningMu     sync.Mutex
}

// HealthSurveyorConfig configures the health surveyor.
type HealthSurveyorConfig struct {
	Address       string
	SurveyTimeout time.Duration
	Interval      time.Duration
}

// NewHealthSurveyor creates a new health surveyor.
func NewHealthSurveyor(
	factory SocketFactory,
	config HealthSurveyorConfig,
	stateProvider StateProvider,
	tracker ReplicaTracker,
) (*HealthSurveyor, error) {
	socket, err := factory.NewSurveyorSocket()
	if err != nil {
		return nil, err
	}

	surveyTime := config.SurveyTimeout
	if surveyTime <= 0 {
		surveyTime = 2 * time.Second
	}

	interval := config.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	return &HealthSurveyor{
		socket:        socket,
		addr:          config.Address,
		stateProvider: stateProvider,
		tracker:       tracker,
		surveyTime:    surveyTime,
		interval:      interval,
		stopCh:        make(chan struct{}),
	}, nil
}

// Start begins the health survey loop.
func (s *HealthSurveyor) Start() error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if s.running {
		return nil
	}

	if err := s.socket.Listen(s.addr); err != nil {
		return err
	}

	if err := s.socket.SetSurveyTime(s.surveyTime); err != nil {
		s.socket.Close()
		return err
	}

	s.running = true
	s.wg.Add(1)
	go s.surveyLoop()

	log.Printf("Health surveyor started on %s", s.addr)
	return nil
}

// Stop stops the surveyor.
func (s *HealthSurveyor) Stop() error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()

	if !s.running {
		return nil
	}

	close(s.stopCh)
	s.running = false
	s.wg.Wait()
	s.socket.Close()

	log.Printf("Health surveyor stopped")
	return nil
}

func (s *HealthSurveyor) surveyLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.conductSurvey()
		}
	}
}

func (s *HealthSurveyor) conductSurvey() {
	// Build survey message
	survey := HeartbeatMessage{
		From:       s.stateProvider.GetID(),
		CurrentLSN: s.stateProvider.GetCurrentLSN(),
		NodeCount:  s.stateProvider.GetNodeCount(),
		EdgeCount:  s.stateProvider.GetEdgeCount(),
	}

	data, err := json.Marshal(survey)
	if err != nil {
		log.Printf("Failed to marshal survey: %v", err)
		return
	}

	// Send survey
	if err := s.socket.Send(data); err != nil {
		log.Printf("Failed to send survey: %v", err)
		return
	}

	// Collect responses
	responded := make(map[string]bool)
	for {
		msg, err := s.socket.Recv()
		if err != nil {
			break // Timeout or no more responses
		}

		var hc HeartbeatMessage
		if err := json.Unmarshal(msg, &hc); err != nil {
			log.Printf("Failed to parse survey response: %v", err)
			continue
		}

		s.tracker.UpdateReplica(hc.From, hc.CurrentLSN)
		responded[hc.From] = true
	}

	// Mark non-responders as potentially unhealthy
	for _, replica := range s.tracker.GetReplicas() {
		if !responded[replica.ReplicaID] && time.Since(replica.LastSeen) > 30*time.Second {
			s.tracker.MarkUnhealthy(replica.ReplicaID)
		}
	}

	if len(responded) > 0 {
		log.Printf("Health survey: %d replicas responded", len(responded))
	}
}
