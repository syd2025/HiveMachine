package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/pkg/gateway/api"
	"github.com/hivemachine/pkg/gateway/provider"
	"github.com/hivemachine/pkg/gateway/proxy"
	"github.com/hivemachine/pkg/p2p"
	"github.com/hivemachine/pkg/paygate/balance"
	"github.com/hivemachine/pkg/paygate/receipt"
	"github.com/hivemachine/pkg/paygate/stripe"
	"github.com/hivemachine/pkg/paygate/tap"
	tnk "github.com/hivemachine/pkg/paygate/tnk"
)

var (
	gatewayAddr   = flag.String("addr", "127.0.0.1:11435", "gateway HTTP address")
	coreAddr      = flag.String("core", "127.0.0.1:50051", "Rust core gRPC address")
	stripeKey     = flag.String("stripe-secret-key", os.Getenv("STRIPE_SECRET_KEY"), "Stripe secret key (or STRIPE_SECRET_KEY env)")
	stripeWebhook = flag.String("stripe-webhook-secret", os.Getenv("STRIPE_WEBHOOK_SECRET"), "Stripe webhook secret (or STRIPE_WEBHOOK_SECRET env)")

	// P2P options.
	p2pEnabled     = flag.Bool("p2p", false, "enable P2P networking")
	p2pListenAddrs  = flag.String("p2p-listen", "/ip4/0.0.0.0/tcp/0", "P2P listen multiaddresses (comma-separated)")
	p2pBootstrap    = flag.String("p2p-bootstrap", "", "P2P bootstrap peer addresses (comma-separated)")
	p2pDHTMode     = flag.String("p2p-dht", "auto", "DHT mode: client, server, auto")

	// TAP / TNK (Ethereum) options.
	tapRPCURL   = flag.String("tap-rpc-url", "", "Ethereum RPC URL for TAP (ERC-20) rail")
	tapEthPrice = flag.Float64("tap-eth-price", 3500, "ETH/USD price for TAP conversion")

	tnkRPCURL   = flag.String("tnk-rpc-url", "", "Ethereum RPC URL for TNK (Trac) rail")
	tnkTNKPrice = flag.Float64("tnk-tnk-price", 0.10, "TNK/USD price for TNK conversion")
)

