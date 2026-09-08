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

	"github.com/hivemachine/internal/grpc"
	"github.com/hivemachine/pkg/gateway/api"
	"github.com/hivemachine/pkg/gateway/provider"
	"github.com/hivemachine/pkg/gateway/proxy"
	"github.com/hivemachine/pkg/paygate/receipt"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:11435", "gateway HTTP address")
	core := flag.String("core", "127.0.0.1:50051", "Rust core gRPC address")
	flag.Parse()

	grpcClient := grpc.NewClient(grpc.Config{
		Addr:     *core,
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

	log.Printf("Starting HiveMachine Gateway on %s → Rust core at %s", *addr, *core)
	server := api.NewServer(*addr, grpcClient, registry, inferenceProxy, receiptSigner)

	// Graceful shutdown: stop heartbeat and server.
	httpServer := &http.Server{Addr: *addr}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		heartbeat.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

// receiptKeyPath returns the path to the Ed25519 signing key.
// Expands ~ to the user's home directory.
func receiptKeyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "receipt.key"
	}
	return home + string(os.PathSeparator) + ".hivemachine" + string(os.PathSeparator) + "receipt.key"
}
