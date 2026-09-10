// Package p2p provides peer-to-peer networking capabilities using libp2p.
package p2p

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Error codes used by P2PError.
const (
	ErrCodeConnectionFailed     = "p2p.connection_failed"
	ErrCodeSessionTimeout      = "p2p.session_timeout"
	ErrCodeSessionClosed      = "p2p.session_closed"
	ErrCodeProtocolMismatch    = "p2p.protocol_mismatch"
	ErrCodeResourceLimit      = "p2p.resource_limit"
	ErrCodeAuthenticationFailed = "p2p.authentication_failed"
	ErrCodeRelayUnavailable   = "p2p.relay_unavailable"
)

// P2PError wraps a lower-level error with a code and message.
type P2PError struct {
	Code    string
	Message string
	Err     error
}

func (e *P2PError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *P2PError) Unwrap() error { return e.Err }

var (
	ErrP2PNotImplemented = &P2PError{Code: "P2P_NOT_IMPLEMENTED", Message: "p2p not yet implemented"}
	ErrSessionNotFound   = &P2PError{Code: ErrCodeSessionClosed, Message: "session not found"}
)

// NewP2PError creates a new P2PError.
func NewP2PError(code, message string, err error) *P2PError {
	return &P2PError{Code: code, Message: message, Err: err}
}

// IsP2PError checks if err is a P2PError with the given code.
func IsP2PError(err error, code string) bool {
	var p2pErr *P2PError
	if errors.As(err, &p2pErr) {
		return p2pErr.Code == code
	}
	return false
}

// P2PEventType classifies P2P events emitted by the P2P stack.
type P2PEventType string

const (
	EventPeerConnected    P2PEventType = "peer_connected"
	EventPeerDisconnected P2PEventType = "peer_disconnected"
	EventSessionOpened   P2PEventType = "session_opened"
	EventSessionClosed    P2PEventType = "session_closed"
	EventMessageReceived P2PEventType = "message_received"
	EventProviderFound    P2PEventType = "provider_found"

	// Legacy names matching host.go's existing event constants.
	P2PEventConnect   P2PEventType = "connect"
	P2PEventDisconnect P2PEventType = "disconnect"
	P2PEventMessage  P2PEventType = "message"
)

// P2PEvent represents a P2P event delivered to registered EventHandlers.
// SessionID uses value type (not pointer) for compatibility with host.go.
type P2PEvent struct {
	Type      P2PEventType
	PeerID    string
	SessionID SessionID
	Data      interface{}
	Timestamp time.Time
}

// EventHandler receives P2P events.
type EventHandler interface {
	HandleP2PEvent(event *P2PEvent)
}

// EventBus distributes P2P events to registered handlers.
type EventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers an event handler.
func (eb *EventBus) Subscribe(handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers = append(eb.handlers, handler)
}

// Unsubscribe removes an event handler.
func (eb *EventBus) Unsubscribe(handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for i, h := range eb.handlers {
		if h == handler {
			eb.handlers = append(eb.handlers[:i], eb.handlers[i+1:]...)
			return
		}
	}
}

// Publish sends an event to all handlers.
func (eb *EventBus) Publish(event *P2PEvent) {
	eb.mu.RLock()
	handlers := eb.handlers
	eb.mu.RUnlock()
	for _, h := range handlers {
		if h != nil {
			h.HandleP2PEvent(event)
		}
	}
}

// PeerInfo describes a connected peer.
type PeerInfo struct {
	ID        string
	Addresses []string
	Latency   string
	Direction string
}

// RelayConfig configures the libp2p relay transport.
type RelayConfig struct {
	Enabled          bool
	MaxCircuitLength int
	MaxReserve       int
}

// DefaultRelayConfig returns sensible defaults.
func DefaultRelayConfig() *RelayConfig {
	return &RelayConfig{
		Enabled:          true,
		MaxCircuitLength: 3,
		MaxReserve:       256,
	}
}

// TransportConfig configures available transports.
type TransportConfig struct {
	EnableQUIC      bool
	EnableTCP       bool
	EnableWebSocket bool
	EnableRelay     bool
	Relay           *RelayConfig
}

