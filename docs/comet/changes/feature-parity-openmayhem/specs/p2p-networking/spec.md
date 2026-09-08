# P2P Networking Specification

## Overview

Implement a P2P networking layer using libp2p Go that enables encrypted peer-to-peer communication between HiveMachine nodes for AI inference marketplace operations.

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    HiveMachine Node                          │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ P2P Host    │  │ DHT         │  │ Session Manager     │  │
│  │ (libp2p)    │  │ (Kademlia)  │  │                     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Noise       │  │relay        │  │ Transport           │  │
│  │ (encryption)│  │             │  │ (TCP/WS/Quic)       │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### Session Protocol

Based on OpenMayhem's SC-Bridge protocol:

| Message Type | Direction | Purpose |
|-------------|-----------|---------|
| `auth` | client→bridge | Authenticate with bridge token |
| `subscribe` | bidirectional | Channel subscription |
| `session_open` | bidirectional | Establish P2P session |
| `session_send` | bidirectional | Send frame data |
| `session_close` | bidirectional | Close session |
| `session_frame` | async | Deliver session data |
| `peer_connect` | client→bridge | Connect via relay |

## Data Structures

### Go Structs

```go
// Session represents an established P2P session
type Session struct {
    ID          SessionID
    PeerID      peer.ID
    State       SessionState
    CreatedAt   time.Time
    LastActivity time.Time
}

// SessionState tracks session lifecycle
type SessionState int

const (
    SessionStateOpening SessionState = iota
    SessionStateOpen
    SessionStateClosing
    SessionStateClosed
)

// SCBridgeClient manages bridge communication
type SCBridgeClient struct {
    url        string
    token      string
    wsConn     *websocket.Conn
    sessions   sync.Map
    nextID     uint64
    mu         sync.Mutex
}

// SessionMessage represents P2P protocol messages
type SessionMessage struct {
    Type    string          `json:"type"`
    ID      *uint64         `json:"id,omitempty"`
    Payload json.RawMessage `json:"payload,omitempty"`
}
```

### Configuration

```go
type P2PConfig struct {
    // Network
    ListenAddrs   []string // Multiaddresses to listen on
    BootstrapPeers []string // Bootstrap node addresses
    
    // Protocol
    ProtocolID    string // Application protocol ID
    MaxMessageBytes int64 // Max message size
    
    // Transport
    EnableQUIC    bool
    EnableWebSocket bool
    
    // DHT
    EnableDHT     bool
    DHTMode       DHTMode // Server, Client, Auto
    
    // Relay
    EnableRelay   bool
    MaxRelayedConnections int
    
    // Security
    InsecureMode  bool // For testing only
    
    // Timeouts
    ConnectTimeout time.Duration
    ReadTimeout   time.Duration
    WriteTimeout  time.Duration
}
```

## Session Lifecycle

### State Machine

```
session_open ──► Opening ──► Open ──► Closing ──► Closed
                      │         │
                      └──E──────┘
```

### Session Establishment

1. **Direct Connection**
   ```
   Node A                    Node B
     │──session_open────────►│
     │◄──session_opened──────│
     │───session_send───────►│
     │◄──session_frame───────│
   ```

2. **Relayed Connection**
   ```
   Node A ──peer_connect──► Relay ──session_open──► Node B
        │◄─────────────────────────────│
   ```

## API Interface

```go
type P2PService interface {
    // Lifecycle
    Start(ctx context.Context) error
    Stop() error
    
    // Session management
    OpenSession(ctx context.Context, peer peer.ID) (*Session, error)
    CloseSession(ctx context.Context, id SessionID) error
    GetSession(id SessionID) (*Session, error)
    
    // Messaging
    Send(ctx context.Context, sessionID SessionID, data []byte) error
    Receive(ctx context.Context, sessionID SessionID, timeout time.Duration) ([]byte, error)
    
    // Discovery
    FindProviders(ctx context.Context, key string) ([]peer.AddrInfo, error)
    Provide(ctx context.Context, key string) error
    
    // Peering
    Connect(ctx context.Context, addr multiaddr.Multiaddr) error
    Disconnect(ctx context.Context, p peer.ID) error
    Peers() []peer.ID
}
```

## Events

```go
// P2P events for event-driven architecture
type P2PEventType string

const (
    EventPeerConnected    P2PEventType = "peer_connected"
    EventPeerDisconnected P2PEventType = "peer_disconnected"
    EventSessionOpened    P2PEventType = "session_opened"
    EventSessionClosed    P2PEventType = "session_closed"
    EventMessageReceived  P2PEventType = "message_received"
    EventProviderFound    P2PEventType = "provider_found"
)

type P2PEvent struct {
    Type      P2PEventType
    PeerID    peer.ID
    SessionID *SessionID
    Data      interface{}
    Timestamp time.Time
}
```

## Error Handling

| Error Code | Description |
|------------|-------------|
| `p2p.connection_failed` | Failed to establish connection |
| `p2p.session_timeout` | Session establishment timed out |
| `p2p.session_closed` | Session was closed |
| `p2p.protocol_mismatch` | Protocol version mismatch |
| `p2p.resource_limit` | Resource limit exceeded |
| `p2p.authentication_failed` | Bridge authentication failed |
| `p2p.relay_unavailable` | Relay peer unavailable |

## Security

### Encryption

- All sessions use Noise protocol for encryption
- X25519 key exchange for session keys
- ChaCha20-Poly1305 for symmetric encryption

### Authentication

- Bridge token-based authentication
- Session tokens for inter-node authentication
- Token rotation support

## Acceptance Criteria

- **A1.1**: Node can listen on configured multiaddresses
- **A1.2**: Node can connect to other nodes via direct connection
- **A1.3**: Node can connect to other nodes via relay
- **A1.4**: Sessions establish within 5 seconds on local network
- **A1.5**: Messages are encrypted end-to-end
- **A1.6**: Sessions can send/receive binary data reliably
- **A1.7**: DHT-based provider discovery returns results
- **A1.8**: Session cleanup occurs on connection loss
- **A1.9**: Resource limits are enforced (max messages, max queued bytes)

## Dependencies

```go
// Required Go modules
github.com/libp2p/go-libp2p
github.com/libp2p/go-libp2p-kad-dht
github.com/libp2p/go-libp2p-noise
github.com/libp2p/go-libp2p-relay
github.com/libp2p/go-ws-transport
github.com/libp2p/go-tcp-transport
github.com/libp2p/go-libp2p-quic
```
