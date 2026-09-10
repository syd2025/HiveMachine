package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SessionManagerConfig holds session manager configuration.
type SessionManagerConfig struct {
	HeartbeatInterval time.Duration
	SessionTimeout    time.Duration
	MaxMessageSize    int64
}

// DefaultSessionManagerConfig returns sensible defaults.
func DefaultSessionManagerConfig() *SessionManagerConfig {
	return &SessionManagerConfig{
		HeartbeatInterval: 30 * time.Second,
		SessionTimeout:    5 * time.Minute,
		MaxMessageSize:    64 * 1024 * 1024, // 64MB
	}
}

// SessionManager handles session lifecycle and message routing.
type SessionManager struct {
	host     *Host
	config   *SessionManagerConfig
	sessions map[SessionID]*ActiveSession
	mu       sync.RWMutex
	closed   bool
}

// NewSessionManager creates a new session manager.
func NewSessionManager(host *Host, cfg *SessionManagerConfig) (*SessionManager, error) {
	if host == nil {
		return nil, fmt.Errorf("host cannot be nil")
	}

	if cfg == nil {
		cfg = DefaultSessionManagerConfig()
	}

	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.SessionTimeout == 0 {
		cfg.SessionTimeout = 5 * time.Minute
	}
	if cfg.MaxMessageSize == 0 {
		cfg.MaxMessageSize = 64 * 1024 * 1024
	}

	sm := &SessionManager{
		host:     host,
		config:   cfg,
		sessions: make(map[SessionID]*ActiveSession),
	}

	// Subscribe to host events for session management
	host.Subscribe(sm)

	return sm, nil
}

// ActiveSession tracks an active session with its state.
type ActiveSession struct {
	ID           SessionID
	PeerID       string
	State        SessionState
	OpenedAt     time.Time
	Messages     int
	BytesIn      int64
	BytesOut     int64
	MessageChan  chan []byte
	mu           sync.RWMutex
	metadata     map[string]interface{}
	host         *Host
	config       *SessionManagerConfig
	lastActivity time.Time
}

// SetMetadata sets session metadata.
func (s *ActiveSession) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metadata == nil {
		s.metadata = make(map[string]interface{})
	}
	s.metadata[key] = value
}

// GetMetadata retrieves session metadata.
func (s *ActiveSession) GetMetadata(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.metadata[key]
	return v, ok
}

// UpdateActivity updates the last activity timestamp.
func (s *ActiveSession) UpdateActivity() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// MessageType for P2P protocol messages.
type MessageType string

const (
	MessageTypeData      MessageType = "data"
	MessageTypeControl   MessageType = "control"
	MessageTypeHeartbeat MessageType = "heartbeat"
	MessageTypeError     MessageType = "error"
)

// MessageHandler handles incoming messages.
type MessageHandler interface {
	HandleMessage(ctx context.Context, session *ActiveSession, msg []byte) error
}

// HandleP2PEvent implements EventHandler for session tracking.
func (sm *SessionManager) HandleP2PEvent(event *P2PEvent) {
	if event == nil {
		return
	}

	switch event.Type {
	case P2PEventDisconnect:
		// Clean up sessions for disconnected peer
		sm.mu.Lock()
		defer sm.mu.Unlock()
		for id, session := range sm.sessions {
			if session.PeerID == event.PeerID {
				session.mu.Lock()
				session.State = SessionStateClosed
				session.mu.Unlock()
				delete(sm.sessions, id)
			}
		}
	case P2PEventMessage:
		// Update session activity
		sm.mu.RLock()
		session, ok := sm.sessions[event.SessionID]
		sm.mu.RUnlock()
		if ok {
			session.UpdateActivity()
		}
	}
}

// OpenSession establishes a new session with a peer.
func (sm *SessionManager) OpenSession(ctx context.Context, remotePeer string) (*ActiveSession, error) {
	sm.mu.Lock()
	if sm.closed {
		sm.mu.Unlock()
		return nil, fmt.Errorf("session manager closed")
	}
	sm.mu.Unlock()

	// Create session via host
	session, err := sm.host.OpenSession(ctx, remotePeer)
	if err != nil {
		return nil, fmt.Errorf("opening session: %w", err)
	}

	activeSession := &ActiveSession{
		ID:           session.ID,
		PeerID:       session.PeerID,
		State:        SessionStateOpen,
		OpenedAt:     session.OpenedAt,
		host:         sm.host,
		config:       sm.config,
		lastActivity: time.Now(),
	}

	sm.mu.Lock()
	sm.sessions[session.ID] = activeSession
	sm.mu.Unlock()

	return activeSession, nil
}