func main() {
	flag.Parse()

	grpcClient := grpc.NewClient(grpc.Config{
		Addr:     *coreAddr,
		PoolSize: 4,
	})

	// Provider registry: refreshes from Rust core and tracks heartbeats.
	registry := provider.NewRegistry(grpcClient)
	heartbeat := provider.NewHeartbeatLoop(registry, grpcClient)
	heartbeat.Start()

	// Initial population of the registry.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	registry.Refresh(ctx)
	cancel()

	// Router with failover support.
	router := proxy.NewRouter(registry, nil, 2)
	inferenceProxy := proxy.NewProxy(grpcClient, router)

	// Receipt signing key — load or generate on first run.
	receiptSigner, err := receipt.NewSignerFromFile(receiptKeyPath())
	if err != nil {
		log.Printf("warning: receipt signer unavailable: %v (receipts disabled)", err)
	}

	// Build server options.
	var serverOpts []api.Option

	// Stripe paygate.
	var balanceStore api.BalanceStore
	var paygate *stripe.Paygate
	if *stripeKey != "" {
		balanceStore = api.NewInMemoryBalanceStore()
		paygate = stripe.NewPaygate(*stripeKey, *stripeWebhook, balanceStore)
		serverOpts = append(serverOpts, api.WithBalanceStore(balanceStore), api.WithStripePaygate(paygate))
		log.Printf("Stripe configured (key prefix: %s)", (*stripeKey)[:min(8, len(*stripeKey))])
	}

	// TAP rail (ERC-20 on Ethereum).
	var tapRail *tap.Rail
	if *tapRPCURL != "" {
		ethClient, err := ethclient.Dial(*tapRPCURL)
		if err != nil {
			log.Fatalf("TAP RPC dial failed: %v", err)
		}
		tapCfg := tap.DefaultConfig()
		tapTNKRail, err := tap.NewTAPRail(tapCfg, ethClient, nil, tap.NewInMemoryDepositTracker())
		if err != nil {
			log.Fatalf("TAP rail creation failed: %v", err)
		}
		railStore := balance.NewRailStore()
		addrMgr := tap.NewDepositAddressManager()
		hotAddr := common.HexToAddress("0x0000000000000000000000000000000000000000")
		tapRail = tap.NewRail(tapTNKRail, railStore, addrMgr, &tap.StaticPriceFeed{PriceUSD: *tapEthPrice}, hotAddr)
		serverOpts = append(serverOpts, api.WithRailStore(railStore))
		log.Printf("TAP rail configured (RPC: %s, ETH/USD: %.0f)", *tapRPCURL, *tapEthPrice)
	}

	// TNK rail (Trac ERC-20 on Ethereum).
	var tnkRail *tnk.Rail
	if *tnkRPCURL != "" {
		ethClient, err := ethclient.Dial(*tnkRPCURL)
		if err != nil {
			log.Fatalf("TNK RPC dial failed: %v", err)
		}
		tnkCfg := tnk.DefaultConfig()
		tnkTNKRail, err := tnk.NewTNKRail(tnkCfg, ethClient, nil, tnk.NewInMemoryDepositTracker())
		if err != nil {
			log.Fatalf("TNK rail creation failed: %v", err)
		}
		railStore := balance.NewRailStore()
		addrMgr := tnk.NewDepositAddressManager()
		hotAddr := common.HexToAddress("0x0000000000000000000000000000000000000000")
		tnkRail = tnk.NewRail(tnkTNKRail, railStore, addrMgr, &tnk.StaticTokenPriceFeed{PriceUSD: *tnkTNKPrice}, hotAddr)
		serverOpts = append(serverOpts, api.WithRailStore(railStore))
		log.Printf("TNK rail configured (RPC: %s, TNK/USD: %.4f)", *tnkRPCURL, *tnkTNKPrice)
	}

	// P2P networking.
	var p2pService *p2p.P2PService
	if *p2pEnabled {
		p2pCfg := p2p.DefaultConfig()
		if *p2pListenAddrs != "" {
			p2pCfg.ListenAddresses = splitAddrs(*p2pListenAddrs)
		}
		if *p2pBootstrap != "" {
			p2pCfg.BootstrapPeers = splitAddrs(*p2pBootstrap)
		}
		switch *p2pDHTMode {
		case "client":
			p2pCfg.DHTMode = p2p.DHTModeClient
		case "server":
			p2pCfg.DHTMode = p2p.DHTModeServer
		default:
			p2pCfg.DHTMode = p2p.DHTModeAuto
		}

		host, err := p2p.NewHost(ctx, p2pCfg)
		if err != nil {
			log.Fatalf("P2P host creation failed: %v", err)
		}
		sm, err := p2p.NewSessionManager(host, p2p.DefaultSessionManagerConfig())
		if err != nil {
			log.Fatalf("P2P session manager creation failed: %v", err)
		}
		p2pService = p2p.NewService(host, sm)
		serverOpts = append(serverOpts, api.WithP2PService(p2pService))
		log.Printf("P2P enabled: listening on %v", p2pCfg.ListenAddresses)
	}

	log.Printf("Starting HiveMachine Gateway on %s → Rust core at %s", *gatewayAddr, *coreAddr)
	server := api.NewServer(*gatewayAddr, grpcClient, registry, inferenceProxy, receiptSigner, serverOpts...)

	// Graceful shutdown.
	httpServer := &http.Server{Addr: *gatewayAddr}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		heartbeat.Stop()
		if p2pService != nil {
			p2pService.Stop()
		}
		if tapRail != nil {
			tapRail.Stop()
		}
		if tnkRail != nil {
			tnkRail.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

func splitAddrs(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(","); i++ {
		if s[i:i+len(",")] == "," {
			part := stripWhitespace(s[start:i])
			if part != "" {
				result = append(result, part)
			}
			start = i + len(",")
			i += len(",") - 1
		}
	}
	part := stripWhitespace(s[start:])
	if part != "" {
		result = append(result, part)
	}
	return result
}

func stripWhitespace(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func min(a, b int) int { if a < b { return a }; return b }

// receiptKeyPath returns the path to the Ed25519 signing key.
func receiptKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "receipt.key"
	}
	return home + string(os.PathSeparator) + ".hivemachine" + string(os.PathSeparator) + "receipt.key"
}
