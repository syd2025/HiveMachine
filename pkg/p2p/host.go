// Package p2p provides peer-to-peer networking capabilities using libp2p.
package p2p

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	dht "github.com/libp2p/go-libp2p-kad-dht"
)

// ProtocolID is the libp2p protocol identifier for HiveMachine.
const ProtocolID protocol.ID = "/hivemachine/1.0.0"

// DefaultConfig returns sensible defaults.
func DefaultConfig() *P2PConfig {
	return &P2PConfig{
		ListenAddresses:      []string{"/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"},
		EnableNATPortMap:     true,
		EnableRelay:          true,
		DHTMode:              DHTModeAuto,
		MinSessionTrustTier:  0,
		ConnectionTimeout:    30 * time.Second,
		MaxMessageSize:       64 * 1024 * 1024, // 64MB
	}
}

// P2PConfig holds configuration for the P2P host.
type P2PConfig struct {
	ListenAddresses      []string
	PrivKey              crypto.PrivKey
	EnableNATPortMap     bool
	EnableRelay          bool
	DHTMode              DHTMode
	MinSessionTrustTier  int
	ConnectionTimeout    time.Duration
	MaxMessageSize       int64
	BootstrapPeers       []string
}

// DHTMode determines DHT operation mode.
type DHTMode string

const (
	DHTModeClient DHTMode = "client"
	DHTModeServer DHTMode = "server"
	DHTModeAuto   DHTMode = "auto"
)

// P2PHost interface defines the P2P host operations.
type P2PHost interface {
	// Start starts the P2P host and begins listening.
	Start(ctx context.Context) error

	// Stop stops the P2P host gracefully.
	Stop() error

	// Connect establishes a connection to a peer.
	Connect(ctx context.Context, addr string) error

	// Disconnect closes a connection to a peer.
	Disconnect(peerID string) error

	// ID returns the local peer ID.
	ID() string

	// Addrs returns the listen addresses.
	Addrs() []string

	// Peers returns all connected peer IDs.
	Peers() []string

	// FindProviders searches for providers of a key.
	FindProviders(ctx context.Context, key string) ([]string, error)

	// Provide announces this node as a provider of a key.
	Provide(ctx context.Context, key string) error

	// OpenSession establishes a new session with a peer.
	OpenSession(ctx context.Context, remotePeer string) (*Session, error)

	// CloseSession closes an existing session.
	CloseSession(ctx context.Context, sessionID SessionID) error

	// GetSession retrieves a session by ID.
	GetSession(sessionID SessionID) (*Session, error)

	// Send sends data on a session.
	Send(ctx context.Context, sessionID SessionID, data []byte) error

	// Receive reads data from a session.
	Receive(ctx context.Context, sessionID SessionID, timeout time.Duration) ([]byte, error)
}

// Host wraps the libp2p host with session management and DHT support.
type Host struct {
	ctx       context.Context
	cancel    context.CancelFunc
	host      host.Host
	config    *P2PConfig
	sessions  map[SessionID]*Session
	sessionsMu sync.RWMutex
	discovery *discovery
	eventBus  *EventBus
	started   bool
	mu        sync.RWMutex
}

// NewHost creates a new libp2p host with the given configuration.
func NewHost(ctx context.Context, cfg *P2PConfig) (*Host, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.ConnectionTimeout == 0 {
		cfg.ConnectionTimeout = 30 * time.Second
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(cfg.ListenAddresses...),
	}

	if cfg.EnableNATPortMap {
		opts = append(opts, libp2p.NATPortMap())
	}

	if cfg.PrivKey != nil {
		opts = append(opts, libp2p.Identity(cfg.PrivKey))
	}

	// Enable relay for NAT traversal
	if cfg.EnableRelay {
		opts = append(opts, libp2p.EnableRelay())
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating libp2p host: %w", err)
	}

	innerCtx, cancel := context.WithCancel(ctx)

	host := &Host{
		ctx:       innerCtx,
		cancel:    cancel,
		host:      h,
		config:    cfg,
		sessions:  make(map[SessionID]*Session),
		eventBus:  NewEventBus(),
	}

	// Initialize DHT discovery
	host.discovery, err = newDiscovery(innerCtx, h, cfg.DHTMode, cfg.BootstrapPeers)
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("creating discovery service: %w", err)
	}

	return host, nil
}

