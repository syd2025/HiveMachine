package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hivemachine/pkg/gateway/api"
)

var providerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active providers",
	RunE:  runProviderList,
}

func init() {
	providerCmd.AddCommand(providerListCmd)
}

func runProviderList(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	client := api.NewClient(addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	providers, err := client.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}

	if len(providers.Data) == 0 {
		fmt.Println("No providers registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER ID\tSTATUS\tREPUTATION\tENDPOINT\tMODELS")
	for _, p := range providers.Data {
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\t%d\n",
			p.ID, p.Status, p.Reputation, p.Endpoint, len(p.Models))
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\n(%d providers)\n", len(providers.Data))
	return nil
}
