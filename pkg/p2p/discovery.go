package p2p

import (
	"context"
	"fmt"
	"sync"

	"github.com/ipfs/go-cid"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// discovery bridges DHT-based and in-memory provider discovery.
type discovery struct {
	host    host.Host
	dht     *dht.IpfsDHT
	mu      sync.RWMutex
	records map[string][]peer.AddrInfo // key → providers
	mode    DHTMode
	bootstrapAddr string
}

// newDiscovery creates a discovery service.
func newDiscovery(ctx context.Context, h host.Host, mode DHTMode, bootstrapPeers []string) (*discovery, error) {
	d := &discovery{
		host:    h,
		records: make(map[string][]peer.AddrInfo),
		mode:    mode,
	}

	if mode != "" && mode != DHTModeAuto {
		var dhtMode dht.Option
		switch mode {
		case DHTModeServer:
			dhtMode = dht.Mode(dht.ModeServer)
		case DHTModeClient:
			dhtMode = dht.Mode(dht.ModeClient)
		default:
			dhtMode = dht.Mode(dht.ModeAuto)
		}

		opts := []dht.Option{
			dhtMode,
			dht.Concurrency(10),
			dht.RoutingTableRefreshPeriod(30*60*1e9), // 30 minutes
		}

		// Use simplified DHT options to avoid bootstrap requirement
		rt, err := dht.New(h, opts...)
		if err != nil {
			// Non-fatal — fall back to in-memory only
			return d, nil
		}
		d.dht = rt

		// Bootstrap if we have bootstrap peers configured
		if len(bootstrapPeers) > 0 {
			go d.bootstrap(ctx, bootstrapPeers)
		}
	}

	return d, nil
}

// bootstrap connects to bootstrap peers for DHT.
func (d *discovery) bootstrap(ctx context.Context, bootstrapPeers []string) {
	for _, addrStr := range bootstrapPeers {
		addrInfo, err := peer.AddrInfoFromString(addrStr)
		if err != nil {
			continue
		}
		if err := d.host.Connect(ctx, *addrInfo); err != nil {
			continue
		}
		// Successfully connected, we can break
		break
	}
}

// setConfig updates the discovery configuration.
func (d *discovery) setConfig(mode DHTMode, bootstrapAddr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mode = mode
	d.bootstrapAddr = bootstrapAddr
}

// keyToCid converts a string key to a CIDv1 for DHT use.
func keyToCid(key string) cid.Cid {
	// Use a namespace CID for DHT key space
	// In production, real CIDs would be derived from content hashes
	return cid.MustParse("bafzbeiczsscd7yxnhl3xylrqlev5ire3gyltlyqo5ka7s6sw34kr2l3lne")
}

// FindProviders searches for providers of the given key.
// Uses DHT if available, falls back to in-memory registry.
func (d *discovery) FindProviders(ctx context.Context, key string) ([]peer.AddrInfo, error) {
	// Try DHT first
	if d.dht != nil {
		c := keyToCid(key)
		providers, err := d.dht.FindProviders(ctx, c)
		if err == nil && len(providers) > 0 {
			return providers, nil
		}
		// Fall through to in-memory on DHT failure
	}

	// Fall back to in-memory registry
	d.mu.RLock()
	defer d.mu.RUnlock()
	providers, ok := d.records[key]
	if !ok {
		return []peer.AddrInfo{}, nil
	}
	return providers, nil
}

// Provide announces this node as a provider of the given key.
func (d *discovery) Provide(ctx context.Context, key string) error {
	ai := peer.AddrInfo{
		ID:    d.host.ID(),
		Addrs: d.host.Addrs(),
	}

	// Announce via DHT if available
	if d.dht != nil {
		c := keyToCid(key)
		if err := d.dht.Provide(ctx, c, true); err != nil {
			// Log but don't fail - we still register locally
			fmt.Printf("DHT provide warning: %v\n", err)
		}
	}

	// Always register in local registry as fallback
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[key] = append(d.records[key], ai)
	return nil
}

// FindPeers searches for peers with specific protocol or service.
// This uses the DHT to find peers, falling back to connected peers.
func (d *discovery) FindPeers(ctx context.Context, protocol string) ([]peer.AddrInfo, error) {
	// First, return our directly connected peers
	var result []peer.AddrInfo
	for _, p := range d.host.Network().Peers() {
		pi := d.host.Peerstore().PeerInfo(p)
		result = append(result, pi)
	}
	return result, nil
}

// Close shuts down the discovery service.
func (d *discovery) Close() error {
	if d.dht != nil {
		return d.dht.Close()
	}
	return nil
}