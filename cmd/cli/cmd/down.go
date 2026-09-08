package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	downCmd.RunE = runDown
}

func runDown(cmd *cobra.Command, args []string) error {
	pidPath := filepath.Join(mayhemDir(), "gateway.pid")

	pid, err := readPID(pidPath)
	if err != nil {
		return fmt.Errorf("no PID file (gateway not running?): %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	// Graceful SIGTERM.
	proc.Signal(syscall.SIGTERM)

	// Poll for exit (5s).
	for range 10 {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			os.Remove(pidPath)
			fmt.Printf("✅ Gateway stopped (PID %d)\n", pid)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill.
	proc.Kill()
	os.Remove(pidPath)
	fmt.Printf("✅ Gateway force-killed (PID %d)\n", pid)
	return nil
}
