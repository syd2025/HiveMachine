package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hivemachine/pkg/gateway/api"
)

// P2P command group.
var netCmd = &cobra.Command{
	Use:   "net",
	Short: "P2P networking commands",
}

var (
	netStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show P2P node info (peer ID, listen addresses)",
		RunE:  runNetStatus,
	}

	netPeersCmd = &cobra.Command{
		Use:   "peers",
		Short: "List all connected P2P peers",
		RunE:  runNetPeers,
	}

	netProvideCmd = &cobra.Command{
		Use:   "provide [key]",
		Short: "Announce a key (e.g. model:name) via DHT",
		Args:  cobra.ExactArgs(1),
		RunE:  runNetProvide,
	}

	netProvidersCmd = &cobra.Command{
		Use:   "providers [key]",
		Short: "Search DHT for providers of a key",
		Args:  cobra.ExactArgs(1),
		RunE:  runNetProviders,
	}
)

func init() {
	netCmd.AddCommand(netStatusCmd)
	netCmd.AddCommand(netPeersCmd)
	netCmd.AddCommand(netProvideCmd)
	netCmd.AddCommand(netProvidersCmd)

	// Register at root level.
	rootCmd.AddCommand(netCmd)
}

var netClient = func(addr string) *api.Client {
	return api.NewClient(addr)
}

func runNetStatus(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	c := netClient(addr)

	info, err := c.P2PInfo(context.Background())
	if err != nil {
		return fmt.Errorf("p2p info: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "P2P Node:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Peer ID: %s\n", info.PeerID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Listen addresses:\n")
	for _, a := range info.Addrs {
		fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", a)
	}
	return nil
}

func runNetPeers(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	c := netClient(addr)

	resp, err := c.P2PPeers(context.Background())
	if err != nil {
		return fmt.Errorf("p2p peers: %w", err)
	}

	if len(resp.Peers) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No connected peers.\n")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Connected peers (%d):\n", len(resp.Peers))
	for _, p := range resp.Peers {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", p.ID)
	}
	return nil
}

func runNetProvide(cmd *cobra.Command, args []string) error {
	key := args[0]
	addr, _ := cmd.Flags().GetString("addr")
	c := netClient(addr)

	err := c.P2PProvide(context.Background(), key)
	if err != nil {
		return fmt.Errorf("p2p provide: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✓ Announced key: %s\n", key)
	return nil
}

func runNetProviders(cmd *cobra.Command, args []string) error {
	key := args[0]
	addr, _ := cmd.Flags().GetString("addr")
	c := netClient(addr)

	providers, err := c.P2PProviders(context.Background(), key)
	if err != nil {
		return fmt.Errorf("p2p providers: %w", err)
	}

	if len(providers) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No providers found for: %s\n", key)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Providers for %s (%d):\n", key, len(providers))
	for _, p := range providers {
		fmt.Fprintf(cmd.OutOrStdout(), "  Peer: %s\n", p.PeerID)
		for _, a := range p.Addrs {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", a)
		}
	}
	return nil
}
