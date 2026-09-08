package p2p

import (
	"context"
	"sync"

	"github.com/ipfs/go-cid"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

var (
	// dummyCid is a placeholder CID used as the DHT key prefix.
	// In production, provider keys would use real CIDs derived from content.
	dummyCid cid.Cid
)

func init() {
	// Create a "namespace" CID for provider discovery.
	// This is a fixed CIDv1 with the raw multicodec, used as a key prefix.
	var err error
	dummyCid, err = cid.Parse("bafzbeiczsscd7yxnhl3xylrqlev5ire3gyltlyqo5ka7s6sw34kr2l3lne")
	if err != nil {
		panic("invalid hardcoded CID: " + err.Error())
	}
}

// discovery bridges DHT-based and in-memory provider discovery.
type discovery struct {
	host    host.Host
	dht     *dht.IpfsDHT
	mu      sync.RWMutex
	records map[string][]peer.AddrInfo // key → providers
}

// newDiscovery creates a discovery service.
func newDiscovery(ctx context.Context, h host.Host, mode DHTMode) (*discovery, error) {
	d := &discovery{
		host:    h,
		records: make(map[string][]peer.AddrInfo),
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

		rt, err := dht.New(h, dhtMode, dht.Concurrency(10))
		if err != nil {
			// Non-fatal — fall back to in-memory only.
			return d, nil
		}
		d.dht = rt
	}

	return d, nil
}

// keyToCid converts a string key to a CIDv1 for DHT use.
func keyToCid(key string) cid.Cid {
	// Use the dummy CID with a namespace prefix.
	// In production, use go-cid to derive a real CID from the key.
	return dummyCid
}

// FindProviders searches for providers of the given key.
// Uses DHT if available, falls back to in-memory registry.
func (d *discovery) FindProviders(ctx context.Context, key string) ([]peer.AddrInfo, error) {
	// Try DHT first.
	if d.dht != nil {
		c := keyToCid(key)
		providers, err := d.dht.FindProviders(ctx, c)
		if err == nil && len(providers) > 0 {
			return providers, nil
		}
		// Fall through to in-memory on DHT failure.
	}

	// Fall back to in-memory registry.
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.records[key], nil
}

// Provide announces this node as a provider of the given key.
func (d *discovery) Provide(ctx context.Context, key string) error {
	ai := peer.AddrInfo{
		ID:    d.host.ID(),
		Addrs: d.host.Addrs(),
	}

	// Announce via DHT if available.
	if d.dht != nil {
		c := keyToCid(key)
		_ = d.dht.Provide(ctx, c, true)
	}

	// Always register in local registry as fallback.
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records[key] = append(d.records[key], ai)
	return nil
}

// Close shuts down the discovery service.
func (d *discovery) Close() error {
	if d.dht != nil {
		return d.dht.Close()
	}
	return nil
}
