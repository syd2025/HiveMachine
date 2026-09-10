package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hivemachine/pkg/gateway/api"
	"github.com/hivemachine/pkg/wallet"
)

// walletCmd is the parent command for all wallet subcommands.
var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Manage your HiveMachine wallet",
}

// ---------------------------------------------------------------------------
// Subcommands
// ---------------------------------------------------------------------------

var (
	walletCreateCmd = &cobra.Command{
		Use:   "create [password]",
		Short: "Create a new BIP-39 wallet",
		Args:  cobra.ExactArgs(1),
		RunE:  runWalletCreate,
	}

	walletRestoreCmd = &cobra.Command{
		Use:   "restore [mnemonic] [password]",
		Short: "Restore a wallet from an existing BIP-39 mnemonic",
		Args:  cobra.ExactArgs(2),
		RunE:  runWalletRestore,
	}

	walletListCmd = &cobra.Command{
		Use:   "list",
		Short: "List all derived addresses",
		RunE:  runWalletList,
	}

	walletBalanceCmd = &cobra.Command{
		Use:   "balance [api_key]",
		Short: "Query gateway balance for an API key",
		Args:  cobra.ExactArgs(1),
		RunE:  runWalletBalance,
	}

	walletDepositCmd = &cobra.Command{
		Use:   "deposit [api_key]",
		Short: "Query gateway deposit address for a rail",
		Args:  cobra.ExactArgs(1),
		RunE:  runWalletDeposit,
	}

	walletStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show wallet file status and lock state",
		RunE:  runWalletStatus,
	}

	walletLockCmd = &cobra.Command{
		Use:   "lock",
		Short: "Lock the wallet",
		RunE:  runWalletLock,
	}

	walletExportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export the mnemonic (wallet must be unlocked)",
		RunE:  runWalletExport,
	}
)

func init() {
	walletCmd.AddCommand(walletCreateCmd)
	walletCmd.AddCommand(walletRestoreCmd)
	walletCmd.AddCommand(walletListCmd)
	walletCmd.AddCommand(walletBalanceCmd)
	walletCmd.AddCommand(walletDepositCmd)
	walletCmd.AddCommand(walletStatusCmd)
	walletCmd.AddCommand(walletLockCmd)
	walletCmd.AddCommand(walletExportCmd)

	walletDepositCmd.Flags().String("rail", "tap", "Payment rail: tap or tnk")
}

var walletService *wallet.PersistentService

func getWalletService() (*wallet.PersistentService, error) {
	if walletService != nil {
		return walletService, nil
	}
	dir, err := wallet.DefaultWalletDir()
	if err != nil {
		return nil, fmt.Errorf("default wallet dir: %w", err)
	}
	walletService, err = wallet.NewPersistentService(dir)
	if err != nil {
		return nil, fmt.Errorf("open wallet store: %w", err)
	}
	return walletService, nil
}

// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------

func runWalletCreate(cmd *cobra.Command, args []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}

	if svc.Exists() {
		return fmt.Errorf("wallet already exists at %s\n  Use 'mayhem wallet restore' to restore from a mnemonic",
			svc.WalletPath())
	}

	w, err := svc.CreateWallet(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✅ Wallet created\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  ID:       %s\n", w.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  File:     %s\n", svc.WalletPath())
	fmt.Fprintf(cmd.OutOrStdout(), "  Ethereum: %s\n", w.Addresses[wallet.ChainEthereum])
	fmt.Fprintf(cmd.OutOrStdout(), "  Trac:     %s\n", w.Addresses[wallet.ChainTrac])

	fmt.Fprintf(cmd.OutOrStdout(), "\n⚠️  BACK UP YOUR MNEMONIC — it is not stored.\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  Run 'mayhem wallet restore \"<24-word phrase>\" \"%s\"'\n", args[0])
	return nil
}

func runWalletRestore(cmd *cobra.Command, args []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}

	if svc.Exists() {
		return fmt.Errorf("wallet already exists at %s\n  Remove it first: rm %s", svc.WalletPath(), svc.WalletPath())
	}

	mnemonic := strings.Join(strings.Fields(args[0]), " ")
	if !wallet.ValidateMnemonic(mnemonic) {
		return fmt.Errorf("invalid BIP-39 mnemonic")
	}

	w, err := svc.RestoreWallet(context.Background(), mnemonic, args[1])
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✅ Wallet restored\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  ID:       %s\n", w.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Ethereum: %s\n", w.Addresses[wallet.ChainEthereum])
	fmt.Fprintf(cmd.OutOrStdout(), "  Trac:     %s\n", w.Addresses[wallet.ChainTrac])
	return nil
}