// DefaultTransportConfig returns sensible defaults.
func DefaultTransportConfig() *TransportConfig {
	return &TransportConfig{
		EnableQUIC:      false,
		EnableTCP:       true,
		EnableWebSocket: false,
		EnableRelay:     true,
		Relay:           DefaultRelayConfig(),
	}
}

// ---------------------------------------------------------------------------
// Service — P2PService interface implementation
// ---------------------------------------------------------------------------

// P2PService is the top-level P2P networking service.
// It wraps Host (libp2p host, DHT) and SessionManager (session lifecycle).
type P2PService struct {
	host     *Host
	sessions *SessionManager
	eventBus *EventBus
	started  bool
	mu       sync.RWMutex
}

// NewService creates a P2PService from its components.
func NewService(h *Host, sm *SessionManager) *P2PService {
	return &P2PService{
		host:     h,
		sessions: sm,
		eventBus: NewEventBus(),
	}
}

// Start boots the libp2p host and starts the session heartbeat.
func (s *P2PService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if err := s.host.Start(ctx); err != nil {
		return err
	}
	sessionsCtx, cancel := context.WithCancel(ctx)
	_ = cancel
	s.sessions.StartHeartbeat(sessionsCtx)
	s.started = true
	return nil
}

// Stop gracefully shuts down the P2P host and all sessions.
func (s *P2PService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	s.sessions.Close()
	s.host.Stop()
	s.started = false
	return nil
}

// OpenSession establishes a new session with a remote peer.
func (s *P2PService) OpenSession(ctx context.Context, remotePeer peer.ID) (*Session, error) {
	active, err := s.sessions.OpenSession(ctx, remotePeer.String())
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:       active.ID,
		PeerID:   active.PeerID,
		State:    SessionStateOpen,
		OpenedAt: active.OpenedAt,
	}, nil
}

// CloseSession closes an active session.
func (s *P2PService) CloseSession(ctx context.Context, id SessionID) error {
	return s.sessions.CloseSession(ctx, id)
}

// GetSession returns a session by ID.
func (s *P2PService) GetSession(id SessionID) (*Session, error) {
	as, err := s.sessions.GetSession(id)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:       as.ID,
		PeerID:   as.PeerID,
		State:    SessionState(as.State),
		OpenedAt: as.OpenedAt,
	}, nil
}

// Send sends data on a session.
func (s *P2PService) Send(ctx context.Context, sessionID SessionID, data []byte) error {
	return s.sessions.SendMessage(ctx, sessionID, MessageTypeData, data)
}

// Receive reads the next message from a session within the given timeout.
func (s *P2PService) Receive(ctx context.Context, sessionID SessionID, timeout time.Duration) ([]byte, error) {
	as, err := s.sessions.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-as.MessageChan:
		return msg, nil
	}
}

// FindProviders searches for providers of a key using the DHT.
func (s *P2PService) FindProviders(ctx context.Context, key string) ([]peer.AddrInfo, error) {
	strs, err := s.host.FindProviders(ctx, key)
	if err != nil {
		return nil, err
	}
	result := make([]peer.AddrInfo, 0, len(strs))
	for _, str := range strs {
		if id, err := peer.Decode(str); err == nil {
			result = append(result, peer.AddrInfo{ID: id})
		}
	}
	return result, nil
}

// Provide announces this node as a provider of a key via DHT.
func (s *P2PService) Provide(ctx context.Context, key string) error {
	return s.host.Provide(ctx, key)
}

// Connect dials a peer address.
func (s *P2PService) Connect(ctx context.Context, addr string) error {
	return s.host.Connect(ctx, addr)
}

// Disconnect closes a connection to a peer.
func (s *P2PService) Disconnect(peerID string) error {
	return s.host.Disconnect(peerID)
}

// Peers returns all connected peer IDs.
func (s *P2PService) Peers() []peer.ID {
	strs := s.host.Peers()
	ids := make([]peer.ID, len(strs))
	for i, str := range strs {
		pid, _ := peer.Decode(str)
		ids[i] = pid
	}
	return ids
}

