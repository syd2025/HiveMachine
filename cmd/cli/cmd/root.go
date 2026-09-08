package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	upCmd        = &cobra.Command{Use: "up", Short: "Start the HiveMachine gateway"}
	downCmd      = &cobra.Command{Use: "down", Short: "Stop the HiveMachine gateway"}
	doctorCmd    = &cobra.Command{Use: "doctor", Short: "Check system health and HiveMachine readiness"}
	modelsCmd    = &cobra.Command{Use: "models", Short: "List available models from the gateway"}
	providerCmd  = &cobra.Command{Use: "provider", Short: "Manage and list providers"}
	payCmd       = &cobra.Command{Use: "pay", Short: "Payment management"}
)

var rootCmd = &cobra.Command{
	Use:   "mayhem",
	Short: "HiveMachine CLI - P2P AI inference marketplace",
	Long:  `HiveMachine - Sell inference from any machine, buy at market price.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("addr", "127.0.0.1:11435", "Gateway HTTP address")
	rootCmd.PersistentFlags().String("core", "127.0.0.1:50051", "Rust core gRPC address")

	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(modelsCmd)
	rootCmd.AddCommand(providerCmd)
	rootCmd.AddCommand(payCmd)
	rootCmd.AddCommand(hardwareCmd)
}