func runWalletList(cmd *cobra.Command, _ []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}
	if !svc.Exists() {
		return fmt.Errorf("no wallet found at %s\n  Run 'mayhem wallet create [password]' to create one", svc.WalletPath())
	}

	addrs, err := svc.ListAddresses(context.Background())
	if err != nil {
		return fmt.Errorf("list addresses: %w", err)
	}

	status := "🔒 locked"
	if svc.IsUnlocked() {
		status = "🔓 unlocked"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Wallet: %s  [%s]\n", svc.WalletPath(), status)
	for chain, addr := range addrs {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-12s  %s\n", chain, addr)
	}
	return nil
}

func runWalletBalance(cmd *cobra.Command, args []string) error {
	apiKey := args[0]
	addr, _ := cmd.Flags().GetString("addr")
	client := api.NewClient(addr)

	resp, err := client.Balance(context.Background(), apiKey)
	if err != nil {
		return fmt.Errorf("gateway balance: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "API key: %s\n", resp.APIKey)
	fmt.Fprintf(cmd.OutOrStdout(), "  Balance:  %d cents (%.2f USD)\n", resp.BalanceCents, float64(resp.BalanceCents)/100)
	fmt.Fprintf(cmd.OutOrStdout(), "  Currency: %s\n", resp.Currency)
	return nil
}

func runWalletDeposit(cmd *cobra.Command, args []string) error {
	rail, _ := cmd.Flags().GetString("rail")
	if rail != "tap" && rail != "tnk" {
		return fmt.Errorf("rail must be 'tap' or 'tnk'; got %q", rail)
	}

	apiKey := args[0]
	addr, _ := cmd.Flags().GetString("addr")
	client := api.NewClient(addr)

	depositAddr, err := client.WalletDepositAddress(context.Background(), apiKey, rail)
	if err != nil {
		return fmt.Errorf("deposit address: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "API key: %s  |  Rail: %s\n", apiKey, rail)
	fmt.Fprintf(cmd.OutOrStdout(), "  Deposit address: %s\n", depositAddr)
	fmt.Fprintf(cmd.OutOrStdout(), "\nSend only %s to this address. Do not send from an exchange.\n", strings.ToUpper(rail))
	return nil
}

func runWalletStatus(cmd *cobra.Command, _ []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}

	if !svc.Exists() {
		fmt.Fprintf(cmd.OutOrStdout(), "No wallet found at %s\n", svc.WalletPath())
		fmt.Fprintf(cmd.OutOrStdout(), "Run 'mayhem wallet create [password]' to create one.\n")
		return nil
	}

	status := "🔒 locked"
	if svc.IsUnlocked() {
		status = "🔓 unlocked"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "File:   %s\n", svc.WalletPath())
	fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", status)

	addrs, _ := svc.ListAddresses(context.Background())
	for chain, addr := range addrs {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-12s  %s\n", chain, addr)
	}
	return nil
}

func runWalletLock(cmd *cobra.Command, _ []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}
	if err := svc.Lock(context.Background()); err != nil && !errors.Is(err, wallet.ErrWalletLocked) {
		return fmt.Errorf("lock: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "🔒 Wallet locked.\n")
	return nil
}

func runWalletExport(cmd *cobra.Command, _ []string) error {
	svc, err := getWalletService()
	if err != nil {
		return fmt.Errorf("wallet service: %w", err)
	}
	if !svc.Exists() {
		return fmt.Errorf("no wallet found")
	}
	if !svc.IsUnlocked() {
		return fmt.Errorf("wallet is locked; unlock it first (currently unsupported — wallet unlocks on startup if file exists)")
	}

	mnemonic, err := svc.GetMnemonic(context.Background(), "")
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Mnemonic (24 words):\n  %s\n", mnemonic)
	fmt.Fprintf(cmd.OutOrStdout(), "\n⚠️  Never share this phrase with anyone.\n")
	return nil
}
