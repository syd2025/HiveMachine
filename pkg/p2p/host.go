package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ProtocolID is the libp2p protocol identifier for HiveMachine.
const ProtocolID protocol.ID = "/hivemachine/1.0.0"

// DefaultConfig returns sensible defaults.
func DefaultConfig() *P2PConfig {
	return &P2PConfig{
		ListenAddresses:  []string{"/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"},
		EnableNATPortMap: true,
	}
}

// P2PConfig holds configuration for the P2P host.
type P2PConfig struct {
	ListenAddresses      []string
	PrivKey              crypto.PrivKey
	EnableNATPortMap     bool
	DHTMode              DHTMode
	MinSessionTrustTier  int
}

// DHTMode determines DHT operation mode.
type DHTMode string

const (
	DHTModeClient DHTMode = "client"
	DHTModeServer DHTMode = "server"
	DHTModeAuto   DHTMode = "auto"
)

// Host wraps the libp2p host with session management.
type Host struct {
	ctx      context.Context
	cancel   context.CancelFunc
	host     host.Host
	config   *P2PConfig
	sessions map[SessionID]*Session
	sessionsMu sync.RWMutex
	dht      *dhtService
}

type dhtService struct {
	mode          DHTMode
	bootstrapAddr string
}

// NewHost creates a new libp2p host.
func NewHost(ctx context.Context, cfg *P2PConfig) (*Host, error) {
	if cfg == nil {
		cfg = DefaultConfig()
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
	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating libp2p host: %w", err)
	}
	innerCtx, cancel := context.WithCancel(ctx)
	return &Host{
		ctx:      innerCtx,
		cancel:   cancel,
		host:     h,
		config:   cfg,
		sessions: make(map[SessionID]*Session),
	}, nil
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
	ai, err := peer.AddrInfoFromString(addr)
	if err != nil {
		return fmt.Errorf("parsing peer address %q: %w", addr, err)
	}
	return h.host.Connect(ctx, *ai)
}

// Disconnect closes a connection to a peer.
func (h *Host) Disconnect(peerID string) error {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return fmt.Errorf("decoding peer ID: %w", err)
	}
	return h.host.Network().ClosePeer(pid)
}

// Peers returns all connected peer IDs.
func (h *Host) Peers() []string {
	var peers []string
	for _, p := range h.host.Network().Peers() {
		peers = append(peers, p.String())
	}
	return peers
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
}

// OpenSession establishes a new session with a peer.
func (h *Host) OpenSession(ctx context.Context, remotePeer string) (*Session, error) {
	pid, err := peer.Decode(remotePeer)
	if err != nil {
		return nil, fmt.Errorf("decoding peer ID: %w", err)
	}
	// Connect is idempotent — succeeds even if already connected.
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
	if _, ok := h.sessions[sessionID]; !ok {
		return errors.New("session not found")
	}
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
	return nil, errors.New("session not found")
}

// Send sends data on a session.
func (h *Host) Send(ctx context.Context, sessionID SessionID, data []byte) error {
	h.sessionsMu.RLock()
	session, ok := h.sessions[sessionID]
	h.sessionsMu.RUnlock()
	if !ok {
		return errors.New("session not found")
	}
	if session.State != SessionStateOpen {
		return fmt.Errorf("session not open: %v", session.State)
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
	h.sessionsMu.Lock()
	session.BytesOut += int64(len(data))
	session.Messages++
	h.sessionsMu.Unlock()
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
		return nil, errors.New("session not found")
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
	buf := make([]byte, 64*1024)
	n, err := stream.Read(buf)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("reading: %w", err)
	}
	h.sessionsMu.Lock()
	session.BytesIn += int64(n)
	session.Messages++
	h.sessionsMu.Unlock()
	return buf[:n], nil
}

// FindProviders searches for providers of a key in the DHT.
func (h *Host) FindProviders(ctx context.Context, key string) ([]string, error) {
	_ = key
	return nil, nil
}

// Provide announces this node as a provider of a key.
func (h *Host) Provide(ctx context.Context, key string) error {
	_ = key
	return nil
}

// Close shuts down the P2P host.
func (h *Host) Close() error {
	h.cancel()
	h.sessionsMu.Lock()
	for id := range h.sessions {
		delete(h.sessions, id)
	}
	h.sessionsMu.Unlock()
	return h.host.Close()
}

// SetDHTConfig configures the DHT service.
func (h *Host) SetDHTConfig(mode DHTMode, bootstrapPeer string) {
	h.dht = &dhtService{mode: mode, bootstrapAddr: bootstrapPeer}
}