// Start starts the P2P host, bootstraps DHT, and begins listening.
func (h *Host) Start(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.started {
		return fmt.Errorf("host already started")
	}

	// Bootstrap DHT if configured
	if h.discovery != nil && h.discovery.dht != nil {
		if err := h.bootstrapDHT(ctx); err != nil {
			// Non-fatal, log and continue
			fmt.Printf("DHT bootstrap warning: %v\n", err)
		}
	}

	h.started = true
	return nil
}

// bootstrapDHT bootstraps the DHT with configured bootstrap peers.
func (h *Host) bootstrapDHT(ctx context.Context) error {
	if h.discovery == nil || h.discovery.dht == nil {
		return nil
	}

	bootstrapAddrs := h.config.BootstrapPeers
	if len(bootstrapAddrs) == 0 {
		// Use default IPFS bootstrap peers
		for _, addr := range dht.DefaultBootstrapPeers {
			bootstrapAddrs = append(bootstrapAddrs, addr.String())
		}
	}

	for _, addrStr := range bootstrapAddrs {
		addrInfo, err := peer.AddrInfoFromString(addrStr)
		if err != nil {
			continue
		}
		if err := h.host.Connect(ctx, *addrInfo); err != nil {
			continue
		}
		break
	}

	return nil
}

// Stop gracefully stops the P2P host.
func (h *Host) Stop() error {
	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	h.cancel()

	// Close all sessions
	h.sessionsMu.Lock()
	for id := range h.sessions {
		delete(h.sessions, id)
	}
	h.sessionsMu.Unlock()

	// Close discovery
	if h.discovery != nil {
		h.discovery.Close()
	}

	return h.host.Close()
}

// ID returns the libp2p peer ID.
func (h *Host) ID() string {
	return h.host.ID().String()
}

// Addrs returns the host's listen addresses.
func (h *Host) Addrs() []string {
	var addrs []string
	for _, a := range h.host.Addrs() {
		addrs = append(addrs, a.String())
	}
	return addrs
}

// Connect establishes a connection to a peer.
func (h *Host) Connect(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, h.config.ConnectionTimeout)
	defer cancel()

	ai, err := peer.AddrInfoFromString(addr)
	if err != nil {
		return fmt.Errorf("parsing peer address %q: %w", addr, err)
	}

	if err := h.host.Connect(ctx, *ai); err != nil {
		return fmt.Errorf("connecting to peer: %w", err)
	}

	h.eventBus.Publish(&P2PEvent{
		Type:   P2PEventConnect,
		PeerID: ai.ID.String(),
	})

	return nil
}

// Disconnect closes a connection to a peer.
func (h *Host) Disconnect(peerID string) error {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("decoding peer ID: %w", err)
	}

	if err := h.host.Network().ClosePeer(pid); err != nil {
		return fmt.Errorf("closing connection: %w", err)
	}

	h.eventBus.Publish(&P2PEvent{
		Type:   P2PEventDisconnect,
		PeerID: peerID,
	})

	return nil
}

// Peers returns all connected peer IDs.
func (h *Host) Peers() []string {
	var peers []string
	for _, p := range h.host.Network().Peers() {
		peers = append(peers, p.String())
	}
	return peers
}

// FindProviders searches for providers of a key in the DHT.
func (h *Host) FindProviders(ctx context.Context, key string) ([]string, error) {
	providers, err := h.discovery.FindProviders(ctx, key)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(providers))
	for _, p := range providers {
		result = append(result, p.ID.String())
	}
	return result, nil
}

// Provide announces this node as a provider of a key.
func (h *Host) Provide(ctx context.Context, key string) error {
	return h.discovery.Provide(ctx, key)
}

// SessionID uniquely identifies a P2P session.
type SessionID string

// SessionState represents the lifecycle state of a session.
type SessionState int

const (
	SessionStateInitial SessionState = iota
	SessionStateConnecting
	SessionStateOpen
	SessionStateClosing
	SessionStateClosed
)

