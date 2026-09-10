package p2p

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionState(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{SessionStateInitial, "initial"},
		{SessionStateConnecting, "connecting"},
		{SessionStateOpen, "open"},
		{SessionStateClosing, "closing"},
		{SessionStateClosed, "closed"},
		{SessionState(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.state.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)
	assert.NotEmpty(t, cfg.ListenAddresses)
	assert.True(t, cfg.EnableNATPortMap)
	assert.True(t, cfg.EnableRelay)
	assert.Equal(t, DHTModeAuto, cfg.DHTMode)
	assert.Equal(t, 30*time.Second, cfg.ConnectionTimeout)
	assert.Equal(t, int64(64*1024*1024), cfg.MaxMessageSize)
}

func TestNewHost(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, h)
	defer h.Close()

	assert.NotEmpty(t, h.ID())
	assert.NotNil(t, h.host)
}

func TestHostListenAddresses(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	addrs := h.Addrs()
	assert.NotEmpty(t, addrs)
}

func TestHostPeers(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	// No peers initially
	peers := h.Peers()
	assert.Empty(t, peers)
}

func TestSessionManagement(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	// Close non-existent session
	err = h.CloseSession(ctx, SessionID("nonexistent"))
	assert.Error(t, err)

	// GetSession on non-existent
	_, err = h.GetSession(SessionID("nonexistent"))
	assert.Error(t, err)
}

func TestHostDisconnectNoPeer(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	err = h.Disconnect("QmNonexistentPeerID")
	assert.Error(t, err)
}

func TestProtocolID(t *testing.T) {
	assert.Equal(t, string(ProtocolID), "/hivemachine/1.0.0")
}

func TestDHTMode(t *testing.T) {
	assert.Equal(t, string(DHTModeClient), "client")
	assert.Equal(t, string(DHTModeServer), "server")
	assert.Equal(t, string(DHTModeAuto), "auto")
}

func TestHostStartStop(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)

	// Start should work (already started in NewHost, but idempotent in practice)
	err = h.Start(ctx)
	assert.NoError(t, err)

	// Stop
	err = h.Stop()
	assert.NoError(t, err)

	// Second stop should be no-op
	err = h.Stop()
	assert.NoError(t, err)
}

func TestHostFindProviders(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	providers, err := h.FindProviders(ctx, "test-key")
	assert.NoError(t, err)
	// Empty list is OK when no providers exist
	assert.NotNil(t, providers)
	assert.Empty(t, providers)
}

func TestHostProvide(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	err = h.Provide(ctx, "test-key")
	assert.NoError(t, err)
}

func TestHostFindProvidersAfterProvide(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	// Provide a key
	err = h.Provide(ctx, "find-test-key")
	require.NoError(t, err)

	// Find providers - should find ourselves
	providers, err := h.FindProviders(ctx, "find-test-key")
	assert.NoError(t, err)
	assert.NotEmpty(t, providers)
}

func TestEventBus(t *testing.T) {
	bus := NewEventBus()
	require.NotNil(t, bus)

	var receivedEvent *P2PEvent
	handler := &testEventHandler{
		onEvent: func(e *P2PEvent) {
			receivedEvent = e
		},
	}

	bus.Subscribe(handler)
	bus.Publish(&P2PEvent{
		Type:   P2PEventConnect,
		PeerID: "test-peer",
	})

	assert.NotNil(t, receivedEvent)
	assert.Equal(t, P2PEventConnect, receivedEvent.Type)
	assert.Equal(t, "test-peer", receivedEvent.PeerID)
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	require.NotNil(t, bus)

	var callCount int
	handler := &testEventHandler{
		onEvent: func(e *P2PEvent) {
			callCount++
		},
	}

	bus.Subscribe(handler)
	bus.Publish(&P2PEvent{Type: P2PEventConnect})
	assert.Equal(t, 1, callCount)

	bus.Unsubscribe(handler)
	bus.Publish(&P2PEvent{Type: P2PEventDisconnect})
	assert.Equal(t, 1, callCount) // Should not have increased
}

func TestEventBusMultipleHandlers(t *testing.T) {
	bus := NewEventBus()

	var count1, count2 int
	handler1 := &testEventHandler{
		onEvent: func(e *P2PEvent) { count1++ },
	}
	handler2 := &testEventHandler{
		onEvent: func(e *P2PEvent) { count2++ },
	}

	bus.Subscribe(handler1)
	bus.Subscribe(handler2)
	bus.Publish(&P2PEvent{Type: P2PEventConnect})

	assert.Equal(t, 1, count1)
	assert.Equal(t, 1, count2)
}

