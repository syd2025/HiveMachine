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

func init() {
	modelsCmd.RunE = runModels
}

func runModels(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	client := api.NewClient(addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	if len(models.Data) == 0 {
		fmt.Println("No models available.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "MODEL ID\tCREATED\tOWNED BY")
	for _, m := range models.Data {
		fmt.Fprintf(w, "%s\t%d\t%s\n", m.ID, m.Created, m.OwnedBy)
	}
	w.Flush()
	fmt.Fprintf(os.Stderr, "\n(%d models)\n", len(models.Data))
	return nil
}
