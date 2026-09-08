package cmd

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/hivemachine/pkg/gateway/api"
)

var (
	payBalanceCmd = &cobra.Command{
		Use:   "balance [api_key]",
		Short: "Show account balance for an API key",
		Args:  cobra.ExactArgs(1),
		RunE:  runPayBalance,
	}

	payTopupCmd = &cobra.Command{
		Use:   "topup [api_key] [cents]",
		Short: "Add credits to an account",
		Args:  cobra.ExactArgs(2),
		RunE:  runPayTopup,
	}
)

func init() {
	payCmd.AddCommand(payBalanceCmd)
	payCmd.AddCommand(payTopupCmd)
}

var payClient = func(addr string) *api.Client {
	return api.NewClient(addr)
}

func runPayBalance(cmd *cobra.Command, args []string) error {
	apiKey := args[0]
	addr, _ := cmd.Flags().GetString("addr")

	client := payClient(addr)
	resp, err := client.Balance(context.Background(), apiKey)
	if err != nil {
		return fmt.Errorf("balance failed: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "┌──────────────────────────────────────┐\n")
	fmt.Fprintf(cmd.OutOrStdout(), "│       Account Balance                  │\n")
	fmt.Fprintf(cmd.OutOrStdout(), "└──────────────────────────────────────┘\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  API Key:        %s\n", resp.APIKey)
	fmt.Fprintf(cmd.OutOrStdout(), "  Balance:        %d cents (%.2f USD)\n", resp.BalanceCents, float64(resp.BalanceCents)/100)
	fmt.Fprintf(cmd.OutOrStdout(), "  Currency:       %s\n", resp.Currency)
	return nil
}

func runPayTopup(cmd *cobra.Command, args []string) error {
	apiKey := args[0]
	cents, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || cents <= 0 {
		return fmt.Errorf("cents must be a positive integer; got %q", args[1])
	}

	addr, _ := cmd.Flags().GetString("addr")
	client := payClient(addr)

	resp, err := client.TopUp(context.Background(), apiKey, cents)
	if err != nil {
		return fmt.Errorf("topup failed: %v", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "┌──────────────────────────────────────┐\n")
	fmt.Fprintf(cmd.OutOrStdout(), "│       Top-Up Successful                │\n")
	fmt.Fprintf(cmd.OutOrStdout(), "└──────────────────────────────────────┘\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  API Key:        %s\n", resp.APIKey)
	fmt.Fprintf(cmd.OutOrStdout(), "  Added:         +%d cents (+%.2f USD)\n", resp.AddedCents, float64(resp.AddedCents)/100)
	fmt.Fprintf(cmd.OutOrStdout(), "  New Balance:    %d cents (%.2f USD)\n", resp.BalanceCents, float64(resp.BalanceCents)/100)

	return nil
}