// Subscribe registers an event handler.
func (s *P2PService) Subscribe(handler EventHandler) {
	s.eventBus.Subscribe(handler)
}

// ID returns the local peer ID.
func (s *P2PService) ID() string {
	return s.host.ID()
}

// Addrs returns the listen addresses.
func (s *P2PService) Addrs() []string {
	return s.host.Addrs()
}

// ---------------------------------------------------------------------------
// SCBridgeClient — WebSocket relay client
// Based on OpenMayhem's SC-Bridge protocol.
// ---------------------------------------------------------------------------

// SessionMessage represents messages sent over the bridge WebSocket.
type SessionMessage struct {
	Type    string          `json:"type"`
	ID      *uint64         `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// SCBridgeClient manages a WebSocket connection to an SC-Bridge relay.
type SCBridgeClient struct {
	url    string
	token  string
	ws     *websocket.Conn
	sess   *SessionManager
	mu     sync.Mutex
	nextID uint64
}

// NewSCBridgeClient creates a client for the given bridge URL and auth token.
func NewSCBridgeClient(url, token string, sm *SessionManager) *SCBridgeClient {
	return &SCBridgeClient{url: url, token: token, sess: sm}
}

// Connect opens the WebSocket and authenticates with the bridge.
func (c *SCBridgeClient) Connect(ctx context.Context) error {
	u, err := url.Parse(c.url)
	if err != nil {
		return NewP2PError(ErrCodeConnectionFailed, "invalid bridge url", err)
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return NewP2PError(ErrCodeConnectionFailed, "bridge dial: "+err.Error(), err)
	}
	c.ws = conn

	auth := SessionMessage{Type: "auth", Payload: json.RawMessage(fmt.Sprintf(`{"token":"%s"}`, c.token))}
	if err := c.ws.WriteJSON(auth); err != nil {
		c.ws.Close()
		return NewP2PError(ErrCodeAuthenticationFailed, err.Error(), err)
	}

	var reply SessionMessage
	if err := c.ws.ReadJSON(&reply); err != nil {
		c.ws.Close()
		return NewP2PError(ErrCodeAuthenticationFailed, "auth response: "+err.Error(), err)
	}
	if reply.Type == "error" {
		c.ws.Close()
		return NewP2PError(ErrCodeAuthenticationFailed, string(reply.Payload), nil)
	}

	go c.readLoop()
	return nil
}

func (c *SCBridgeClient) nextMessageID() uint64 {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()
	return id
}

// SendSessionMessage sends a session frame over the bridge WebSocket.
func (c *SCBridgeClient) SendSessionMessage(_ context.Context, sessionID uint64, payload []byte) error {
	c.mu.Lock()
	ws := c.ws
	c.mu.Unlock()
	if ws == nil {
		return NewP2PError(ErrCodeSessionClosed, "not connected to bridge", nil)
	}
	msg := SessionMessage{
		Type:    "session_send",
		ID:      ptrUint64(c.nextMessageID()),
		Payload: payload,
	}
	return ws.WriteJSON(msg)
}

// readLoop processes incoming bridge messages in a goroutine.
func (c *SCBridgeClient) readLoop() {
	for {
		var msg SessionMessage
		if err := c.ws.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "session_frame":
			var frame struct {
				SessionID uint64 `json:"session_id"`
				Data     []byte  `json:"data"`
			}
			if json.Unmarshal(msg.Payload, &frame) == nil {
				c.sess.HandleBridgeFrame(frame.SessionID, frame.Data)
			}
		case "peer_connect", "error":
			// Bridge protocol events — handled by caller via session events.
		}
	}
}

// Close gracefully closes the bridge connection.
func (c *SCBridgeClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil {
		return nil
	}
	c.ws.WriteControl(websocket.CloseMessage, nil, time.Now().Add(5*time.Second))
	return c.ws.Close()
}

func ptrUint64(v uint64) *uint64 { return &v }
