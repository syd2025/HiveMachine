package p2p

import (
	"context"
	"testing"
	"time"
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
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if len(cfg.ListenAddresses) == 0 {
		t.Error("ListenAddresses is empty")
	}
	if !cfg.EnableNATPortMap {
		t.Error("EnableNATPortMap should be true by default")
	}
}

func TestNewHost(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	defer h.Close()
	if h.ID() == "" {
		t.Error("Host.ID() is empty")
	}
}

func TestHostListenAddresses(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	defer h.Close()
	addrs := h.Addrs()
	if len(addrs) == 0 {
		t.Error("Host.Addrs() returned empty")
	}
}

func TestSessionManagement(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	defer h.Close()

	// Peers on empty host
	if peers := h.Peers(); len(peers) != 0 {
		t.Errorf("Peers() on empty host = %v, want []", peers)
	}

	// Close a non-existent session
	err = h.CloseSession(ctx, SessionID("nonexistent"))
	if err == nil {
		t.Error("CloseSession(nonexistent) should return error")
	}

	// GetSession on non-existent
	_, err = h.GetSession(SessionID("nonexistent"))
	if err == nil {
		t.Error("GetSession(nonexistent) should return error")
	}
}

func TestSessionSendReceive_NoSession(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	defer h.Close()

	// Send without session
	err = h.Send(ctx, SessionID("nonexistent"), []byte("hello"))
	if err == nil {
		t.Error("Send on nonexistent session should error")
	}

	// Receive without session
	_, err = h.Receive(ctx, SessionID("nonexistent"), time.Second)
	if err == nil {
		t.Error("Receive on nonexistent session should error")
	}
}

func TestHostDisconnect_NoPeer(t *testing.T) {
	ctx := context.Background()
	h, err := NewHost(ctx, nil)
	if err != nil {
		t.Fatalf("NewHost() error = %v", err)
	}
	defer h.Close()

	err = h.Disconnect("QmNonexistentPeerID")
	if err == nil {
		t.Error("Disconnect on nonexistent peer should error")
	}
}

func TestProtocolID(t *testing.T) {
	if ProtocolID != "/hivemachine/1.0.0" {
		t.Errorf("ProtocolID = %q, want %q", ProtocolID, "/hivemachine/1.0.0")
	}
}

func TestDHTMode(t *testing.T) {
	if DHTModeClient != "client" {
		t.Errorf("DHTModeClient = %q, want %q", DHTModeClient, "client")
	}
	if DHTModeServer != "server" {
		t.Errorf("DHTModeServer = %q, want %q", DHTModeServer, "server")
	}
	if DHTModeAuto != "auto" {
		t.Errorf("DHTModeAuto = %q, want %q", DHTModeAuto, "auto")
	}
}
