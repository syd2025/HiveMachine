// Package p2p provides peer-to-peer networking capabilities.
// This is a stub — full P2P implementation is deferred until trust tier system is designed.
package p2p

import "sync"

// P2PError represents P2P-related errors (stub).
type P2PError struct {
	Code    string
	Message string
	Err     error
}

func (e *P2PError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *P2PError) Unwrap() error { return e.Err }

// Error codes (stub).
var (
	ErrP2PNotImplemented = &P2PError{Code: "P2P_NOT_IMPLEMENTED", Message: "p2p not yet implemented"}
	ErrSessionNotFound   = &P2PError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
)

// P2PEventType for event callbacks (stub type).
type P2PEventType string

const (
	P2PEventConnect    P2PEventType = "connect"
	P2PEventDisconnect P2PEventType = "disconnect"
	P2PEventMessage    P2PEventType = "message"
	P2PEventError      P2PEventType = "error"
)

// P2PEvent for event callbacks (stub).
type P2PEvent struct {
	Type      P2PEventType
	SessionID SessionID
	PeerID    string
	Data      []byte
}

// EventHandler handles P2P events (stub interface).
type EventHandler interface {
	HandleP2PEvent(event *P2PEvent)
}

// EventBus for pub/sub of P2P events.
type EventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe adds an event handler.
func (eb *EventBus) Subscribe(handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.handlers = append(eb.handlers, handler)
}

// Publish sends an event to all handlers, skipping any nil entries.
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
