package cmd

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/hivemachine/pkg/gateway/api"
)

func init() {
	doctorCmd.RunE = runDoctor
}

func runDoctor(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	core, _ := cmd.Flags().GetString("core")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := api.NewClient(addr)

	type check struct {
		name   string
		status string
		ok     bool
	}

	checks := []check{
		{"Go version", runtime.Version(), true},
	}

	// Gateway port.
	if portListening(addr) {
		checks = append(checks, check{"Gateway port open", "✅ "+addr, true})
	} else {
		checks = append(checks, check{"Gateway port open", "❌ not listening on "+addr, false})
	}

	// /health.
	if err := client.Health(ctx); err != nil {
		checks = append(checks, check{"/health", "❌ " + err.Error(), false})
	} else {
		checks = append(checks, check{"/health", "✅ healthy", true})
	}

	// /v1/models.
	if models, err := client.ListModels(ctx); err != nil {
		checks = append(checks, check{"/v1/models", "❌ " + err.Error(), false})
	} else {
		checks = append(checks, check{"/v1/models", fmt.Sprintf("✅ %d models available", len(models.Data)), true})
	}

	// Rust core gRPC.
	if portListening(core) {
		checks = append(checks, check{"Rust core gRPC", "✅ " + core, true})
	} else {
		checks = append(checks, check{"Rust core gRPC", "❌ not listening on " + core, false})
	}

	// Disk space.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_ = m // future: syscall.Statfs

	allOk := true
	fmt.Println("┌──────────────────────────────────────────────────────┐")
	fmt.Println("│         HiveMachine Diagnostic Report                 │")
	fmt.Println("└──────────────────────────────────────────────────────┘")
	for _, c := range checks {
		prefix := "✅"
		if !c.ok {
			prefix = "❌"
			allOk = false
		}
		fmt.Printf("%s %-20s %s\n", prefix, c.name+":", c.status)
	}
	fmt.Println()

	if allOk {
		fmt.Println("✅ All checks passed.")
		return nil
	}
	fmt.Println("⚠ Some checks failed.")
	return fmt.Errorf("diagnostic failed")
}