func (s SessionState) String() string {
	switch s {
	case SessionStateInitial:
		return "initial"
	case SessionStateConnecting:
		return "connecting"
	case SessionStateOpen:
		return "open"
	case SessionStateClosing:
		return "closing"
	case SessionStateClosed:
		return "closed"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// Session represents an established P2P session.
type Session struct {
	ID       SessionID
	PeerID   string
	State    SessionState
	OpenedAt time.Time
	Messages int
	BytesIn  int64
	BytesOut int64
	mu       sync.RWMutex
}

// OpenSession establishes a new session with a peer.
func (h *Host) OpenSession(ctx context.Context, remotePeer string) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, h.config.ConnectionTimeout)
	defer cancel()

	pid, err := peer.Decode(remotePeer)
	if err != nil {
		return nil, fmt.Errorf("decoding peer ID: %w", err)
	}

	// Connect is idempotent — succeeds even if already connected
	if err := h.host.Connect(ctx, peer.AddrInfo{ID: pid}); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", remotePeer, err)
	}

	sid := SessionID(fmt.Sprintf("%s-%d", remotePeer, time.Now().UnixNano()))
	session := &Session{
		ID:       sid,
		PeerID:   remotePeer,
		State:    SessionStateOpen,
		OpenedAt: time.Now(),
	}

	h.sessionsMu.Lock()
	h.sessions[sid] = session
	h.sessionsMu.Unlock()

	return session, nil
}

// CloseSession closes an existing session.
func (h *Host) CloseSession(ctx context.Context, sessionID SessionID) error {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	session, ok := h.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.Lock()
	session.State = SessionStateClosing
	session.mu.Unlock()

	delete(h.sessions, sessionID)
	return nil
}

// GetSession retrieves a session by ID.
func (h *Host) GetSession(sessionID SessionID) (*Session, error) {
	h.sessionsMu.RLock()
	defer h.sessionsMu.RUnlock()

	if session, ok := h.sessions[sessionID]; ok {
		return session, nil
	}
	return nil, fmt.Errorf("session not found")
}

// Send sends data on a session.
func (h *Host) Send(ctx context.Context, sessionID SessionID, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, h.config.ConnectionTimeout)
	defer cancel()

	h.sessionsMu.RLock()
	session, ok := h.sessions[sessionID]
	h.sessionsMu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	session.mu.RLock()
	state := session.State
	session.mu.RUnlock()

	if state != SessionStateOpen {
		return fmt.Errorf("session not open: %v", state)
	}

	pid, err := peer.Decode(session.PeerID)
	if err != nil {
		return err
	}

	stream, err := h.host.NewStream(ctx, pid, ProtocolID)
	if err != nil {
		return fmt.Errorf("opening stream: %w", err)
	}
	defer stream.Close()

	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("writing: %w", err)
	}

	session.mu.Lock()
	session.BytesOut += int64(len(data))
	session.Messages++
	session.mu.Unlock()

	h.eventBus.Publish(&P2PEvent{
		Type:      P2PEventMessage,
		SessionID: sessionID,
		PeerID:    session.PeerID,
		Data:      data,
	})

	return nil
}

// Receive reads data from a session.
func (h *Host) Receive(ctx context.Context, sessionID SessionID, timeout time.Duration) ([]byte, error) {
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	h.sessionsMu.RLock()
	session, ok := h.sessions[sessionID]
	h.sessionsMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	pid, err := peer.Decode(session.PeerID)
	if err != nil {
		return nil, err
	}

	stream, err := h.host.NewStream(readCtx, pid, ProtocolID)
	if err != nil {
		return nil, fmt.Errorf("opening stream: %w", err)
	}
	defer stream.Close()

	buf := make([]byte, h.config.MaxMessageSize)
	n, err := stream.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return nil, fmt.Errorf("reading: %w", err)
	}

	session.mu.Lock()
	session.BytesIn += int64(n)
	session.Messages++
	session.mu.Unlock()

	data := buf[:n]
	h.eventBus.Publish(&P2PEvent{
		Type:      P2PEventMessage,
		SessionID: sessionID,
		PeerID:    session.PeerID,
		Data:      data,
	})

	return data, nil
}

// Close shuts down the P2P host.
func (h *Host) Close() error {
	return h.Stop()
}

// SetDHTConfig configures the DHT service.
func (h *Host) SetDHTConfig(mode DHTMode, bootstrapPeer string) {
	if h.discovery != nil {
		h.discovery.setConfig(mode, bootstrapPeer)
	}
}

// Subscribe to P2P events.
func (h *Host) Subscribe(handler EventHandler) {
	h.eventBus.Subscribe(handler)
}

// Publish sends an event to all handlers.
func (h *Host) Publish(event *P2PEvent) {
	h.eventBus.Publish(event)
}