// CloseSession closes an active session.
func (sm *SessionManager) CloseSession(ctx context.Context, sessionID SessionID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return fmt.Errorf("session manager closed")
	}

	session, ok := sm.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	session.State = SessionStateClosing
	session.mu.Unlock()

	// Close the underlying session
	if err := sm.host.CloseSession(ctx, sessionID); err != nil {
		return fmt.Errorf("closing host session: %w", err)
	}

	delete(sm.sessions, sessionID)
	return nil
}

// GetSession retrieves an active session.
func (sm *SessionManager) GetSession(sessionID SessionID) (*ActiveSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.closed {
		return nil, fmt.Errorf("session manager closed")
	}

	session, ok := sm.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

// ListSessions returns all active sessions.
func (sm *SessionManager) ListSessions() []*ActiveSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*ActiveSession, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// SendMessage sends a message on a session.
func (sm *SessionManager) SendMessage(ctx context.Context, sessionID SessionID, msgType MessageType, payload interface{}) error {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.RLock()
	state := session.State
	session.mu.RUnlock()

	if state != SessionStateOpen {
		return fmt.Errorf("session not open: %v", state)
	}

	// Serialize payload based on type
	var data []byte
	switch p := payload.(type) {
	case []byte:
		data = p
	case string:
		data = []byte(p)
	default:
		return fmt.Errorf("unsupported payload type: %T", payload)
	}

	// Wrap message with type header
	msg := &Message{
		Type:    msgType,
		Payload: data,
		SentAt:  time.Now(),
	}

	msgData, err := msg.Marshal()
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	if err := sm.host.Send(ctx, sessionID, msgData); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}

	session.mu.Lock()
	session.BytesOut += int64(len(data))
	session.Messages++
	session.lastActivity = time.Now()
	session.mu.Unlock()

	return nil
}

// BroadcastMessage sends a message to all sessions.
func (sm *SessionManager) BroadcastMessage(ctx context.Context, msgType MessageType, payload interface{}) error {
	sm.mu.RLock()
	sessions := make([]*ActiveSession, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	sm.mu.RUnlock()

	var lastErr error
	for _, session := range sessions {
		if err := sm.SendMessage(ctx, session.ID, msgType, payload); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// StartHeartbeat starts the heartbeat routine for session keep-alive.
func (sm *SessionManager) StartHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(sm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.checkSessions()
		}
	}
}

// checkSessions checks session health and cleans up stale sessions.
func (sm *SessionManager) checkSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for id, session := range sm.sessions {
		session.mu.RLock()
		isOpen := session.State == SessionStateOpen
		lastActivity := session.lastActivity
		session.mu.RUnlock()

		if isOpen && now.Sub(lastActivity) > sm.config.SessionTimeout {
			session.mu.Lock()
			session.State = SessionStateClosed
			session.mu.Unlock()
			delete(sm.sessions, id)
		}
	}
}

// Close shuts down the session manager.
func (sm *SessionManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return nil
	}

	sm.closed = true
	// Close all sessions
	for id := range sm.sessions {
		delete(sm.sessions, id)
	}
	return nil
}

// HandleBridgeFrame routes an incoming frame from the SCBridgeClient to the correct session.
func (sm *SessionManager) HandleBridgeFrame(sessionID uint64, data []byte) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	i := uint64(0)
	for _, as := range sm.sessions {
		if i == sessionID {
			select {
			case as.MessageChan <- data:
			default:
			}
			return
		}
		i++
	}
}
// Message represents a P2P protocol message.
type Message struct {
	Type    MessageType
	Payload []byte
	SentAt  time.Time
}

// messageTypeCode maps MessageType to a byte code for marshaling.
var messageTypeCode = map[MessageType]byte{
	MessageTypeData:      0x01,
	MessageTypeControl:   0x02,
	MessageTypeHeartbeat: 0x03,
	MessageTypeError:     0x04,
}

// codeToMessageType maps byte code back to MessageType.
var codeToMessageType = map[byte]MessageType{
	0x01: MessageTypeData,
	0x02: MessageTypeControl,
	0x03: MessageTypeHeartbeat,
	0x04: MessageTypeError,
}

// Marshal serializes a message.
func (m *Message) Marshal() ([]byte, error) {
	// Simple length-prefixed encoding: [type byte][4 bytes length][payload]
	// For production, use protobuf
	code, ok := messageTypeCode[m.Type]
	if !ok {
		code = 0x00
	}
	data := make([]byte, 1+4+len(m.Payload))
	data[0] = code
	PutUint32BE(data[1:5], uint32(len(m.Payload)))
	copy(data[5:], m.Payload)
	return data, nil
}

// PutUint32BE writes a uint32 in big-endian format.
func PutUint32BE(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

// GetUint32BE reads a uint32 in big-endian format.
func GetUint32BE(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}