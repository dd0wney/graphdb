package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/logging"
)

// connectionManager manages connection to primary
func (rn *ReplicaNode) connectionManager() {
	defer rn.wg.Done()
	for {
		select {
		case <-rn.stopCh:
			return
		default:
		}

		// Connect to primary
		if err := rn.connectToPrimary(); err != nil {
			log.Printf("Failed to connect to primary: %v", err)

			select {
			case <-rn.stopCh:
				return
			case <-time.After(rn.config.ReconnectDelay):
				continue
			}
		}

		// Receive and apply WAL entries
		rn.receiveFromPrimary()

		// Disconnected, wait before reconnecting
		rn.disconnect()

		select {
		case <-rn.stopCh:
			return
		case <-time.After(rn.config.ReconnectDelay):
		}
	}
}

// connectToPrimary establishes connection to primary
//
// Concurrent Edge Cases:
// 1. Called from connectionManager goroutine only (single-threaded access)
// 2. However, Stop() can be called concurrently, triggering disconnect()
// 3. Uses local encoder/decoder variables during handshake to avoid races
//   - Only stores in struct fields after handshake succeeds
//   - This prevents disconnect() from interfering during handshake
//
// 4. After storing conn/encoder/decoder, they become visible to:
//   - sendHeartbeats goroutine (spawned at end of this function)
//   - disconnect() (can be called from Stop() at any time)
//
// 5. All these goroutines use proper locking (connectedMu) for safe access
func (rn *ReplicaNode) connectToPrimary() error {
	rn.logger.Info("connecting to primary",
		logging.String("primary_addr", rn.config.PrimaryAddr),
		logging.Operation("connect"))

	// Use configured connect timeout
	connectTimeout := rn.config.GetConnectTimeout()
	conn, err := net.DialTimeout("tcp", rn.config.PrimaryAddr, connectTimeout)
	if err != nil {
		return fmt.Errorf("failed to dial primary (timeout=%v): %w", connectTimeout, err)
	}

	// Set handshake timeout
	handshakeTimeout := rn.config.GetHandshakeTimeout()
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after deadline error: %v", closeErr)
		}
		return fmt.Errorf("failed to set handshake deadline: %w", err)
	}

	// Use local encoder/decoder for handshake to avoid races
	// Note: json.NewEncoder and json.NewDecoder do not return errors - they always succeed
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Perform handshake
	if err := rn.performHandshake(conn, encoder, decoder); err != nil {
		return err
	}

	// Clear handshake deadline after successful handshake
	if err := conn.SetDeadline(time.Time{}); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after clearing deadline: %v", closeErr)
		}
		return fmt.Errorf("failed to clear handshake deadline: %w", err)
	}

	// Start heartbeat sender
	rn.wg.Add(1)
	go rn.sendHeartbeats()

	return nil
}

// performHandshake performs the handshake protocol with the primary
func (rn *ReplicaNode) performHandshake(conn net.Conn, encoder *json.Encoder, decoder *json.Decoder) error {
	// Get last applied LSN under lock for handshake
	rn.connectedMu.RLock()
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.RUnlock()

	// Send handshake
	handshake := HandshakeRequest{
		ReplicaID:    rn.replicaID,
		LastLSN:      lastAppliedLSN,
		Version:      "1.0",
		Capabilities: []string{"wal-streaming"},
	}

	msg, err := NewMessage(MsgHandshake, handshake)
	if err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after handshake creation error: %v", closeErr)
		}
		return fmt.Errorf("failed to create handshake: %w", err)
	}

	if err := encoder.Encode(msg); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after handshake send error: %v", closeErr)
		}
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// Receive handshake response
	var responseMsg Message
	if err := decoder.Decode(&responseMsg); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after handshake receive error: %v", closeErr)
		}
		return fmt.Errorf("failed to receive handshake response: %w", err)
	}

	var response HandshakeResponse
	if err := responseMsg.Decode(&response); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after handshake decode error: %v", closeErr)
		}
		return fmt.Errorf("failed to decode handshake response: %w", err)
	}

	if !response.Accepted {
		if closeErr := conn.Close(); closeErr != nil {
			log.Printf("Failed to close connection after handshake rejection: %v", closeErr)
		}
		return fmt.Errorf("handshake rejected: %s", response.ErrorMessage)
	}

	// Handshake succeeded - store connection state under lock
	rn.connectedMu.Lock()
	rn.conn = conn
	rn.encoder = encoder
	rn.decoder = decoder
	rn.primaryID = response.PrimaryID
	rn.connectedMu.Unlock()
	rn.setConnected(true)

	rn.logger.Info("connected to primary",
		logging.String("primary_id", rn.primaryID),
		logging.Uint64("current_lsn", response.CurrentLSN),
		logging.Operation("connected"))

	return nil
}

// disconnect closes connection to primary
//
// Concurrent Edge Cases:
// 1. Can be called concurrently from Stop() and connectionManager()
// 2. Must be safe to call multiple times (idempotent)
// 3. encoder/decoder may be in use by sendHeartbeats/receiveFromPrimary goroutines
//   - These goroutines check encoder/decoder != nil before use
//   - Setting to nil here will cause them to exit gracefully
//
// 4. conn.Close() may race with active Read/Write operations
//   - Go's net.Conn.Close() is safe to call concurrently
//   - Active operations will return "use of closed connection" error
func (rn *ReplicaNode) disconnect() {
	rn.setConnected(false)

	rn.connectedMu.Lock()
	if rn.conn != nil {
		if err := rn.conn.Close(); err != nil {
			log.Printf("Warning: Failed to close connection: %v", err)
		}
		rn.conn = nil
	}
	rn.encoder = nil
	rn.decoder = nil
	rn.connectedMu.Unlock()
}

// isConnected returns connection status
func (rn *ReplicaNode) isConnected() bool {
	rn.connectedMu.RLock()
	defer rn.connectedMu.RUnlock()
	return rn.connected
}

// setConnected sets connection status
func (rn *ReplicaNode) setConnected(connected bool) {
	rn.connectedMu.Lock()
	defer rn.connectedMu.Unlock()
	rn.connected = connected
}