func TestP2PError(t *testing.T) {
	baseErr := fmt.Errorf("underlying error")
	err := NewP2PError("TEST_CODE", "test message", baseErr)

	assert.Contains(t, err.Error(), "TEST_CODE: test message:")
	assert.Equal(t, baseErr, err.Unwrap())
	assert.True(t, IsP2PError(err, "TEST_CODE"))
	assert.False(t, IsP2PError(err, "OTHER_CODE"))
	assert.False(t, IsP2PError(fmt.Errorf("other"), "TEST_CODE"))
}

func TestDefaultRelayConfig(t *testing.T) {
	cfg := DefaultRelayConfig()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 3, cfg.MaxCircuitLength)
	assert.Equal(t, 256, cfg.MaxReserve)
}

func TestDefaultTransportConfig(t *testing.T) {
	cfg := DefaultTransportConfig()
	require.NotNil(t, cfg)
	assert.True(t, cfg.EnableTCP)
	assert.False(t, cfg.EnableQUIC)
	assert.False(t, cfg.EnableWebSocket)
	assert.True(t, cfg.EnableRelay)
	require.NotNil(t, cfg.Relay)
}

func TestDefaultSessionManagerConfig(t *testing.T) {
	cfg := DefaultSessionManagerConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, 30*time.Second, cfg.HeartbeatInterval)
	assert.Equal(t, 5*time.Minute, cfg.SessionTimeout)
	assert.Equal(t, int64(64*1024*1024), cfg.MaxMessageSize)
}

func TestSessionManagerWithHost(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	sm, err := NewSessionManager(h, nil)
	require.NoError(t, err)
	require.NotNil(t, sm)
	defer sm.Close()

	// ListSessions should be empty
	sessions := sm.ListSessions()
	assert.Empty(t, sessions)

	// GetSession on non-existent
	_, err = sm.GetSession(SessionID("nonexistent"))
	assert.Error(t, err)
}

func TestActiveSessionMetadata(t *testing.T) {
	session := &ActiveSession{
		ID:     "test-session",
		PeerID: "test-peer",
	}

	session.SetMetadata("key1", "value1")
	session.SetMetadata("key2", 42)

	val1, ok := session.GetMetadata("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val1)

	val2, ok := session.GetMetadata("key2")
	assert.True(t, ok)
	assert.Equal(t, 42, val2)

	_, ok = session.GetMetadata("nonexistent")
	assert.False(t, ok)
}

func TestMessageMarshaling(t *testing.T) {
	msg := &Message{
		Type:    MessageTypeData,
		Payload: []byte("hello world"),
		SentAt:  time.Now(),
	}

	data, err := msg.Marshal()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify length prefix
	length := GetUint32BE(data[1:5])
	assert.Equal(t, uint32(len(msg.Payload)), length)
}

func TestPutUint32BE(t *testing.T) {
	tests := []struct {
		value uint32
		want  []byte
	}{
		{0, []byte{0, 0, 0, 0}},
		{1, []byte{0, 0, 0, 1}},
		{256, []byte{0, 0, 1, 0}},
		{0xFFFFFFFF, []byte{255, 255, 255, 255}},
	}

	for _, tt := range tests {
		b := make([]byte, 4)
		PutUint32BE(b, tt.value)
		assert.Equal(t, tt.want, b)
	}
}

func TestGetUint32BE(t *testing.T) {
	tests := []struct {
		data  []byte
		value uint32
	}{
		{[]byte{0, 0, 0, 0}, 0},
		{[]byte{0, 0, 0, 1}, 1},
		{[]byte{0, 0, 1, 0}, 256},
		{[]byte{255, 255, 255, 255}, 0xFFFFFFFF},
	}

	for _, tt := range tests {
		got := GetUint32BE(tt.data)
		assert.Equal(t, tt.value, got)
	}
}

func TestHostSubscribe(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	var receivedEvent *P2PEvent
	handler := &testEventHandler{
		onEvent: func(e *P2PEvent) {
			receivedEvent = e
		},
	}

	h.Subscribe(handler)

	// Publish an event
	h.Publish(&P2PEvent{
		Type:   P2PEventConnect,
		PeerID: "test-peer",
	})

	assert.NotNil(t, receivedEvent)
	assert.Equal(t, P2PEventConnect, receivedEvent.Type)
}

func TestHostSetDHTConfig(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	require.NoError(t, err)
	defer h.Close()

	// Should not panic
	h.SetDHTConfig(DHTModeServer, "/ip4/127.0.0.1/tcp/5001")
	h.SetDHTConfig(DHTModeClient, "")
}

// testEventHandler is a test implementation of EventHandler.
type testEventHandler struct {
	onEvent func(event *P2PEvent)
}

func (h *testEventHandler) HandleP2PEvent(event *P2PEvent) {
	if h.onEvent != nil {
		h.onEvent(event)
	}
}